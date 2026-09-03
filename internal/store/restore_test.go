package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// seedStore makes a database with something in it worth losing, and closes it,
// since a restore is offline by definition.
func seedStore(t *testing.T, dbPath, libraryName string) {
	t.Helper()
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.CreateLibrary(context.Background(), libraryName, "movie",
		filepath.Join(t.TempDir(), libraryName)); err != nil {
		t.Fatal(err)
	}
}

// libraryNames is the cheapest way to ask which database is on disk.
func libraryNames(t *testing.T, dbPath string) []string {
	t.Helper()
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	libs, err := st.ListLibraries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range libs {
		out = append(out, l.Name)
	}
	return out
}

func takeSnapshot(t *testing.T, dbPath, snapPath string) {
	t.Helper()
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.Snapshot(context.Background(), snapPath); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSnapshotReplacesTheDatabase(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lancast.db")
	snapSrc := filepath.Join(dir, "source.db")
	snap := filepath.Join(dir, "backup.db")

	seedStore(t, snapSrc, "From The Backup")
	takeSnapshot(t, snapSrc, snap)
	seedStore(t, db, "Live And Doomed")

	res, err := RestoreSnapshot(context.Background(), db, snap)
	if err != nil {
		t.Fatal(err)
	}

	if got := libraryNames(t, db); len(got) != 1 || got[0] != "From The Backup" {
		t.Fatalf("libraries after restore = %v, want the backup's", got)
	}
	if res.SchemaVersionAfter != CurrentSchemaVersion {
		t.Errorf("schema after = %d, want %d", res.SchemaVersionAfter, CurrentSchemaVersion)
	}

	// The backup must survive being restored from. It is frequently the only
	// copy, and a restore that consumes it is a restore that can happen once.
	if _, err := os.Stat(snap); err != nil {
		t.Errorf("the snapshot did not survive the restore: %v", err)
	}

	// The replaced database is kept, not deleted — the last chance to notice
	// the wrong backup was restored.
	if res.ReplacedPath == "" {
		t.Fatal("restore did not report where the previous database went")
	}
	if got := libraryNames(t, res.ReplacedPath); len(got) != 1 || got[0] != "Live And Doomed" {
		t.Errorf("the replaced database is not the one that was live: %v", got)
	}
}

/*
 * The quiet one. A `-wal` left beside a replaced database belongs to the
 * previous database, and SQLite applies it to the new file on the next open —
 * producing neither database, by way of a restore that reported success.
 */
func TestRestoreMovesSidecarsAside(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lancast.db")
	snapSrc := filepath.Join(dir, "source.db")
	snap := filepath.Join(dir, "backup.db")

	seedStore(t, snapSrc, "Backup")
	takeSnapshot(t, snapSrc, snap)
	seedStore(t, db, "Live")

	// Stand-ins, because a cleanly closed database leaves none behind and the
	// case that matters is the one after a crash, which did.
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.WriteFile(db+suffix, []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := RestoreSnapshot(context.Background(), db, snap); err != nil {
		t.Fatal(err)
	}

	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(db + suffix); err == nil {
			t.Errorf("stale %s survived the restore beside the new database", suffix)
		}
	}
	if got := libraryNames(t, db); len(got) != 1 || got[0] != "Backup" {
		t.Fatalf("libraries after restore = %v", got)
	}
}

// Restoring onto a machine that has never run LANcast is normal, and is most
// of the point of having a backup.
func TestRestoreWithNoExistingDatabase(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lancast.db")
	snapSrc := filepath.Join(dir, "source.db")
	snap := filepath.Join(dir, "backup.db")

	seedStore(t, snapSrc, "Fresh Machine")
	takeSnapshot(t, snapSrc, snap)

	res, err := RestoreSnapshot(context.Background(), db, snap)
	if err != nil {
		t.Fatal(err)
	}
	if res.ReplacedPath != "" {
		t.Errorf("replaced path = %q, want empty when there was nothing to replace", res.ReplacedPath)
	}
	if got := libraryNames(t, db); len(got) != 1 || got[0] != "Fresh Machine" {
		t.Fatalf("libraries = %v", got)
	}
}

/*
 * Sessions are server-side precisely so they can be revoked. A restore handing
 * back the logins the backup was carrying would undo that — anyone holding a
 * cookie from the backup's era would be signed in again.
 */
func TestRestoreClearsSessions(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db := filepath.Join(dir, "lancast.db")
	snapSrc := filepath.Join(dir, "source.db")
	snap := filepath.Join(dir, "backup.db")

	src, err := Open(snapSrc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.CreateUser(ctx, "u_1", "Chris", "hash", "admin"); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"hash-a", "hash-b"} {
		if err := src.CreateSession(ctx, h, "u_1", time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := src.Snapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	src.Close()

	res, err := RestoreSnapshot(ctx, db, snap)
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionsCleared != 2 {
		t.Errorf("sessions cleared = %d, want 2", res.SessionsCleared)
	}

	restored, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	n, err := restored.CountSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("sessions after restore = %d, want 0", n)
	}
	// The accounts themselves must survive. Clearing sessions signs everyone
	// out; it does not remove anybody.
	users, err := restored.CountUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if users != 1 {
		t.Errorf("users after restore = %d, want 1", users)
	}
}

// The schema gate, at the layer where it does the work: refused before
// anything on disk moves, rather than discovered at the next startup.
func TestRestoreRefusesNewerSnapshotAndChangesNothing(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lancast.db")
	snapSrc := filepath.Join(dir, "source.db")
	snap := filepath.Join(dir, "backup.db")

	seedStore(t, snapSrc, "From The Future")
	takeSnapshot(t, snapSrc, snap)
	setSnapshotVersion(t, snap, CurrentSchemaVersion+1)
	seedStore(t, db, "Live And Safe")
	before := fileDigest(t, db)

	_, err := RestoreSnapshot(context.Background(), db, snap)
	var tooNew *SnapshotTooNewError
	if !errors.As(err, &tooNew) {
		t.Fatalf("error = %v, want SnapshotTooNewError", err)
	}
	if after := fileDigest(t, db); after != before {
		t.Error("a refused restore modified the live database")
	}
	if got := libraryNames(t, db); len(got) != 1 || got[0] != "Live And Safe" {
		t.Errorf("the live database changed: %v", got)
	}
	matches, _ := filepath.Glob(db + ".replaced-*")
	if len(matches) != 0 {
		t.Errorf("a refused restore moved the database aside: %v", matches)
	}
}

// An older backup migrates forward. There are no down migrations, so this is
// the direction that has to work.
func TestRestoreMigratesOlderSnapshotForward(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lancast.db")
	snapSrc := filepath.Join(dir, "source.db")
	snap := filepath.Join(dir, "backup.db")

	seedStore(t, snapSrc, "Old But Good")
	takeSnapshot(t, snapSrc, snap)

	res, err := RestoreSnapshot(context.Background(), db, snap)
	if err != nil {
		t.Fatal(err)
	}
	if res.SchemaVersionAfter != CurrentSchemaVersion {
		t.Errorf("schema after = %d, want %d", res.SchemaVersionAfter, CurrentSchemaVersion)
	}
}

// Garbage never reaches the point of replacing anything.
func TestRestoreRejectsNonSnapshotAndChangesNothing(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lancast.db")
	seedStore(t, db, "Live")
	before := fileDigest(t, db)

	junk := filepath.Join(dir, "holiday.jpg")
	if err := os.WriteFile(junk, []byte("not a database at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := RestoreSnapshot(context.Background(), db, junk); !errors.Is(err, ErrNotSnapshot) {
		t.Fatalf("error = %v, want ErrNotSnapshot", err)
	}
	if after := fileDigest(t, db); after != before {
		t.Error("a rejected restore modified the live database")
	}
}

// Restoring the live database over itself would move it aside and then copy it
// from a path that no longer holds anything.
func TestRestoreRefusesTheLiveDatabaseAsItsOwnBackup(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lancast.db")
	seedStore(t, db, "Live")

	_, err := RestoreSnapshot(context.Background(), db, db)
	if err == nil {
		t.Fatal("restoring the live database from itself succeeded")
	}
	if !strings.Contains(err.Error(), "live database") {
		t.Errorf("error %q does not explain what is wrong", err)
	}
	if got := libraryNames(t, db); len(got) != 1 || got[0] != "Live" {
		t.Errorf("the live database changed: %v", got)
	}
}

/*
 * Two restores in the same second must not have the second one overwrite the
 * copy the first one set aside — the updater learned this with `.old`, where a
 * fixed name meant the file kept was not the file wanted.
 */
func TestRestoreKeepsEveryReplacedDatabase(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lancast.db")
	snapSrc := filepath.Join(dir, "source.db")
	snap := filepath.Join(dir, "backup.db")

	seedStore(t, snapSrc, "Backup")
	takeSnapshot(t, snapSrc, snap)
	seedStore(t, db, "First Live")

	first, err := RestoreSnapshot(context.Background(), db, snap)
	if err != nil {
		t.Fatal(err)
	}

	snap2 := filepath.Join(dir, "backup2.db")
	takeSnapshot(t, snapSrc, snap2)
	second, err := RestoreSnapshot(context.Background(), db, snap2)
	if err != nil {
		t.Fatal(err)
	}

	if first.ReplacedPath == second.ReplacedPath {
		t.Fatalf("both restores used the same aside name %q", first.ReplacedPath)
	}
	if _, err := os.Stat(first.ReplacedPath); err != nil {
		t.Errorf("the first restore's replaced database was destroyed: %v", err)
	}
	if got := libraryNames(t, first.ReplacedPath); len(got) != 1 || got[0] != "First Live" {
		t.Errorf("the first replaced database is not what was live: %v", got)
	}
}
