package store

import (
	"context"
	"testing"
)

// afterwardsLibrary is a franchise of three and four films outside it, with
// genres and credits chosen so each rule has exactly one film that tests it.
type afterwardsLibrary struct {
	st                        *Store
	coll                      int64
	one, two, three           int64
	byDirector, twoGenres     int64
	oneGenre, nothingInCommon int64
	sameFilm                  int64
}

func newAfterwardsLibrary(t *testing.T) afterwardsLibrary {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	add := func(path, title string, year int, genres []string, credits ...Credit) int64 {
		f := file(lib.ID, path, title)
		f.Year = &year
		if _, err := st.UpsertItem(ctx, f); err != nil {
			t.Fatal(err)
		}
		known, _ := st.KnownFiles(ctx, lib.ID)
		id := known[path].ID
		if err := st.ReplaceGenres(ctx, id, genres); err != nil {
			t.Fatal(err)
		}
		if len(credits) > 0 {
			if err := st.ReplaceCredits(ctx, id, "tmdb", credits); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	dir := Credit{Name: "A Director", Role: "director"}
	lead := Credit{Name: "A Lead", Role: "actor", Order: 0}

	l := afterwardsLibrary{st: st}
	l.one = add(`C:\m\s1.mkv`, "Saga 1", 2001, []string{"Horror", "Thriller"}, dir, lead)
	l.two = add(`C:\m\s2.mkv`, "Saga 2", 2003, []string{"Horror", "Thriller"}, dir, lead)
	l.three = add(`C:\m\s3.mkv`, "Saga 3", 2005, []string{"Horror", "Thriller"}, dir, lead)
	l.byDirector = add(`C:\m\d.mkv`, "Same Director", 2010, []string{"Western"}, dir)
	l.twoGenres = add(`C:\m\g2.mkv`, "Two Genres", 1990, []string{"Horror", "Thriller"})
	l.oneGenre = add(`C:\m\g1.mkv`, "One Genre", 2002, []string{"Thriller"})
	l.nothingInCommon = add(`C:\m\n.mkv`, "Nothing", 2001, []string{"Comedy"})
	// Another copy of Saga 1, outside the collection: the same film, not a
	// similar one.
	l.sameFilm = add(`C:\m\s1x.mkv`, "Saga 1", 2001, []string{"Horror", "Thriller"}, dir, lead)

	coll, _, err := st.EnsureCollection(ctx, lib.ID, "tmdb", "1", "Saga", "Saga")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []int64{l.one, l.two, l.three} {
		if err := st.AddToCollection(ctx, f, coll, 0); err != nil {
			t.Fatal(err)
		}
	}
	l.coll = coll
	return l
}

func TestTheNextFilmAfterThisOne(t *testing.T) {
	ctx := context.Background()
	l := newAfterwardsLibrary(t)

	// Counted from this film, not from watch state: nothing has been saved
	// yet, which is the moment the end of a film asks.
	next, err := l.st.NextAfterInCollection(ctx, l.one, "local")
	if err != nil || next == nil || next.ID != l.two {
		t.Fatalf("after film 1: %+v, %v; want film 2", next, err)
	}
	if c := next.ContinuesFrom; c == nil || c.CollectionID != l.coll || c.AfterID != l.one ||
		c.AfterTitle != "Saga 1" || c.CollectionTitle != "Saga" {
		t.Fatalf("continues_from = %+v", next.ContinuesFrom)
	}

	// A film already watched is stepped over, forward only.
	if err := l.st.SaveProgress(ctx, l.two, "local", 0, true); err != nil {
		t.Fatal(err)
	}
	next, _ = l.st.NextAfterInCollection(ctx, l.one, "local")
	if next == nil || next.ID != l.three {
		t.Fatalf("film 2 watched: %+v; want film 3", next)
	}

	// The last film has nothing after it, and does not wrap to the first.
	if next, _ := l.st.NextAfterInCollection(ctx, l.three, "local"); next != nil {
		t.Fatalf("after the last film: %+v; want none", next)
	}
	// A film in no collection has no next film.
	if next, _ := l.st.NextAfterInCollection(ctx, l.oneGenre, "local"); next != nil {
		t.Fatalf("a film in no collection: %+v", next)
	}
}

func TestSimilarFilmsComeFromOutsideTheFranchise(t *testing.T) {
	ctx := context.Background()
	l := newAfterwardsLibrary(t)

	got, err := l.st.SimilarFilms(ctx, l.one, "local", 10)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, it := range got {
		ids = append(ids, it.ID)
	}
	// Two shared genres (4) beat the director (3) beat one genre (2); the
	// franchise, the other copy and the film with nothing in common are absent.
	want := []int64{l.twoGenres, l.byDirector, l.oneGenre}
	if len(ids) != len(want) {
		t.Fatalf("similar = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("similar = %v, want %v", ids, want)
		}
	}

	// Watched films are not suggested.
	if err := l.st.SaveProgress(ctx, l.twoGenres, "local", 0, true); err != nil {
		t.Fatal(err)
	}
	got, _ = l.st.SimilarFilms(ctx, l.one, "local", 1)
	if len(got) != 1 || got[0].ID != l.byDirector {
		t.Fatalf("with the best match watched, limit 1: %+v", got)
	}
}
