package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

/*
 * Revision 18 — library_root, and the item's own root (ADR 0034).
 *
 * These build a real revision-17 database, put data in it that a user would
 * mind losing, and then carry it forward — because the interesting failures of
 * this migration are all about data rather than about SQL.
 *
 * The one that would actually have happened: `library.path` carries a UNIQUE
 * constraint, SQLite cannot DROP COLUMN through one, and the rebuild that
 * replaces it drops the `library` table. With `foreign_keys` on — which this
 * store sets — dropping a parent table is a *data* operation: SQLite deletes
 * its rows, and `media_item.library_id` cascades. That is every item, every
 * watch position and every lock, gone during an upgrade nobody asked for.
 */

// openAtRevision17 builds a database exactly as a pre-0034 build left it.
func openAtRevision17(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rev17.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(schemaSQL); err != nil {
		t.Fatalf("schema: %v", err)
	}
	for _, m := range migrations {
		if m.version > 17 {
			break
		}
		if err := applyMigration(db, m); err != nil {
			t.Fatalf("migration %d: %v", m.version, err)
		}
	}
	return db, path
}

// seedRev17 puts two libraries, four items and a watch position in place.
func seedRev17(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`INSERT INTO library (id, name, kind, path, created_at) VALUES
			(1, 'Films', 'movie', '/mnt/films', 100),
			(2, 'Shows', 'show',  '/mnt/shows', 100)`,
		`INSERT INTO media_item (id, library_id, kind, path, title, sort_title, added_at, updated_at) VALUES
			(10, 1, 'movie', '/mnt/films/a.mkv', 'A', 'a', 100, 100),
			(11, 1, 'movie', '/mnt/films/b.mkv', 'B', 'b', 100, 100),
			(12, 2, 'episode', '/mnt/shows/s1e1.mkv', 'S1E1', 's1e1', 100, 100),
			(13, 2, 'episode', '/mnt/shows/s1e2.mkv', 'S1E2', 's1e2', 100, 100)`,
		`INSERT INTO item_lock (item_id, field) VALUES (10, 'title')`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed: %v\n%s", err, q)
		}
	}
	// A resume point, which is the thing a rescan cannot regenerate and the
	// clearest proof the cascade did not fire.
	if _, err := db.Exec(
		`INSERT INTO playback_state (item_id, user_id, position_ms, watched, updated_at)
		 VALUES (10, 'local', 5150, 0, 100)`); err != nil {
		t.Fatalf("seed progress: %v", err)
	}
}

