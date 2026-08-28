package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

/*
 * Revision 31 — the backfill is a claim about somebody's history.
 *
 * Adding the column is trivial; deciding what it says about rows that predate
 * it is not. A row known to be finished has been finished *at least* once, so
 * one is the honest maximum — it under-reports a film watched twenty times and
 * cannot do otherwise, because nobody recorded those viewings.
 *
 * Asserted against a database that genuinely predates the column, for the same
 * reason revision 27's test is: a default written into a fresh row proves
 * nothing about the upgrade path that real libraries actually take.
 */

func openAtRevision30(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rev30.db")
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
		if m.version > 30 {
			break
		}
		if _, err := db.Exec(m.sql); err != nil {
			t.Fatalf("revision %d: %v", m.version, err)
		}
	}
	if _, err := db.Exec(`UPDATE meta SET value = '30' WHERE key = 'schema_version'`); err != nil {
		t.Fatalf("set version: %v", err)
	}
	return db
}

func TestRevision31BackfillsFromWatched(t *testing.T) {
	db := openAtRevision30(t)

	if _, err := db.Exec(`
		INSERT INTO library (id, name, kind, created_at)
		VALUES (1, 'Films', 'movie', 1)`); err != nil {
		t.Fatalf("seed library: %v", err)
	}
	for id, title := range map[int]string{1: "Friday", 2: "Half Baked", 3: "Never Seen"} {
		if _, err := db.Exec(`
			INSERT INTO media_item (id, library_id, kind, path, title, sort_title, added_at, updated_at)
			VALUES (?, 1, 'movie', ?, ?, ?, 1, 1)`,
			id, fmt.Sprintf(`C:\m\%d.mkv`, id), title, title); err != nil {
			t.Fatalf("seed item: %v", err)
		}
	}

	// Three histories from before the column existed: finished, part-way, and
	// finished by a second account. The part-way row is the one that would be
	// wrong if the backfill were a blanket 1.
	seed := []struct {
		item    int
		user    string
		pos     int64
		watched int
	}{
		{1, "local", 5400000, 1},
		{2, "local", 900000, 0},
		{1, "georgia", 5400000, 1},
	}
	for _, r := range seed {
		if _, err := db.Exec(`
			INSERT INTO playback_state (item_id, user_id, position_ms, watched, updated_at)
			VALUES (?, ?, ?, ?, 1)`, r.item, r.user, r.pos, r.watched); err != nil {
			t.Fatalf("seed progress: %v", err)
		}
	}

	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, want := range []struct {
		item  int
		user  string
		count int
		why   string
	}{
		{1, "local", 1, "a finished film starts its tally at one"},
		{2, "local", 0, "a part-watched film has never been finished"},
		{1, "georgia", 1, "the tally belongs to each account separately"},
	} {
		var got int
		if err := db.QueryRow(
			`SELECT watch_count FROM playback_state WHERE item_id = ? AND user_id = ?`,
			want.item, want.user).Scan(&got); err != nil {
			t.Fatalf("read count: %v", err)
		}
		if got != want.count {
			t.Errorf("item %d for %s: count = %d, want %d — %s",
				want.item, want.user, got, want.count, want.why)
		}
	}

	// An item nobody ever played has no row at all, and the upgrade must not
	// invent one: absence is how "never started" is stored.
	var rows int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM playback_state WHERE item_id = 3`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("the upgrade created %d playback rows for an unplayed film, want 0", rows)
	}
}

// Counting resumes from the backfilled value rather than restarting: somebody
// who had finished a film once and watches it again is on two, not one.
func TestCountingContinuesFromTheBackfill(t *testing.T) {
	db := openAtRevision30(t)
	if _, err := db.Exec(`
		INSERT INTO library (id, name, kind, created_at) VALUES (1, 'Films', 'movie', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO media_item (id, library_id, kind, path, title, sort_title, added_at, updated_at)
		VALUES (1, 1, 'movie', 'C:\m\1.mkv', 'Friday', 'Friday', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO playback_state (item_id, user_id, position_ms, watched, updated_at)
		VALUES (1, 'local', 5400000, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Started again, then finished again — the transition the counter watches.
	if _, err := db.Exec(`UPDATE playback_state SET watched = 0, position_ms = 300 WHERE item_id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		UPDATE playback_state
		SET watch_count = watch_count + 1, watched = 1
		WHERE item_id = 1`); err != nil {
		t.Fatal(err)
	}

	var got int
	if err := db.QueryRow(`SELECT watch_count FROM playback_state WHERE item_id = 1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Errorf("count = %d after a rewatch of a backfilled row, want 2", got)
	}
}
