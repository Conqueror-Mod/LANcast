package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

/*
 * Snapshots — a backup is the database (ADR 0058).
 *
 * `VACUUM INTO` is the whole reason this is short. It writes a consistent
 * snapshot of a database that is *in use*, which is the thing a file copy
 * cannot do: with WAL enabled, copying `lancast.db` on its own yields a file
 * that is torn, plausible, and wrong — the worst failure a backup can have,
 * because it is discovered at restore time rather than at backup time.
 *
 * Artwork is deliberately not here. The cache is roughly forty-six times the
 * database's size and it is content-addressed and re-fetchable, so including
 * it would turn a sub-second operation somebody might do daily into a
 * multi-gigabyte one they do never — and a backup nobody takes protects
 * nothing.
 *
 * What makes any of this urgent is not what a rescan rebuilds slowly but what
 * it cannot rebuild at any price: watch history and positions, ratings,
 * playlists and their membership locks, collections, sensitive marks, and
 * every locked field. A rescan reconciles *files* and deliberately does not
 * re-litigate identity, so nothing can reconstruct a person's corrections from
 * the media.
 */

// Snapshot describes a backup file — the one taken, or one found on disk.
type Snapshot struct {
	Path string `json:"path"`
	// SchemaVersion is the revision recorded *inside* the snapshot, which is
	// what decides whether this build can restore it.
	SchemaVersion int   `json:"schema_version"`
	Bytes         int64 `json:"bytes"`
	TakenAt       int64 `json:"taken_at"`
}

// ErrSnapshotExists reports a destination that is already occupied. A backup
// never overwrites: SQLite refuses the write itself, and this turns that into
// something a caller can test for rather than a string to match on.
var ErrSnapshotExists = errors.New("snapshot already exists")

// ErrNotSnapshot reports a file that is not a LANcast database.
var ErrNotSnapshot = errors.New("not a LANcast backup")

/*
 * SnapshotTooNewError is the schema gate, and it is the reason restoring reads
 * the snapshot before the server opens it.
 *
 * Migrations are one-way by design — there are no down migrations, because
 * rolling a media library's schema backwards loses exactly the data a rescan
 * cannot regenerate. An *older* backup restored into a newer build migrates
 * forward and is fine. A *newer* backup opened by an older build is not: it
 * gets "database is schema version N but this build supports N-1" from
 * migrate, at startup, after the file has already replaced the live one.
 *
 * That failure has cost this project once already. Naming the build a person
 * needs, before anything is moved, is the entire point.
 */
type SnapshotTooNewError struct {
	Found     int
	Supported int
}

func (e *SnapshotTooNewError) Error() string {
	return fmt.Sprintf("backup is schema version %d but this build supports %d — upgrade LANcast to restore it",
		e.Found, e.Supported)
}

/*
 * Snapshot writes a consistent copy of the database to path.
 *
 * The destination must not exist. SQLite enforces that itself, and it is kept
 * rather than smoothed over: a backup command that silently replaces the
 * previous backup has, at the moment it fails halfway, destroyed the only good
 * copy in service of writing a bad one.
 *
 * Safe against a live server with no downtime — no lock is held on the source
 * beyond a read, and nothing is written to it.
 */
