package store

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

/*
 * Restoring a snapshot (ADR 0058).
 *
 * Restoring is offline, and that is not a limitation to be smoothed over: a
 * live restore would mean swapping the database out from under open
 * transactions, which is how a restore becomes the incident. This function
 * therefore assumes the server is stopped, and its caller is responsible for
 * having checked — the check is a question about processes and ports, which
 * this layer has no business answering.
 *
 * Three things make the difference between a restore and a bad afternoon.
 *
 * The snapshot is *copied*, never moved. Somebody restoring from their only
 * backup must still have that backup when it is over.
 *
 * The database being replaced is moved aside under a stamped name and never
 * deleted. Stamped rather than reused, for the reason the updater learned the
 * hard way: a fixed `.old` name is one a second run overwrites, and the copy it
 * overwrites is the one you wanted.
 *
 * And the WAL and shared-memory sidecars go with it. This is the quiet one. A
 * `-wal` left beside a replaced database belongs to the *previous* database,
 * and SQLite will cheerfully apply it to the new file on the next open. The
 * result is neither the old database nor the new one, and it is arrived at by
 * a restore that reported success.
 */

// Restore is what a restore did, reported so a caller can say it rather than
// assert it.
type Restore struct {
	DBPath   string   `json:"db_path"`
	Snapshot Snapshot `json:"snapshot"`
	// ReplacedPath is where the previous database was moved to, empty when
	// there was none. It is left on disk deliberately.
	ReplacedPath string `json:"replaced_path,omitempty"`
	// SchemaVersionAfter is the revision after migrating forward, which may be
	// higher than the snapshot's own if the backup came from an older build.
	SchemaVersionAfter int `json:"schema_version_after"`
	// SessionsCleared is how many logins the backup carried, which for a
	// backup taken by this build is zero — they are cleared at snapshot time
	// (ADR 0058, as amended), so the property belongs to the file rather than
	// to whoever restores it.
	//
	// This is not therefore dead: a backup written before that amendment
	// carries its sessions and stays restorable, which is the point of having
	// backups, and this is the only thing that covers one.
	SessionsCleared int `json:"sessions_cleared"`
}

// sidecars are the files SQLite keeps beside a database. They must travel with
// it or not exist; a stale one is worse than a missing one.
var sidecars = []string{"-wal", "-shm", "-journal"}

/*
 * RestoreSnapshot replaces the database at dbPath with the snapshot at
 * snapshotPath.
 *
 * The schema gate runs first, before anything on disk is touched: a backup
 * from a newer build is refused by name rather than discovered at the next
 * startup, after the live database has already been replaced.
 *
 * A backup from an *older* build is fine and is migrated forward on the open
 * that follows, which is why there are no down migrations to worry about here.
 */
func RestoreSnapshot(ctx context.Context, dbPath, snapshotPath string) (Restore, error) {
	snap, err := InspectSnapshot(snapshotPath)
	if err != nil {
		return Restore{}, err
	}
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return Restore{}, fmt.Errorf("restore: resolve %s: %w", dbPath, err)
	}
	if sameFile(abs, snap.Path) {
		return Restore{}, fmt.Errorf("restore: %s is the live database, not a backup of it", snap.Path)
	}

	stamp := time.Now().Format("20060102-150405")
	moved, err := moveAside(abs, stamp)
	if err != nil {
		return Restore{}, err
	}

	// Everything from here can fail with the live database already moved, so
	// every path out either finishes or puts it back.
	restore := Restore{DBPath: abs, Snapshot: snap}
	for _, m := range moved {
		// The database itself, not a sidecar — a stray `-wal` beside no
		// database moves too, and reporting that as "your previous database is
		// here" would send somebody to a file that is not one.
		if m.from == abs {
			restore.ReplacedPath = m.to
		}
	}
	undo := func(cause error) (Restore, error) {
		os.Remove(abs)
		for _, m := range moved {
			if err := os.Rename(m.to, m.from); err != nil {
				return Restore{}, fmt.Errorf("%w\n"+
					"and the database could not be put back: %v\n"+
					"it is at %s — the restore did not happen, so move it back by hand",
					cause, err, m.to)
			}
		}
		return Restore{}, cause
	}

	if err := copyFile(snap.Path, abs); err != nil {
		return undo(fmt.Errorf("restore: copy %s into place: %w", snap.Path, err))
	}

	// Opening migrates an older backup forward, and is also the first real
	// proof that what was copied is a database this build can use.
	st, err := Open(abs)
	if err != nil {
		return undo(fmt.Errorf("restore: the restored database would not open: %w", err))
	}
	defer st.Close()

	count, err := st.CountSessions(ctx)
	if err != nil {
		return undo(fmt.Errorf("restore: count sessions: %w", err))
	}
	if err := st.DeleteAllSessions(ctx); err != nil {
		return undo(fmt.Errorf("restore: clear sessions: %w", err))
	}
	version, err := st.SchemaVersion()
	if err != nil {
		return undo(fmt.Errorf("restore: read schema version: %w", err))
	}

	restore.SessionsCleared = count
	restore.SchemaVersionAfter = version
	return restore, nil
}

type movedFile struct{ from, to string }

/*
 * moveAside renames the database and its sidecars out of the way, newest
 * name first.
 *
 * A missing database is not an error: restoring onto a machine that has never
 * run LANcast is a normal thing to do, and is most of the point.
 */
func moveAside(dbPath, stamp string) ([]movedFile, error) {
	var moved []movedFile
	paths := append([]string{dbPath}, sidecarPaths(dbPath)...)
	suffix := freeSuffix(paths, stamp)

	for _, from := range paths {
		if _, err := os.Stat(from); err != nil {
			continue
		}
		to := from + suffix
		if err := os.Rename(from, to); err != nil {
			// Put back whatever already moved, so a failure halfway does not
			// leave a database beside an orphaned WAL.
			for _, m := range moved {
				os.Rename(m.to, m.from)
			}
			return nil, fmt.Errorf("restore: move %s aside: %w\n"+
				"this usually means the server is still running and holds the database", from, err)
		}
		moved = append(moved, movedFile{from: from, to: to})
	}
	return moved, nil
}

/*
 * freeSuffix finds an aside suffix that collides with nothing.
 *
 * The stamp is only second-resolution, and two restores in the same second are
 * not hypothetical — restoring, seeing it was the wrong backup, and restoring
 * again is the normal way this goes wrong. `os.Rename` overwrites on Windows,
 * so a collision would silently destroy the copy the *first* restore set
 * aside: the one holding the data that was live before any of this started,
 * and the only thing standing between somebody and losing it.
 *
 * This is the `.old` mistake the updater made, in a place where the file at
 * stake is the whole library rather than an executable that can be downloaded
 * again.
 */
func freeSuffix(paths []string, stamp string) string {
	base := ".replaced-" + stamp
	for n := 1; ; n++ {
		suffix := base
		if n > 1 {
			suffix = fmt.Sprintf("%s-%d", base, n)
		}
		taken := false
		for _, p := range paths {
			if _, err := os.Stat(p + suffix); err == nil {
				taken = true
				break
			}
		}
		if !taken {
			return suffix
		}
	}
}

func sidecarPaths(dbPath string) []string {
	out := make([]string, 0, len(sidecars))
	for _, suffix := range sidecars {
		out = append(out, dbPath+suffix)
	}
	return out
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	// Synced before the handle is reported good. A restore that returns success
	// and then loses the file to a power cut has told the truth about nothing.
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

// sameFile answers whether two paths are the same file, by identity where the
// OS can say so and by name where it cannot.
func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}
