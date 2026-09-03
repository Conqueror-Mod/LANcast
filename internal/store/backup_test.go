package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fileDigest is how "inspecting did not modify it" is asserted: a hash, not a
// timestamp, because a filesystem's mtime granularity is coarse enough to hide
// a write that happened in the same tick.
func fileDigest(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestSnapshotRoundTrips(t *testing.T) {
	ctx := context.Background()
	src := openTestStore(t)
	lib, err := src.CreateLibrary(ctx, "Films", "movie", filepath.Join(t.TempDir(), "media"))
	if err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "lancast-backup.db")
	snap, err := src.Snapshot(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Bytes <= 0 {
		t.Errorf("snapshot reported %d bytes", snap.Bytes)
	}
	if snap.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("snapshot schema version = %d, want %d", snap.SchemaVersion, CurrentSchemaVersion)
	}

	// The real assertion: the file is a working LANcast database, not merely a
	// file of about the right size.
	restored, err := Open(dst)
	if err != nil {
		t.Fatalf("reopen snapshot: %v", err)
	}
	defer restored.Close()
	libs, err := restored.ListLibraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 1 || libs[0].Name != lib.Name {
		t.Fatalf("snapshot lost the library: %+v", libs)
	}
}

// A snapshot is taken from a server that is *running*, which is the entire
// reason for VACUUM INTO over a file copy. This writes throughout.
func TestSnapshotIsConsistentUnderConcurrentWrites(t *testing.T) {
	ctx := context.Background()
	src := openTestStore(t)
	dir := t.TempDir()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			// Errors are not fatal here — the goroutine exists to keep the
			// source busy, not to assert anything about writing.
			_, _ = src.CreateLibrary(ctx, fmt.Sprintf("Lib %d", i), "movie",
				filepath.Join(dir, fmt.Sprintf("root%d", i)))
		}
	}()
	time.Sleep(20 * time.Millisecond)

	dst := filepath.Join(dir, "busy.db")
	_, err := src.Snapshot(ctx, dst)
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatalf("snapshot under load: %v", err)
	}

	restored, err := Open(dst)
	if err != nil {
		t.Fatalf("snapshot taken under load is not a usable database: %v", err)
	}
	defer restored.Close()
	// A torn copy passes "it opened" and fails an integrity check, which is
	// precisely the failure this test exists to catch.
	var result string
	if err := restored.db.QueryRow(`PRAGMA integrity_check`).Scan(&result); err != nil {
		t.Fatal(err)
	}
	if result != "ok" {
		t.Fatalf("integrity_check = %q, want ok", result)
	}
}

func TestSnapshotNeverOverwrites(t *testing.T) {
	ctx := context.Background()
	src := openTestStore(t)
	dst := filepath.Join(t.TempDir(), "once.db")

	if _, err := src.Snapshot(ctx, dst); err != nil {
		t.Fatal(err)
	}
	before := fileDigest(t, dst)

	_, err := src.Snapshot(ctx, dst)
	if !errors.Is(err, ErrSnapshotExists) {
		t.Fatalf("second snapshot error = %v, want ErrSnapshotExists", err)
	}
	if after := fileDigest(t, dst); after != before {
		t.Error("the refused snapshot modified the existing backup")
	}
}

func TestSnapshotIntoMissingDirectorySaysWhich(t *testing.T) {
	ctx := context.Background()
	src := openTestStore(t)
	missing := filepath.Join(t.TempDir(), "no-such-dir")

	_, err := src.Snapshot(ctx, filepath.Join(missing, "b.db"))
	if err == nil {
		t.Fatal("snapshot into a missing directory succeeded")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q does not name the missing directory %q", err, missing)
	}
}

// Backup destinations are chosen by a person, so they have spaces in them.
func TestSnapshotPathWithSpaces(t *testing.T) {
	ctx := context.Background()
	src := openTestStore(t)
	dir := filepath.Join(t.TempDir(), "My Backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "LANcast backup 1.db")

	if _, err := src.Snapshot(ctx, dst); err != nil {
		t.Fatalf("snapshot to a path with spaces: %v", err)
	}
	snap, err := InspectSnapshot(dst)
	if err != nil {
		t.Fatalf("inspect a path with spaces: %v", err)
	}
	if snap.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("schema version = %d, want %d", snap.SchemaVersion, CurrentSchemaVersion)
	}
}

func TestInspectSnapshotReadsVersionWithoutModifying(t *testing.T) {
	ctx := context.Background()
	src := openTestStore(t)
	dst := filepath.Join(t.TempDir(), "read.db")
	if _, err := src.Snapshot(ctx, dst); err != nil {
		t.Fatal(err)
	}
	before := fileDigest(t, dst)

	snap, err := InspectSnapshot(dst)
	if err != nil {
		t.Fatal(err)
	}
	if snap.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("schema version = %d, want %d", snap.SchemaVersion, CurrentSchemaVersion)
	}
	if snap.Bytes <= 0 {
		t.Errorf("bytes = %d", snap.Bytes)
	}

	// Open would have applied the schema and migrated. Inspecting must not.
	if after := fileDigest(t, dst); after != before {
		t.Error("inspecting the backup modified it")
	}
	for _, sidecar := range []string{dst + "-wal", dst + "-shm", dst + "-journal"} {
		if _, err := os.Stat(sidecar); err == nil {
			t.Errorf("inspecting left %s beside the backup", filepath.Base(sidecar))
		}
	}
}

