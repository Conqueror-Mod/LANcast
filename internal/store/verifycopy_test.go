package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// copyLive copies a database and its sidecars while the source is still open,
// which is the worst case a mode switch has to survive: the copy's newest rows
// live only in the -wal that travelled beside it.
func copyLive(t *testing.T, src, dst string) {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		b, err := os.ReadFile(src + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst+suffix, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVerifyCopyFoldsTheLogIntoTheFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	src := filepath.Join(dir, "lancast.db")
	s, err := Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.CreateLibrary(ctx, "Films", "movie", filepath.Join(dir, "media")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(src + "-wal"); err != nil || fi.Size() == 0 {
		t.Fatalf("the test needs rows that live only in the WAL; -wal is %v, %v", fi, err)
	}

	copied := filepath.Join(t.TempDir(), "lancast.db.incoming")
	copyLive(t, src, copied)
	if err := VerifyCopy(copied); err != nil {
		t.Fatalf("VerifyCopy: %v", err)
	}
	if fi, err := os.Stat(copied + "-wal"); err == nil && fi.Size() > 0 {
		t.Errorf("the copy's -wal still holds %d bytes after verifying", fi.Size())
	}

	// Only the database file travels on from here — the rename takes one
	// name. So the library must be in that file alone.
	alone := filepath.Join(t.TempDir(), "lancast.db")
	b, err := os.ReadFile(copied)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alone, b, 0o644); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(alone)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	libs, err := reopened.ListLibraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 1 || libs[0].Name != "Films" {
		t.Errorf("libraries in the verified file = %+v, want the one created before the copy", libs)
	}
}

func TestVerifyCopyRefusesWhatIsNotALibrary(t *testing.T) {
	garbage := filepath.Join(t.TempDir(), "lancast.db.incoming")
	if err := os.WriteFile(garbage, []byte("this is not a database"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyCopy(garbage); err == nil {
		t.Error("VerifyCopy accepted a file that is not a database")
	}

	if err := VerifyCopy(filepath.Join(t.TempDir(), "absent.db")); err == nil {
		t.Error("VerifyCopy accepted a file that does not exist")
	}

	// A database, but not LANcast's.
	other := filepath.Join(t.TempDir(), "other.db")
	db, err := sql.Open("sqlite", other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE t (x)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := VerifyCopy(other); !errors.Is(err, ErrNotSnapshot) {
		t.Errorf("VerifyCopy(foreign database) = %v, want ErrNotSnapshot", err)
	}
}

func TestVerifyCopyRefusesANewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lancast.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`, CurrentSchemaVersion+1); err != nil {
		t.Fatal(err)
	}
	s.Close()

	var tooNew *SnapshotTooNewError
	if err := VerifyCopy(path); !errors.As(err, &tooNew) {
		t.Errorf("VerifyCopy(newer schema) = %v, want SnapshotTooNewError", err)
	}
}
