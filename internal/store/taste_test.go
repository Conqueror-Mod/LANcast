package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

/*
 * RecentTaste (ADR 0119 §1): the genre recent finishes share most, counted by
 * title, and only once it is a pattern.
 */

type tasteFixture struct {
	st   *Store
	me   string
	film func(name string, genres ...string) int64
}

func newTasteFixture(t *testing.T) tasteFixture {
	t.Helper()
	st, me, _ := viewingFixture(t)
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "More films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return tasteFixture{st: st, me: me, film: func(name string, genres ...string) int64 {
		t.Helper()
		id, err := st.UpsertItem(ctx, ScanFile{
			LibraryID: lib.ID, Path: filepath.Join(lib.Path, name+".mkv"), Kind: "movie",
			Title: name, SortTitle: name, Container: "mkv", SizeBytes: 1, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.ReplaceGenres(ctx, id, genres); err != nil {
			t.Fatal(err)
		}
		return id
	}}
}

func (fx tasteFixture) finish(t *testing.T, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		if err := fx.st.SaveProgress(context.Background(), id, fx.me, 0, true); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecentTasteIsTheGenreMostFinishedLately(t *testing.T) {
	fx := newTasteFixture(t)
	fx.finish(t,
		fx.film("A", "Thriller", "Drama"),
		fx.film("B", "Thriller"),
		fx.film("C", "Thriller", "Comedy"),
		fx.film("D", "Comedy"),
	)
	got, ok, err := fx.st.RecentTaste(context.Background(), fx.me)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Genre != "Thriller" || got.Titles != 3 {
		t.Errorf("taste = %+v ok=%v, want Thriller across 3 titles", got, ok)
	}
}

// Two films is not "lately": the sentence would be a claim about a coincidence.
func TestRecentTasteNeedsThreeTitles(t *testing.T) {
	fx := newTasteFixture(t)
	fx.finish(t, fx.film("A", "Western"), fx.film("B", "Western"), fx.film("C", "Drama"))
	if got, ok, err := fx.st.RecentTaste(context.Background(), fx.me); err != nil || ok {
		t.Errorf("taste = %+v ok=%v err=%v, want none below three titles", got, ok, err)
	}
}

// Rewatching one film five times is one title, not five.
func TestRecentTasteCountsTitlesNotViewings(t *testing.T) {
	fx := newTasteFixture(t)
	ctx := context.Background()
	heist := fx.film("Heist", "Crime")
	for i := 0; i < 5; i++ {
		fx.finish(t, heist)
		if err := fx.st.SaveProgress(ctx, heist, fx.me, 1000, false); err != nil {
			t.Fatal(err)
		}
	}
	if got, ok, _ := fx.st.RecentTaste(ctx, fx.me); ok {
		t.Errorf("taste = %+v from one film watched five times", got)
	}
}

// Episodes count as their show, once, with the show's genres.
func TestRecentTasteCountsAShowOnce(t *testing.T) {
	fx := newTasteFixture(t)
	ctx := context.Background()
	tv, err := fx.st.CreateLibrary(ctx, "TV", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	show, _, err := fx.st.EnsureShow(ctx, tv.ID, filepath.Join(tv.Path, "Bones"), "Bones", "bones")
	if err != nil {
		t.Fatal(err)
	}
	if err := fx.st.ReplaceGenres(ctx, show, []string{"Mystery"}); err != nil {
		t.Fatal(err)
	}
	for e := 1; e <= 6; e++ {
		series, s, ep := "Bones", 1, e
		id, err := fx.st.UpsertItem(ctx, ScanFile{
			LibraryID: tv.ID, Path: filepath.Join(tv.Path, "Bones", fmt.Sprintf("s01e%02d.mkv", e)),
			Kind: "episode", Title: fmt.Sprintf("Episode %d", e), SortTitle: "bones",
			Series: &series, Season: &s, Episode: &ep, Container: "mkv", SizeBytes: 1, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := fx.st.SetParent(ctx, id, &show); err != nil {
			t.Fatal(err)
		}
		fx.finish(t, id)
	}
	// Six episodes of one show, and two films: Mystery is two titles, not seven.
	fx.finish(t, fx.film("X", "Mystery"))
	if got, ok, _ := fx.st.RecentTaste(ctx, fx.me); ok {
		t.Errorf("taste = %+v; six episodes of one show counted as more than one title", got)
	}
	fx.finish(t, fx.film("Y", "Mystery"))
	got, ok, err := fx.st.RecentTaste(ctx, fx.me)
	if err != nil || !ok || got.Genre != "Mystery" || got.Titles != 3 {
		t.Errorf("taste = %+v ok=%v err=%v, want Mystery across the show and two films", got, ok, err)
	}
}

// Somebody else's finishes are not this account's taste.
func TestRecentTasteIsPerAccount(t *testing.T) {
	fx := newTasteFixture(t)
	fx.finish(t, fx.film("A", "Horror"), fx.film("B", "Horror"), fx.film("C", "Horror"))
	other, err := fx.st.CreateUser(context.Background(), "", "eli", "hash", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok, _ := fx.st.RecentTaste(context.Background(), other.ID); ok {
		t.Errorf("a new account inherited a taste: %+v", got)
	}
}
