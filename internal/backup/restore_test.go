package backup

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// refuseGuard refuses the paths it is given, and records what it was asked.
type refuseGuard struct {
	refuse map[string]bool
	asked  []string
}

func (g *refuseGuard) CheckFile(p string) error {
	g.asked = append(g.asked, filepath.Base(p))
	if g.refuse[filepath.Base(p)] {
		return errors.New("owned by a guest account, not by Administrators or SYSTEM")
	}
	return nil
}

// stagedRestore is a database whose only library is "Live", and a staged
// restore of a backup whose only library is "Planted".
func stagedRestore(t *testing.T) (d *Dir, db, name string) {
	t.Helper()
	dataDir := t.TempDir()
	d = New(dataDir)
	db = liveWith(t, dataDir, "Planted")
	name = NewName(time.Now().Add(-time.Hour))
	snapshotOf(t, d, db, name)
	if err := os.Remove(db); err != nil {
		t.Fatal(err)
	}
	liveWith(t, dataDir, "Live")
	if err := d.Stage(Pending{Name: name, RequestedBy: "someone"}); err != nil {
		t.Fatal(err)
	}
	return d, db, name
}

func assertNotRestored(t *testing.T, d *Dir, db string, out *Outcome) {
	t.Helper()
	if out == nil || out.OK || out.Error == "" {
		t.Fatalf("outcome = %+v, want a refusal with a reason", out)
	}
	if got := libraryNames(t, db); len(got) != 1 || got[0] != "Live" {
		t.Fatalf("the database was replaced: it holds %v", got)
	}
	if d.Staged() != nil {
		t.Error("the refused marker survived, so every start would read it again")
	}
	if last := d.LastRestore(); last == nil || last.OK {
		t.Errorf("last restore = %+v, want the refusal recorded for Settings", last)
	}
}

// R10-2: a marker the server did not write is ignored, whatever it names.
func TestAMarkerTheGuardRefusesIsIgnored(t *testing.T) {
	d, db, _ := stagedRestore(t)
	g := &refuseGuard{refuse: map[string]bool{pendingFile: true}}
	out := d.WithGuard(g).ApplyPending(context.Background(), db, time.Now())
	assertNotRestored(t, d, db, out)
	t.Logf("refused: %s", out.Error)
}

// And a genuine-looking marker naming a backup the server did not write.
func TestABackupTheGuardRefusesIsNotRestored(t *testing.T) {
	d, db, name := stagedRestore(t)
	g := &refuseGuard{refuse: map[string]bool{name: true}}
	out := d.WithGuard(g).ApplyPending(context.Background(), db, time.Now())
	assertNotRestored(t, d, db, out)
	if len(g.asked) != 2 {
		t.Errorf("guard asked about %v, want the marker and the backup", g.asked)
	}
}

// Both accepted: the restore goes ahead exactly as before.
func TestARestoreTheGuardAcceptsIsApplied(t *testing.T) {
	d, db, _ := stagedRestore(t)
	out := d.WithGuard(&refuseGuard{}).ApplyPending(context.Background(), db, time.Now())
	if out == nil || !out.OK {
		t.Fatalf("outcome = %+v", out)
	}
	if got := libraryNames(t, db); len(got) != 1 || got[0] != "Planted" {
		t.Fatalf("after restore the database holds %v", got)
	}
}

// R07-8: the restore is recorded in the database it produced, naming who asked
// and where the previous database was kept. Before, the only record was in the
// database the restore replaced.
func TestARestoreIsAuditedInTheRestoredDatabase(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	d := New(dataDir)
	db := liveWith(t, dataDir, "Before")
	name := NewName(time.Now().Add(-time.Hour))
	snapshotOf(t, d, db, name)
	if err := d.Stage(Pending{Name: name, RequestedBy: "dana", RequestedAt: 1, SafetyBackup: "safety.db"}); err != nil {
		t.Fatal(err)
	}
	out := d.ApplyPending(ctx, db, time.Unix(100, 0))
	if out == nil || !out.OK {
		t.Fatalf("outcome = %+v", out)
	}

	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := RecordOutcome(ctx, st, out); err != nil {
		t.Fatal(err)
	}
	events, _, err := st.ListAudit(ctx, store.AuditFilter{Action: "backup.restore"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("restore audit events = %+v, want one", events)
	}
	ev := events[0]
	if ev.ActorName != "dana" || ev.TargetID != name || ev.At != 100 ||
		!strings.Contains(ev.Summary, "safety.db") || !strings.Contains(ev.Detail, "safety.db") {
		t.Errorf("event = %+v", ev)
	}

	// A failed one is recorded too, as a failure.
	ev = outcomeAudit(&Outcome{Pending: Pending{Name: "x.db"}, Error: "boom"})
	if !strings.Contains(ev.Summary, "did not take") || !strings.Contains(ev.Detail, "boom") || ev.ActorName == "" {
		t.Errorf("failure event = %+v", ev)
	}
}
