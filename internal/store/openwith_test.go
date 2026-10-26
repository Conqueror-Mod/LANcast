package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// oldDatabase leaves a real, non-empty SQLite file at path that has never seen
// LANcast's schema, so opening it migrates from revision 1 — the same path an
// install that predates every migration takes.
func oldDatabase(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE leftover (x INTEGER)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
}

func TestBeforeMigrateSnapshotsTheOldRevision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lancast.db")
	oldDatabase(t, path)

	var from, to int
	var snap Snapshot
	st, err := OpenWith(path, OpenOptions{BeforeMigrate: func(f, tt int, snapshot func(string) (Snapshot, error)) error {
		from, to = f, tt
		var err error
		snap, err = snapshot(filepath.Join(dir, "before.db"))
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if from != 1 || to != CurrentSchemaVersion {
		t.Fatalf("hook saw %d → %d, want 1 → %d", from, to, CurrentSchemaVersion)
	}
	// The snapshot is the database before the migration, not after it.
	if snap.SchemaVersion != 1 {
		t.Fatalf("snapshot is revision %d, want 1", snap.SchemaVersion)
	}
	inspected, err := InspectSnapshot(snap.Path)
	if err != nil || inspected.SchemaVersion != 1 {
		t.Fatalf("snapshot on disk: %+v, %v", inspected, err)
	}
	// And the migration still ran.
	if v, _ := st.SchemaVersion(); v != CurrentSchemaVersion {
		t.Fatalf("after open: revision %d, want %d", v, CurrentSchemaVersion)
	}
}

func TestBeforeMigrateIsNotCalledForANewDatabase(t *testing.T) {
	for name, prepare := range map[string]func(string){
		"missing": func(string) {},
		"empty":   func(p string) { os.WriteFile(p, nil, 0o600) },
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "lancast.db")
			prepare(path)
			called := false
			st, err := OpenWith(path, OpenOptions{BeforeMigrate: func(int, int, func(string) (Snapshot, error)) error {
				called = true
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			st.Close()
			if called {
				t.Fatal("a new install has nothing to protect and must not be snapshotted")
			}
		})
	}
}

func TestBeforeMigrateIsNotCalledWhenCurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lancast.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()

	called := false
	st, err = OpenWith(path, OpenOptions{BeforeMigrate: func(int, int, func(string) (Snapshot, error)) error {
		called = true
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	if called {
		t.Fatal("no migration is pending, so there is nothing to snapshot before")
	}
}

func TestBeforeMigrateErrorStopsTheMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lancast.db")
	oldDatabase(t, path)
	stop := errors.New("no")
	_, err := OpenWith(path, OpenOptions{BeforeMigrate: func(int, int, func(string) (Snapshot, error)) error {
		return stop
	}})
	if !errors.Is(err, stop) {
		t.Fatalf("got %v, want the hook's error", err)
	}

	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v int
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Fatalf("revision %d after a refused open, want 1: nothing should have migrated", v)
	}
}
