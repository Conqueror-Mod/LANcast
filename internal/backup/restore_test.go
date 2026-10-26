package backup

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lancast/internal/store"
)

// liveWith makes a live database at dataDir/lancast.db holding one library
// named name, so a test can tell which database it is looking at.
func liveWith(t *testing.T, dataDir, name string) string {
	t.Helper()
	path := filepath.Join(dataDir, "lancast.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.CreateLibrary(context.Background(), name, "movie", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	return path
}

func libraryNames(t *testing.T, path string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT name FROM library ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		out = append(out, n)
	}
	return out
}

func snapshotOf(t *testing.T, d *Dir, dbPath, name string) {
	t.Helper()
	if err := d.Create(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.Snapshot(context.Background(), filepath.Join(d.Path(), name)); err != nil {
		t.Fatal(err)
	}
}

func TestAStagedRestoreIsAppliedOnceAtTheNextStart(t *testing.T) {
	dataDir := t.TempDir()
	d := New(dataDir)
	db := liveWith(t, dataDir, "Before")
	name := NewName(time.Now().Add(-time.Hour))
	snapshotOf(t, d, db, name)

	// The library changes after the backup was taken.
	st, _ := store.Open(db)
	st.CreateLibrary(context.Background(), "After", "movie", t.TempDir())
	st.Close()

	if err := d.Stage(Pending{Name: name, RequestedBy: "dana", RequestedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if p := d.Staged(); p == nil || p.Name != name {
		t.Fatalf("staged = %+v", p)
	}

	out := d.ApplyPending(context.Background(), db, time.Unix(100, 0))
	if out == nil || !out.OK {
		t.Fatalf("outcome = %+v", out)
	}
	if got := libraryNames(t, db); len(got) != 1 || got[0] != "Before" {
		t.Fatalf("after restore the database holds %v, want the backup's [Before]", got)
	}
	if out.RequestedBy != "dana" || out.AppliedAt != 100 || out.SchemaTo != store.CurrentSchemaVersion {
		t.Fatalf("outcome = %+v", out)
	}

	// Recorded for Settings, and not applied twice.
	if last := d.LastRestore(); last == nil || !last.OK || last.Name != name {
		t.Fatalf("last restore = %+v", last)
	}
	if d.Staged() != nil {
		t.Fatal("the marker survived the restore")
	}
	if again := d.ApplyPending(context.Background(), db, time.Now()); again != nil {
		t.Fatalf("a second start restored again: %+v", again)
	}
}

// A restore that fails must not be retried on every start: the marker goes
// first, and the database that was there stays.
func TestAFailedRestoreIsNotRetriedAndLeavesTheDatabase(t *testing.T) {
	dataDir := t.TempDir()
	d := New(dataDir)
	db := liveWith(t, dataDir, "Live")
	name := NewName(time.Now())
	snapshotOf(t, d, db, name)
	if err := d.Stage(Pending{Name: name}); err != nil {
		t.Fatal(err)
	}
	// The backup goes bad between staging and the restart.
	if err := os.WriteFile(filepath.Join(d.Path(), name), []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := d.ApplyPending(context.Background(), db, time.Now())
	if out == nil || out.OK || out.Error == "" {
		t.Fatalf("outcome = %+v, want a failure with a reason", out)
	}
	if got := libraryNames(t, db); len(got) != 1 || got[0] != "Live" {
		t.Fatalf("the live database was lost: %v", got)
	}
	if d.Staged() != nil {
		t.Fatal("a failed restore is still staged, so it would run on every start")
	}
	if last := d.LastRestore(); last == nil || last.OK {
		t.Fatalf("last restore = %+v, want the failure recorded", last)
	}
}

func TestStageRefusesWhatCannotBeRestored(t *testing.T) {
	d := New(t.TempDir())
	if err := d.Stage(Pending{Name: "../lancast.db"}); !errors.Is(err, ErrBadName) {
		t.Errorf("escaping name: %v", err)
	}
	if err := d.Stage(Pending{Name: NewName(time.Now())}); err == nil {
		t.Error("a backup that does not exist was staged")
	}
	if d.Staged() != nil {
		t.Error("a refused stage left a marker")
	}
}

func TestUnstage(t *testing.T) {
	dataDir := t.TempDir()
	d := New(dataDir)
	db := liveWith(t, dataDir, "Live")
	name := NewName(time.Now())
	snapshotOf(t, d, db, name)
	if err := d.Unstage(); !errors.Is(err, ErrNothingStaged) {
		t.Fatalf("unstage with nothing staged: %v", err)
	}
	if err := d.Stage(Pending{Name: name}); err != nil {
		t.Fatal(err)
	}
	if err := d.Unstage(); err != nil {
		t.Fatal(err)
	}
	if d.ApplyPending(context.Background(), db, time.Now()) != nil {
		t.Fatal("an unstaged restore was applied")
	}
}

// The marker and the result sit in the backups folder; neither is a backup.
func TestTheMarkerIsNotListedAsABackup(t *testing.T) {
	dataDir := t.TempDir()
	d := New(dataDir)
	db := liveWith(t, dataDir, "Live")
	name := NewName(time.Now())
	snapshotOf(t, d, db, name)
	d.Stage(Pending{Name: name})
	files, err := d.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != name {
		t.Fatalf("listed %+v", files)
	}
}
