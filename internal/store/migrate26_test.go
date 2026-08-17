package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

/*
 * Revision 26 — undoing the seasons that were matched by name.
 *
 * The data this migration exists for is not hypothetical: a real library had
 * nine season-2 rows, across nine unrelated shows, all carrying the title,
 * year and poster of one Thai drama whose name happens to contain "season 2".
 * The season's own name was the search query, so the winner depended only on
 * the season number.
 *
 * These tests build that state and assert both halves of the cleanup: the wrong
 * identity is gone and the row is queued to be resolved again, and a season a
 * person has edited is left exactly as they left it.
 */

// openAtRevision25 builds a database as the build before this fix left it.
func openAtRevision25(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rev25.db")
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
		if m.version > 25 {
			break
		}
		if err := applyMigration(db, m); err != nil {
			t.Fatalf("migration %d: %v", m.version, err)
		}
	}
	return db
}

// seedNameMatchedSeasons lays down two shows, each with a season 2 wrongly
// matched to the same drama, plus its artwork rows.
func seedNameMatchedSeasons(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`INSERT INTO library (id, name, kind, created_at) VALUES
			(1, 'Shows', 'show', 100)`,
		`INSERT INTO media_item
			(id, library_id, kind, path, title, sort_title, added_at, updated_at) VALUES
			(10, 1, 'show', '/mnt/shows/The League',  'The League',  'league', 100, 100),
			(11, 1, 'show', '/mnt/shows/Black Books', 'Black Books', 'black books', 100, 100)`,
		// Both season 2 rows carry the same wrong show's identity and artwork.
		`INSERT INTO media_item
			(id, library_id, kind, path, title, sort_title, season, parent_id,
			 year, overview, series, provider, external_id, match_state, match_score,
			 metadata_updated_at, added_at, updated_at) VALUES
			(20, 1, 'season', '/mnt/shows/The League/S02',  'A Drama season 2', 'a drama season 2', 2, 10,
			 2019, 'Not this show.', 'A Drama season 2', 'tmdb', '92299', 'matched', 0.905, 200, 100, 100),
			(21, 1, 'season', '/mnt/shows/Black Books/S02', 'A Drama season 2', 'a drama season 2', 2, 11,
			 2019, 'Not this show.', 'A Drama season 2', 'tmdb', '92299', 'matched', 0.905, 200, 100, 100)`,
		`INSERT INTO artwork (id, hash, kind, source_url, created_at) VALUES
			(1, 'e9955a93', 'poster', 'https://img/wrong.jpg', 100)`,
		`INSERT INTO item_artwork (item_id, artwork_id, kind, selected) VALUES
			(20, 1, 'poster', 1),
			(21, 1, 'poster', 1)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed: %v\n%s", err, s)
		}
	}
}

func TestRevision26ClearsSeasonsMatchedByName(t *testing.T) {
	db := openAtRevision25(t)
	seedNameMatchedSeasons(t, db)

	if err := applyMigration(db, migration{version: 26, sql: schemaRevision26}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, id := range []int64{20, 21} {
		var title, sortTitle, state string
		var provider, external, overview, series sql.NullString
		var year, stamp sql.NullInt64
		err := db.QueryRow(`
			SELECT title, sort_title, match_state, provider, external_id,
			       overview, series, year, metadata_updated_at
			FROM media_item WHERE id = ?`, id).
			Scan(&title, &sortTitle, &state, &provider, &external,
				&overview, &series, &year, &stamp)
		if err != nil {
			t.Fatalf("read season %d: %v", id, err)
		}

		if title != "Season 2" {
			t.Errorf("season %d titled %q, want the position it actually is", id, title)
		}
		// Zero-padded: the default listing order leads with sort_title, and
		// "Season 10" sorts before "Season 2" as text.
		if sortTitle != "season 002" {
			t.Errorf("season %d sort_title = %q, want a numeric key", id, sortTitle)
		}
		if provider.Valid || external.Valid {
			t.Errorf("season %d still claims %v/%v", id, provider, external)
		}
		if overview.Valid || series.Valid || year.Valid {
			t.Errorf("season %d kept the wrong show's metadata: %v %v %v", id, overview, series, year)
		}
		if state != "unmatched" {
			t.Errorf("season %d match_state = %q, want unmatched", id, state)
		}
		// The stamp is what puts the row back in front of the worker. Without
		// it nothing would ever revisit these rows: seasons are not offered for
		// review and a stamped row is not pending.
		if stamp.Valid {
			t.Errorf("season %d still stamped enriched; it will never be resolved", id)
		}
	}

	var art int
	if err := db.QueryRow(`SELECT COUNT(*) FROM item_artwork WHERE item_id IN (20, 21)`).Scan(&art); err != nil {
		t.Fatal(err)
	}
	if art != 0 {
		t.Errorf("%d artwork links survived; the wrong poster is still on screen", art)
	}
}

// A season a person has edited is their decision. A cleanup is a rescan-class
// event, and a rescan reconciles files — it does not re-litigate decisions
// (CLAUDE.md).
func TestRevision26LeavesEditedSeasonsAlone(t *testing.T) {
	db := openAtRevision25(t)
	seedNameMatchedSeasons(t, db)

	if _, err := db.Exec(
		`INSERT INTO item_lock (item_id, field) VALUES (20, 'title')`); err != nil {
		t.Fatalf("lock: %v", err)
	}

	if err := applyMigration(db, migration{version: 26, sql: schemaRevision26}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var title string
	var external sql.NullString
	if err := db.QueryRow(
		`SELECT title, external_id FROM media_item WHERE id = 20`).Scan(&title, &external); err != nil {
		t.Fatal(err)
	}
	if title != "A Drama season 2" || !external.Valid {
		t.Errorf("locked season was rewritten: title %q, external %v", title, external)
	}

	// The unlocked one alongside it is still cleaned.
	var other string
	if err := db.QueryRow(`SELECT title FROM media_item WHERE id = 21`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if other != "Season 2" {
		t.Errorf("unlocked season title = %q, want Season 2", other)
	}
}