func countRows(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// The one that matters. Everything else in this file is detail.
func TestRevision18KeepsEveryItem(t *testing.T) {
	db, _ := openAtRevision17(t)
	seedRev17(t, db)

	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if n := countRows(t, db, `SELECT COUNT(*) FROM media_item`); n != 4 {
		t.Errorf("media_item count = %d, want 4 — the rebuild cascaded", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM library`); n != 2 {
		t.Errorf("library count = %d, want 2", n)
	}
	var pos int
	if err := db.QueryRow(
		`SELECT position_ms FROM playback_state WHERE item_id = 10 AND user_id = 'local'`).
		Scan(&pos); err != nil {
		t.Fatalf("resume point did not survive: %v", err)
	}
	if pos != 5150 {
		t.Errorf("position_ms = %d, want 5150", pos)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM item_lock`); n != 1 {
		t.Errorf("item_lock count = %d, want 1 — a lock is a user correction", n)
	}
}

func TestRevision18BackfillsOneRootPerLibrary(t *testing.T) {
	db, _ := openAtRevision17(t)
	seedRev17(t, db)
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if n := countRows(t, db, `SELECT COUNT(*) FROM library_root`); n != 2 {
		t.Fatalf("library_root count = %d, want 2", n)
	}
	var path string
	if err := db.QueryRow(
		`SELECT path FROM library_root WHERE library_id = 1`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	if path != "/mnt/films" {
		t.Errorf("root path = %q, want /mnt/films", path)
	}
}

// Every item must point at the root of its own library, not merely at some
// root. This is the column the containment check will read.
func TestRevision18PointsEveryItemAtItsOwnRoot(t *testing.T) {
	db, _ := openAtRevision17(t)
	seedRev17(t, db)
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if n := countRows(t, db, `SELECT COUNT(*) FROM media_item WHERE root_id IS NULL`); n != 0 {
		t.Errorf("%d items have no root", n)
	}
	mismatched := countRows(t, db, `
		SELECT COUNT(*) FROM media_item i
		JOIN library_root r ON r.id = i.root_id
		WHERE r.library_id <> i.library_id`)
	if mismatched != 0 {
		t.Errorf("%d items point at a root belonging to another library", mismatched)
	}
}

// The column is gone, not merely unused — two places holding one truth is what
// this migration exists to prevent.
func TestRevision18DropsTheLibraryPathColumn(t *testing.T) {
	db, _ := openAtRevision17(t)
	seedRev17(t, db)
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.QueryRow(`SELECT path FROM library WHERE id = 1`).Scan(new(string)); err == nil {
		t.Error("library.path still exists")
	}
}

// The rebuild drops and renames a table every other table references. If that
// left a single reference pointing at nothing, this is where it shows.
func TestRevision18LeavesNoDanglingReferences(t *testing.T) {
	db, _ := openAtRevision17(t)
	seedRev17(t, db)
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		t.Error("foreign_key_check reported a violation")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// Foreign keys are turned off around the rebuild. Leaving them off would mean
// the rest of the process enforced nothing — a far worse outcome than the
// migration itself failing.
func TestRevision18RestoresForeignKeyEnforcement(t *testing.T) {
	db, _ := openAtRevision17(t)
	seedRev17(t, db)
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var on int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil {
		t.Fatal(err)
	}
	if on != 1 {
		t.Fatal("foreign keys left disabled after the rebuild")
	}
	// Proven by behaviour as well as by the pragma: an item pointing at no
	// library must be refused.
	if _, err := db.Exec(
		`INSERT INTO media_item (library_id, kind, path, title, sort_title, added_at, updated_at)
		 VALUES (999, 'movie', '/nowhere/x.mkv', 'X', 'x', 100, 100)`); err == nil {
		t.Error("insert with a dangling library_id succeeded")
	}
}

// The property that makes this migration safe to ship on its own: a single-root
// library behaves exactly as it did, through the ordinary Store API.
func TestRevision18SingleRootLibraryIsIndistinguishable(t *testing.T) {
	db, path := openAtRevision17(t)
	seedRev17(t, db)
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open after migrate: %v", err)
	}
	defer st.Close()

	lib, err := st.GetLibrary(t.Context(), 1)
	if err != nil {
		t.Fatalf("GetLibrary: %v", err)
	}
	if lib.Path != "/mnt/films" {
		t.Errorf("Library.Path = %q, want /mnt/films", lib.Path)
	}
	if lib.Name != "Films" || lib.Kind != "movie" {
		t.Errorf("library = %+v", lib)
	}

	libs, err := st.ListLibraries(t.Context())
	if err != nil {
		t.Fatalf("ListLibraries: %v", err)
	}
	if len(libs) != 2 {
		t.Fatalf("ListLibraries returned %d, want 2", len(libs))
	}
	for _, l := range libs {
		if l.Path == "" {
			t.Errorf("library %d has an empty path", l.ID)
		}
	}
}

// A fresh database and a migrated one must end up the same shape, or the next
// migration is written against two different schemas.
func TestFreshDatabaseMatchesMigratedShape(t *testing.T) {
	migrated, path := openAtRevision17(t)
	seedRev17(t, migrated)
	if err := migrate(migrated); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	migrated.Close()

	old, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()

	fresh, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()

	for _, table := range []string{"library", "library_root", "media_item"} {
		if a, b := columnsOf(t, old, table), columnsOf(t, fresh, table); a != b {
			t.Errorf("%s columns differ:\n migrated: %s\n fresh:    %s", table, a, b)
		}
	}
}

func columnsOf(t *testing.T, s *Store, table string) string {
	t.Helper()
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info(?) ORDER BY name`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := ""
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out += n + ","
	}
	return out
}

// Revision 19 — colour on media_stream (ADR 0033).
//
// Additive columns, so the interesting property is that an existing database
// carries forward with its rows intact and reads back as "not probed" rather
// than as SDR.
func TestRevision19AddsColourWithoutTouchingRows(t *testing.T) {
	db, path := openAtRevision17(t)
	seedRev17(t, db)
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM media_item`); n != 4 {
		t.Errorf("media_item = %d, want 4", n)
	}
	db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	v, err := st.SchemaVersion()
	if err != nil || v != CurrentSchemaVersion {
		t.Fatalf("SchemaVersion = %d, %v, want %d", v, err, CurrentSchemaVersion)
	}
	// The columns exist and a pre-existing stream row reads back empty rather
	// than failing to scan.
	if _, err := st.db.ExecContext(t.Context(), `
		INSERT INTO media_stream (item_id, idx, kind, codec) VALUES (10, 0, 'video', 'h264')`); err != nil {
		t.Fatal(err)
	}
	got, err := st.Streams(t.Context(), 10)
	if err != nil {
		t.Fatalf("Streams: %v", err)
	}
	if len(got) != 1 || got[0].ColorTransfer != "" {
		t.Errorf("streams = %+v, want one row with empty colour", got)
	}
}