// The schema gate. An older backup migrates forward and is fine; a newer one
// cannot be restored by this build, and must say so before anything is moved.
func TestInspectSnapshotRefusesNewerSchema(t *testing.T) {
	ctx := context.Background()
	src := openTestStore(t)
	dst := filepath.Join(t.TempDir(), "future.db")
	if _, err := src.Snapshot(ctx, dst); err != nil {
		t.Fatal(err)
	}
	setSnapshotVersion(t, dst, CurrentSchemaVersion+1)

	snap, err := InspectSnapshot(dst)
	var tooNew *SnapshotTooNewError
	if !errors.As(err, &tooNew) {
		t.Fatalf("error = %v, want SnapshotTooNewError", err)
	}
	if tooNew.Found != CurrentSchemaVersion+1 || tooNew.Supported != CurrentSchemaVersion {
		t.Errorf("error = %+v, want found %d supported %d", tooNew, CurrentSchemaVersion+1, CurrentSchemaVersion)
	}
	// Returned alongside the error so a caller can report what it found rather
	// than only that something was wrong.
	if snap.SchemaVersion != CurrentSchemaVersion+1 {
		t.Errorf("snapshot details lost: %+v", snap)
	}
	if !strings.Contains(tooNew.Error(), "upgrade LANcast") {
		t.Errorf("message %q does not say what to do", tooNew.Error())
	}
}

func TestInspectSnapshotAcceptsOlderSchema(t *testing.T) {
	ctx := context.Background()
	src := openTestStore(t)
	dst := filepath.Join(t.TempDir(), "old.db")
	if _, err := src.Snapshot(ctx, dst); err != nil {
		t.Fatal(err)
	}
	setSnapshotVersion(t, dst, 2)

	snap, err := InspectSnapshot(dst)
	if err != nil {
		t.Fatalf("an older backup must be restorable: %v", err)
	}
	if snap.SchemaVersion != 2 {
		t.Errorf("schema version = %d, want 2", snap.SchemaVersion)
	}
}

func TestInspectSnapshotRejectsNonDatabases(t *testing.T) {
	dir := t.TempDir()
	junk := filepath.Join(dir, "holiday.jpg")
	if err := os.WriteFile(junk, []byte("not a database at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectSnapshot(junk); !errors.Is(err, ErrNotSnapshot) {
		t.Errorf("error = %v, want ErrNotSnapshot", err)
	}

	// A real SQLite database that is not a LANcast one fails the same way, and
	// should: it cannot be restored either.
	stranger := filepath.Join(dir, "stranger.db")
	db, err := sql.Open("sqlite", stranger)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE t (a INTEGER)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := InspectSnapshot(stranger); !errors.Is(err, ErrNotSnapshot) {
		t.Errorf("foreign database: error = %v, want ErrNotSnapshot", err)
	}
}

func TestInspectSnapshotMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone.db")
	_, err := InspectSnapshot(missing)
	if err == nil {
		t.Fatal("inspecting a missing file succeeded")
	}
	if errors.Is(err, ErrNotSnapshot) {
		t.Error("a missing file is not the same problem as a corrupt one")
	}
}

/*
 * A backup carries records, not credentials.
 *
 * Sessions are server-side precisely so they can be revoked, and a backup that
 * held them would be a file that signs people in — anyone holding a cookie
 * from the backup's era would be back in the moment it was restored.
 *
 * Cleared at snapshot time rather than at restore time, because the ADR's own
 * picture of a backup is a file somebody copies to a stick, and a file put
 * back by hand never runs restore's code. The property has to belong to the
 * file to be true when it matters.
 */
func TestSnapshotCarriesNoSessions(t *testing.T) {
	ctx := context.Background()
	src := openTestStore(t)
	if _, err := src.CreateUser(ctx, "u_1", "Chris", "hash", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := src.CreateSession(ctx, "tokenhash", "u_1", time.Hour); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "withsession.db")
	if _, err := src.Snapshot(ctx, dst); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()

	var n int
	if err := restored.db.QueryRow(`SELECT COUNT(*) FROM session`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("session rows in snapshot = %d, want 0", n)
	}
	// The accounts must survive. A backup that lost them would be a backup
	// that cannot be restored onto a working server.
	users, err := restored.CountUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if users != 1 {
		t.Errorf("users in snapshot = %d, want 1", users)
	}

	// The live database is untouched — taking a backup must not sign anybody
	// out of the server they are using.
	live, err := src.CountSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if live != 1 {
		t.Errorf("sessions on the live server after a backup = %d, want 1", live)
	}
}

// Clearing reopens the snapshot to write to it, and a backup whose contents
// depend on a sidecar is a backup somebody copies half of.
func TestSnapshotIsOneSelfContainedFile(t *testing.T) {
	ctx := context.Background()
	src := openTestStore(t)
	if _, err := src.CreateUser(ctx, "u_1", "Chris", "hash", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := src.CreateSession(ctx, "tokenhash", "u_1", time.Hour); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "alone.db")
	if _, err := src.Snapshot(ctx, dst); err != nil {
		t.Fatal(err)
	}
	for _, sidecar := range []string{dst + "-wal", dst + "-shm", dst + "-journal"} {
		if _, err := os.Stat(sidecar); err == nil {
			t.Errorf("taking a backup left %s beside it", filepath.Base(sidecar))
		}
	}
}

// setSnapshotVersion rewrites a backup's recorded revision, which is the only
// way to build a backup from a build that does not exist yet.
func setSnapshotVersion(t *testing.T, path string, version int) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`, version); err != nil {
		t.Fatal(err)
	}
}
