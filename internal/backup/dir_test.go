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

	_ "modernc.org/sqlite"

	"lancast/internal/store"
)

// writeBackup puts a real snapshot in the folder under the given name.
func writeBackup(t *testing.T, d *Dir, name string) string {
	t.Helper()
	if err := d.Create(); err != nil {
		t.Fatal(err)
	}
	src, err := store.Open(filepath.Join(t.TempDir(), "src.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	path := filepath.Join(d.Path(), name)
	if _, err := src.Snapshot(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	return path
}

/*
 * The containment rule, which is the reason this package exists separately.
 *
 * A name arrives from a URL and becomes a filesystem path. Every way of
 * spelling "somewhere else" has to die here rather than further in, so each
 * one is named individually — a table that passed as a group would not say
 * which spelling had started working.
 */
func TestResolveRefusesEverySpellingOfElsewhere(t *testing.T) {
	d := New(t.TempDir())

	bad := map[string]string{
		"empty":                  "",
		"dot":                    ".",
		"dot dot":                "..",
		"parent traversal":       "../lancast-backup-x.db",
		"windows traversal":      `..\lancast-backup-x.db`,
		"nested":                 "sub/lancast-backup-x.db",
		"absolute unix":          "/etc/lancast-backup-x.db",
		"absolute windows":       `C:\Windows\lancast-backup-x.db`,
		"deep traversal":         "../../../lancast-backup-x.db",
		"traversal inside name":  "lancast-backup-../../x.db",
		"the live database":      "lancast.db",
		"wrong extension":        "lancast-backup-x.txt",
		"wrong prefix":           "notabackup.db",
		"no prefix or extension": "passwd",
	}
	for label, name := range bad {
		if _, err := d.Resolve(name); !errors.Is(err, ErrBadName) {
			t.Errorf("%s (%q): err = %v, want ErrBadName", label, name, err)
		}
	}
}

func TestResolveAcceptsARealName(t *testing.T) {
	dir := t.TempDir()
	d := New(dir)
	name := NewName(time.Now())

	got, err := d.Resolve(name)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "backups", name)
	if got != want {
		t.Errorf("Resolve = %q, want %q", got, want)
	}
}

/*
 * Built from local components. A name derived from UTC reads as tomorrow's
 * backup all evening in every US timezone, and that mistake has shipped in
 * this project before — in a place where it corrupted both writes and
 * "is it today?" reads.
 */
func TestNewNameUsesLocalTime(t *testing.T) {
	// Late enough in the day that UTC is already tomorrow anywhere west of it.
	when := time.Date(2026, 9, 3, 22, 30, 15, 0, time.FixedZone("UTC-7", -7*3600))
	got := NewName(when)

	if want := "lancast-backup-20260903-223015.db"; got != want {
		t.Errorf("NewName = %q, want %q", got, want)
	}
	// And it must be a name Resolve accepts, or backups could be written and
	// never read back.
	if _, err := New(t.TempDir()).Resolve(got); err != nil {
		t.Errorf("NewName produced a name Resolve rejects: %v", err)
	}
}

// A server nobody has taken a backup on has no folder, and that is an empty
// list rather than a failure.
func TestListWithNoFolder(t *testing.T) {
	got, err := New(t.TempDir()).List()
	if err != nil {
		t.Fatalf("List with no folder: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List = %v, want empty", got)
	}
}

func TestListReportsRestorableBackupsNewestFirst(t *testing.T) {
	d := New(t.TempDir())
	older := writeBackup(t, d, "lancast-backup-20260901-120000.db")
	newer := writeBackup(t, d, "lancast-backup-20260903-120000.db")

	// Modification times decide the order, so they are set rather than assumed
	// — both files were written in the same instant by this test.
	os.Chtimes(older, time.Now().Add(-48*time.Hour), time.Now().Add(-48*time.Hour))
	os.Chtimes(newer, time.Now(), time.Now())

	got, err := d.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("List returned %d entries, want 2", len(got))
	}
	if got[0].Name != "lancast-backup-20260903-120000.db" {
		t.Errorf("newest first: got %q", got[0].Name)
	}
	for _, f := range got {
		if !f.Restorable {
			t.Errorf("%s is not restorable: %q", f.Name, f.Problem)
		}
		if f.SchemaVersion != store.CurrentSchemaVersion {
			t.Errorf("%s schema = %d, want %d", f.Name, f.SchemaVersion, store.CurrentSchemaVersion)
		}
		if f.Bytes <= 0 {
			t.Errorf("%s bytes = %d", f.Name, f.Bytes)
		}
	}
}

// Anything that is not a backup is not listed as one.
func TestListIgnoresOtherFiles(t *testing.T) {
	d := New(t.TempDir())
	writeBackup(t, d, "lancast-backup-20260903-120000.db")
	for _, name := range []string{"lancast.db", "notes.txt", "lancast-backup-x.txt"} {
		if err := os.WriteFile(filepath.Join(d.Path(), name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(d.Path(), "lancast-backup-dir.db"), 0o700); err != nil {
		t.Fatal(err)
	}

	got, err := d.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "lancast-backup-20260903-120000.db" {
		t.Fatalf("List = %v, want only the real backup", got)
	}
}

/*
 * A backup that has gone bad is the single most important thing this screen
 * can say, so it is listed and marked rather than quietly omitted. A list that
 * hides it says the opposite of the truth: that everything is fine.
 */
func TestListMarksUnreadableBackupsRatherThanHidingThem(t *testing.T) {
	d := New(t.TempDir())
	if err := d.Create(); err != nil {
		t.Fatal(err)
	}
	corrupt := filepath.Join(d.Path(), "lancast-backup-20260903-120000.db")
	if err := os.WriteFile(corrupt, []byte("this was a backup once"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := d.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("List returned %d entries, want the broken one listed", len(got))
	}
	if got[0].Restorable {
		t.Error("a file that is not a database is reported as restorable")
	}
	if got[0].Problem == "" {
		t.Error("no problem reported for an unreadable backup")
	}
}

// A backup from a newer build is listed, named as such, and not restorable —
// which is the difference between finding out now and finding out mid-restore.
func TestListMarksNewerBackups(t *testing.T) {
	d := New(t.TempDir())
	path := writeBackup(t, d, "lancast-backup-20260903-120000.db")

	db, err := openForWrite(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`,
		store.CurrentSchemaVersion+1); err != nil {
		t.Fatal(err)
	}
	db.Close()

	got, err := d.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("List returned %d entries", len(got))
	}
	if got[0].Restorable {
		t.Error("a backup from a newer build is reported as restorable")
	}
	if !strings.Contains(got[0].Problem, "newer") {
		t.Errorf("problem = %q, does not say it is from a newer build", got[0].Problem)
	}
	if got[0].SchemaVersion != store.CurrentSchemaVersion+1 {
		t.Errorf("schema = %d, want %d", got[0].SchemaVersion, store.CurrentSchemaVersion+1)
	}
}

func TestRemove(t *testing.T) {
	d := New(t.TempDir())
	path := writeBackup(t, d, "lancast-backup-20260903-120000.db")

	if err := d.Remove("lancast-backup-20260903-120000.db"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("the backup is still there")
	}
}

// Remove goes through the same gate, so a name that could delete something
// else fails before anything is touched.
func TestRemoveRefusesEscapingNames(t *testing.T) {
	dir := t.TempDir()
	d := New(dir)
	if err := d.Create(); err != nil {
		t.Fatal(err)
	}
	bystander := filepath.Join(dir, "lancast.db")
	if err := os.WriteFile(bystander, []byte("the live database"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := d.Remove("../lancast.db"); !errors.Is(err, ErrBadName) {
		t.Errorf("err = %v, want ErrBadName", err)
	}
	if _, err := os.Stat(bystander); err != nil {
		t.Error("the live database was deleted by a backup name")
	}
}

// openForWrite is how a test forges a backup from a build that does not exist
// yet. The store deliberately offers no way to write to a snapshot.
func openForWrite(path string) (*sql.DB, error) {
	return sql.Open("sqlite", path)
}
