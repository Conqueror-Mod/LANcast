package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

type watchFixture struct {
	st          *Store
	user        string
	film, film2 int64
	show        int64
	ep1, ep2    int64
}

func seedWatchlist(t *testing.T) watchFixture {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	add := func(name, kind string, parent *int64) int64 {
		f := file(lib.ID, filepath.Join(lib.Path, name), name)
		f.Kind = kind
		id, err := st.UpsertItem(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		if parent != nil {
			if err := st.SetParent(ctx, id, parent); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	u, err := st.CreateUser(ctx, "", "chris", "h", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	f := watchFixture{st: st, user: u.ID}
	f.film = add("Paddington.mkv", "movie", nil)
	f.film2 = add("Heat.mkv", "movie", nil)
	f.show = add("Show", "show", nil)
	season := add("Show S1", "season", &f.show)
	f.ep1 = add("Show S1E1.mkv", "episode", &season)
	f.ep2 = add("Show S1E2.mkv", "episode", &season)
	return f
}

func (f watchFixture) ids(t *testing.T) []int64 {
	t.Helper()
	items, err := f.st.Watchlist(context.Background(), f.user, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := []int64{}
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func TestTheWatchlistIsNewestFirstAndTakesFilmsAndShows(t *testing.T) {
	f := seedWatchlist(t)
	ctx := context.Background()
	for _, id := range []int64{f.film, f.show, f.film2} {
		if err := f.st.AddToWatchlist(ctx, f.user, id); err != nil {
			t.Fatal(err)
		}
	}
	// Same-second adds tie on added_at; insertion order breaks it, so the
	// last added is first even though the show has the highest item id.
	got := f.ids(t)
	if len(got) != 3 || got[0] != f.film2 {
		t.Errorf("watchlist = %v, want film2 first of three", got)
	}
	// Adding again changes nothing.
	if err := f.st.AddToWatchlist(ctx, f.user, f.film); err != nil || len(f.ids(t)) != 3 {
		t.Errorf("re-adding: %v, %v", err, f.ids(t))
	}
	if err := f.st.AddToWatchlist(ctx, f.user, f.ep1); !errors.Is(err, ErrNotWatchlistable) {
		t.Errorf("adding an episode err = %v", err)
	}
	if err := f.st.AddToWatchlist(ctx, f.user, 99999); !errors.Is(err, ErrNotFound) {
		t.Errorf("adding nothing err = %v", err)
	}
	// Private: another account's list is empty.
	other, _ := f.st.CreateUser(ctx, "", "georgia", "h", RoleMember)
	if items, _ := f.st.Watchlist(ctx, other.ID, 0); len(items) != 0 {
		t.Errorf("another account sees %d", len(items))
	}
}

// ADR 0085 §3: finishing a film takes it off; progress short of that does not.
func TestFinishingAFilmTakesItOff(t *testing.T) {
	f := seedWatchlist(t)
	ctx := context.Background()
	_ = f.st.AddToWatchlist(ctx, f.user, f.film)
	_ = f.st.AddToWatchlist(ctx, f.user, f.film2)
	if err := f.st.SaveProgress(ctx, f.film, f.user, 600000, false); err != nil {
		t.Fatal(err)
	}
	if len(f.ids(t)) != 2 {
		t.Fatal("a film half watched left the list")
	}
	if err := f.st.SaveProgress(ctx, f.film, f.user, 0, true); err != nil {
		t.Fatal(err)
	}
	if got := f.ids(t); len(got) != 1 || got[0] != f.film2 {
		t.Errorf("after finishing = %v, want only the other film", got)
	}
}

// A show leaves only when its last episode is finished.
func TestAShowLeavesWhenItsLastEpisodeIsFinished(t *testing.T) {
	f := seedWatchlist(t)
	ctx := context.Background()
	_ = f.st.AddToWatchlist(ctx, f.user, f.show)
	if err := f.st.SaveProgress(ctx, f.ep1, f.user, 0, true); err != nil {
		t.Fatal(err)
	}
	if len(f.ids(t)) != 1 {
		t.Fatal("the show left after its first episode")
	}
	if err := f.st.SaveProgress(ctx, f.ep2, f.user, 0, true); err != nil {
		t.Fatal(err)
	}
	if len(f.ids(t)) != 0 {
		t.Errorf("the show is still listed after its last episode: %v", f.ids(t))
	}
}