func (s *Store) Snapshot(ctx context.Context, path string) (Snapshot, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("snapshot: resolve %s: %w", path, err)
	}

	// Checked up front only so the failure says which directory is missing.
	// SQLite's own answer to an absent parent is "unable to open database
	// file", which is true and tells nobody what to do about it.
	dir := filepath.Dir(abs)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return Snapshot{}, fmt.Errorf("snapshot: %s is not a directory to write into", dir)
	}
	if _, err := os.Stat(abs); err == nil {
		return Snapshot{}, fmt.Errorf("snapshot %s: %w", abs, ErrSnapshotExists)
	}

	// Bound rather than interpolated. VACUUM INTO takes an expression, so the
	// parameter binds like any other — verified against modernc.org/sqlite,
	// the driver LANcast ships, rather than assumed. Interpolating would mean
	// quoting a user-chosen path into SQL by hand, for no gain.
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, abs); err != nil {
		return Snapshot{}, fmt.Errorf("snapshot %s: %w", abs, err)
	}

	// A backup that still held sessions would be a file that signs people in,
	// and it must never be handed back as good. Anything that goes wrong here
	// takes the snapshot with it.
	if err := clearSnapshotSessions(abs); err != nil {
		os.Remove(abs)
		return Snapshot{}, err
	}

	fi, err := os.Stat(abs)
	if err != nil {
		return Snapshot{}, fmt.Errorf("snapshot %s: %w", abs, err)
	}
	version, err := s.SchemaVersion()
	if err != nil {
		return Snapshot{}, fmt.Errorf("snapshot %s: read schema version: %w", abs, err)
	}
	return Snapshot{
		Path:          abs,
		SchemaVersion: version,
		Bytes:         fi.Size(),
		TakenAt:       time.Now().Unix(),
	}, nil
}

/*
 * clearSnapshotSessions removes the live logins from a freshly written
 * snapshot, so the backup carries records and not credentials.
 *
 * Done here rather than on restore, which is where ADR 0058 first put it. The
 * ADR's own picture of a backup is a file somebody copies to a USB stick — and
 * a file copied and put back by hand never runs restore's code at all, so a
 * restore-time rule would be absent in exactly the case that most wants it.
 * Clearing at snapshot time makes it a property of the file, which travels
 * with the file.
 *
 * `journal_mode(DELETE)` is not a preference. A WAL-journalled write would
 * leave a `-wal` beside the backup, and a snapshot whose contents depend on a
 * sidecar is a snapshot somebody copies half of. VACUUM INTO already produces
 * a delete-journalled database, so this states what is already true rather
 * than changing it — and the rollback journal it does use exists only inside
 * the transaction, leaving one self-contained file.
 *
 * The restored database's journal mode is not affected: Open sets WAL on every
 * open, which is where that decision belongs.
 */
func clearSnapshotSessions(path string) error {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(DELETE)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("snapshot %s: clear sessions: %w", path, err)
	}
	if _, err := db.Exec(`DELETE FROM session`); err != nil {
		db.Close()
		return fmt.Errorf("snapshot %s: clear sessions: %w", path, err)
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("snapshot %s: clear sessions: %w", path, err)
	}
	return nil
}

/*
 * InspectSnapshot reads a backup file without opening it as the live database.
 *
 * Deliberately not Open: that applies the schema and runs migrations, which
 * would *modify the backup* in the course of asking a question about it, and
 * would migrate a file forward that the caller has not yet decided to restore.
 * This opens read-only and reads one row.
 *
 * A snapshot that is too new is returned *with* its details alongside
 * SnapshotTooNewError, so a caller can say which version it found rather than
 * only that something was wrong.
 */
func InspectSnapshot(path string) (Snapshot, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("inspect %s: %w", path, err)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return Snapshot{}, fmt.Errorf("inspect %s: %w", abs, err)
	}

	// mode=ro is not decoration. It means a malformed or half-written file
	// cannot be "recovered" into something worse by the act of looking at it,
	// and that no journal or WAL sidecar appears beside a backup a person is
	// about to copy to a stick.
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=ro", abs))
	if err != nil {
		return Snapshot{}, fmt.Errorf("inspect %s: %w", abs, err)
	}
	defer db.Close()

	snap := Snapshot{Path: abs, Bytes: fi.Size(), TakenAt: fi.ModTime().Unix()}
	err = db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&snap.SchemaVersion)
	if err != nil {
		// Every way of not being a LANcast backup arrives here as the same
		// kind of problem — not a database at all, a database with no meta
		// table, or a meta table with no schema_version — and they deserve the
		// same answer, because none of them can be restored.
		return Snapshot{}, fmt.Errorf("inspect %s: %w (%v)", abs, ErrNotSnapshot, err)
	}
	if snap.SchemaVersion > CurrentSchemaVersion {
		return snap, &SnapshotTooNewError{Found: snap.SchemaVersion, Supported: CurrentSchemaVersion}
	}
	return snap, nil
}
