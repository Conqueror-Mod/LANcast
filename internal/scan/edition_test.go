package scan

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"lancast/internal/store"

	_ "modernc.org/sqlite"
)

/*
 * The edition backfill, end to end (ADR 0049).
 *
 * These go through a real scan rather than a fake store, because the property
 * under test is not "the function writes a column" — it is that a row whose
 * file will never change again can still gain its marker. That depends on the
 * scanner *not* upserting unchanged files, which is exactly the behaviour that
 * created the problem, and a fake store cannot exercise it.
 */

// scannerAt is newScanner with the database path handed back, so a test can
// reach the column directly and manufacture the one state the store has no
// method to produce: a row written before the edition column existed.
func scannerAt(t *testing.T) (*Scanner, *store.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(st, log), st, path
}

// forgetEditions makes every row look like one written before revision 29.
func forgetEditions(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE media_item SET edition = NULL`); err != nil {
		t.Fatal(err)
	}
}

func editionsByTitle(t *testing.T, dbPath string) map[string]*string {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT title, edition FROM media_item WHERE kind = 'movie'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	out := map[string]*string{}
	for rows.Next() {
		var title string
		var ed *string
		if err := rows.Scan(&title, &ed); err != nil {
			t.Fatal(err)
		}
		out[title] = ed
	}
	return out
}

/*
 * The whole point, in one test.
 *
 * A library scanned, its markers forgotten, and scanned again — with the files
 * untouched, so the scanner upserts nothing and the only thing that can restore
 * them is the backfill.
 */
func TestALegacyRowGainsItsMarkerOnTheNextScan(t *testing.T) {
	ctx := context.Background()
	sc, st, dbPath := scannerAt(t)
	root := t.TempDir()

	writeFile(t, root, "Aliens SE (1986)/Aliens SE.mkv", 1000)
	writeFile(t, root, "Alien (1979)/Alien.mkv", 1000)

	lib, err := st.CreateLibrary(ctx, "Media", "movie", root)
	if err != nil {
		t.Fatal(err)
	}
	scanAndWait(t, sc, *lib)

	// The state every library that predates revision 29 is in.
	forgetEditions(t, dbPath)

	// Nothing on disk moved, so the scanner upserts nothing -- which is the
	// whole reason these rows cannot heal themselves.
	scanAndWait(t, sc, *lib)

	got := editionsByTitle(t, dbPath)
	aliens, ok := got["Aliens"]
	if !ok {
		t.Fatalf("no row titled Aliens; got %v", editionKeys(got))
	}
	if aliens == nil {
		t.Fatal("Aliens is still NULL — a row whose file never changes again can never gain its marker")
	}
	if *aliens != "SE" {
		t.Errorf("Aliens edition = %q, want SE", *aliens)
	}

	// And the film with no marker keeps none. NULL is how "no marker" is spelt
	// (ADR 0042), so this row is indistinguishable from one never read — which is
	// the trade the backfill accepts rather than changing an API contract.
	alien, ok := got["Alien"]
	if !ok {
		t.Fatalf("no row titled Alien; got %v", editionKeys(got))
	}
	if alien != nil {
		t.Errorf("Alien edition = %q, want no marker at all", *alien)
	}
}

// editionKeys is spelt out rather than reusing keys(), which is typed for
// store.Item -- this map holds the raw column, nulls included.
func editionKeys(m map[string]*string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
