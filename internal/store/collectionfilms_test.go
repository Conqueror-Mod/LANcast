package store

import (
	"context"
	"testing"
	"time"
)

/*
 * A collection's film list (ADR 0093): which films are owned is decided at
 * read time against the library, missing films are shown, and a rating
 * ceiling shows none of them.
 */

type filmsFixture struct {
	st   *Store
	lib  *Library
	coll int64
	hulk int64 // owned, matched to tmdb 1
}

func newFilmsFixture(t *testing.T) filmsFixture {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	if _, err := st.UpsertItem(ctx, file(lib.ID, `C:\m\hulk.mkv`, "Hulk")); err != nil {
		t.Fatal(err)
	}
	known, _ := st.KnownFiles(ctx, lib.ID)
	hulk := known[`C:\m\hulk.mkv`].ID
	if err := st.SetMatch(ctx, hulk, "tmdb", "1", "matched", 1); err != nil {
		t.Fatal(err)
	}
	coll, _, err := st.EnsureCollection(ctx, lib.ID, "tmdb", "133352", "Hulk Collection", "hulk collection")
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().AddDate(1, 0, 0).Unix()
	if err := st.ReplaceCollectionFilms(ctx, coll, "tmdb", []CollectionFilm{
		{ExternalID: "1", Title: "Hulk", Year: 2003, ReleasedAt: 1056067200},
		{ExternalID: "2", Title: "The Incredible Hulk", Year: 2008, ReleasedAt: 1213228800},
		{ExternalID: "3", Title: "Hulk Again", Year: 2027, ReleasedAt: future},
	}); err != nil {
		t.Fatal(err)
	}
	return filmsFixture{st: st, lib: lib, coll: coll, hulk: hulk}
}

func TestACollectionListsWhatIsOwnedAndWhatIsMissing(t *testing.T) {
	f := newFilmsFixture(t)
	films, err := f.st.CollectionFilmsFor(context.Background(), f.coll, "local")
	if err != nil {
		t.Fatal(err)
	}
	if len(films) != 3 {
		t.Fatalf("films = %+v, want all three", films)
	}
	if films[0].ItemID != f.hulk || films[0].Upcoming {
		t.Errorf("Hulk = %+v, want owned by item %d and released", films[0], f.hulk)
	}
	if films[1].ItemID != 0 || films[1].Upcoming {
		t.Errorf("The Incredible Hulk = %+v, want missing and released", films[1])
	}
	if !films[2].Upcoming {
		t.Errorf("a film dated next year = %+v, want upcoming", films[2])
	}
}

// Owned is a read-time answer: a film matched after the list was stored is
// owned at once, with no list refresh.
func TestAFilmAddedLaterIsOwnedAtOnce(t *testing.T) {
	f := newFilmsFixture(t)
	ctx := context.Background()
	if _, err := f.st.UpsertItem(ctx, file(f.lib.ID, `C:\m\ih.mkv`, "The Incredible Hulk")); err != nil {
		t.Fatal(err)
	}
	known, _ := f.st.KnownFiles(ctx, f.lib.ID)
	ih := known[`C:\m\ih.mkv`].ID
	if err := f.st.SetMatch(ctx, ih, "tmdb", "2", "matched", 1); err != nil {
		t.Fatal(err)
	}
	films, _ := f.st.CollectionFilmsFor(ctx, f.coll, "local")
	if films[1].ItemID != ih {
		t.Errorf("The Incredible Hulk = %+v, want owned by %d", films[1], ih)
	}
	// And it no longer needs a poster of its own: it has one.
	need, err := f.st.CollectionFilmsNeedingPosters(ctx, f.coll, "tmdb")
	if err != nil {
		t.Fatal(err)
	}
	if len(need) != 1 || need[0] != "3" {
		t.Errorf("needing posters = %v, want only the missing film 3", need)
	}
}

// A rating ceiling shows the owned films it permits, and no missing ones.
func TestARestrictedAccountIsShownNoMissingFilms(t *testing.T) {
	f := newFilmsFixture(t)
	ctx := context.Background()
	if _, err := f.st.db.Exec(`UPDATE media_item SET content_rating = 'PG-13' WHERE id = ?`, f.hulk); err != nil {
		t.Fatal(err)
	}
	u, err := f.st.CreateUser(ctx, "", "child", "hash", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetMaxContentRating(ctx, u.ID, "PG-13"); err != nil {
		t.Fatal(err)
	}
	films, err := f.st.CollectionFilmsFor(ctx, f.coll, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(films) != 1 || films[0].ItemID != f.hulk {
		t.Errorf("restricted account sees %+v, want only the owned, permitted film", films)
	}

	// Below the owned film's certificate, it goes too.
	if err := f.st.SetMaxContentRating(ctx, u.ID, "PG"); err != nil {
		t.Fatal(err)
	}
	films, _ = f.st.CollectionFilmsFor(ctx, f.coll, u.ID)
	if len(films) != 0 {
		t.Errorf("a film above the ceiling is listed: %+v", films)
	}
}

// A refresh replaces the list whole and keeps a poster already downloaded.
func TestARefreshReplacesTheListAndKeepsPosters(t *testing.T) {
	f := newFilmsFixture(t)
	ctx := context.Background()
	const h = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := f.st.SetCollectionFilmPoster(ctx, f.coll, "tmdb", "2", h); err != nil {
		t.Fatal(err)
	}
	if err := f.st.ReplaceCollectionFilms(ctx, f.coll, "tmdb", []CollectionFilm{
		{ExternalID: "1", Title: "Hulk", ReleasedAt: 1056067200},
		{ExternalID: "2", Title: "The Incredible Hulk", ReleasedAt: 1213228800},
	}); err != nil {
		t.Fatal(err)
	}
	films, _ := f.st.CollectionFilmsFor(ctx, f.coll, "local")
	if len(films) != 2 {
		t.Fatalf("films = %+v, want the dropped sequel gone", films)
	}
	if films[1].Poster != h {
		t.Errorf("poster = %q, want the one already downloaded kept", films[1].Poster)
	}
	// And the prune keeps that file: nothing else names it.
	live, err := f.st.ReferencedArtworkHashes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !live[h] {
		t.Error("a missing film's poster is not counted as referenced, so the prune would delete it")
	}
}

// Old collections are found for the backfill; keyword groupings never are.
func TestCollectionsNeedingFilmsSkipsKeywordsAndListedOnes(t *testing.T) {
	f := newFilmsFixture(t)
	ctx := context.Background()
	old, _, err := f.st.EnsureCollection(ctx, f.lib.ID, "tmdb", "999", "Old", "old")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.st.EnsureCollection(ctx, f.lib.ID, "tmdb", "keyword:180547", "MCU", "mcu"); err != nil {
		t.Fatal(err)
	}
	need, err := f.st.CollectionsNeedingFilms(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(need) != 1 || need[0].ID != old || need[0].ExternalID != "999" {
		t.Errorf("needing films = %+v, want only the old TMDB collection", need)
	}
}
