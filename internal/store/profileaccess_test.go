package store

import (
	"context"
	"testing"
)

/*
 * The profile page and the watchlist under an account's limits (ADR 0084,
 * review finding R05-1).
 *
 * The profile's three queries were written after the hidden-library rule and
 * never picked it up, so a member whose library was hidden after they had
 * played something in it still saw the titles, with posters, and still had
 * them counted. Each assertion is paired with a positive control: the same
 * history for an account nothing is hidden from, so a pass cannot come from
 * the rows never having been there.
 */

// finishTimes marks an item finished n times for an account.
func finishTimes(t *testing.T, st *Store, itemID int64, userID string, n int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		if err := st.SaveProgress(ctx, itemID, userID, 0, false); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveProgress(ctx, itemID, userID, 0, true); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTheProfileLeavesOutAHiddenLibrary(t *testing.T) {
	f := seedForAccess(t)
	ctx := context.Background()
	for _, u := range []*User{f.member, f.other} {
		finishTimes(t, f.st, f.privateFilm, u.ID, 3)
		finishTimes(t, f.st, f.openFilm, u.ID, 2)
	}

	check := func(u *User, wantPrivate bool) {
		t.Helper()
		hist, err := f.st.History(ctx, u.ID, GroupAll, 50, 0)
		if err != nil {
			t.Fatal(err)
		}
		most, err := f.st.MostPlayed(ctx, u.ID, 5)
		if err != nil {
			t.Fatal(err)
		}
		stats, err := f.st.ProfileStatistics(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		has := func(ids []int64) bool {
			for _, id := range ids {
				if id == f.privateFilm {
					return true
				}
			}
			return false
		}
		var histIDs, mostIDs []int64
		for _, e := range hist {
			histIDs = append(histIDs, e.Item.ID)
		}
		for _, p := range most {
			mostIDs = append(mostIDs, p.Item.ID)
		}
		if has(histIDs) != wantPrivate {
			t.Errorf("%s: history %v, private film listed = %v, want %v", u.Name, histIDs, has(histIDs), wantPrivate)
		}
		if has(mostIDs) != wantPrivate {
			t.Errorf("%s: most played %v, private film listed = %v, want %v", u.Name, mostIDs, has(mostIDs), wantPrivate)
		}
		wantStarted, wantViewings := 1, 2
		if wantPrivate {
			wantStarted, wantViewings = 2, 5
		}
		if stats.Started != wantStarted || stats.Viewings != wantViewings {
			t.Errorf("%s: started %d viewings %d, want %d and %d",
				u.Name, stats.Started, stats.Viewings, wantStarted, wantViewings)
		}
		// The open film must still be there for both: a pass by emptiness
		// proves nothing.
		if len(hist) == 0 || len(most) == 0 {
			t.Errorf("%s: history %d rows, most played %d rows; the open film is missing too", u.Name, len(hist), len(most))
		}
	}
	check(f.member, false)
	check(f.other, true)

	// Showing it again brings it back: the rows were never touched.
	if err := f.st.SetHiddenLibraries(ctx, f.member.ID, nil); err != nil {
		t.Fatal(err)
	}
	check(f.member, true)
}

// A hidden title must not take a place in the ranking either, or the list is
// one short of what a visible title would have filled.
func TestMostPlayedRanksOnlyWhatIsShown(t *testing.T) {
	f := seedForAccess(t)
	ctx := context.Background()
	finishTimes(t, f.st, f.privateFilm, f.member.ID, 4)
	finishTimes(t, f.st, f.openFilm, f.member.ID, 2)
	most, err := f.st.MostPlayed(ctx, f.member.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(most) != 1 || most[0].Item.ID != f.openFilm {
		t.Fatalf("most played (1 per group) = %+v, want the open film alone", most)
	}
}

func TestTheWatchlistLeavesOutWhatTheAccountCannotSee(t *testing.T) {
	f := seedForAccess(t)
	ctx := context.Background()
	for _, u := range []*User{f.member, f.other} {
		for _, id := range []int64{f.openFilm, f.privateFilm} {
			if err := f.st.AddToWatchlist(ctx, u.ID, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	ids := func(u *User) map[int64]bool {
		t.Helper()
		items, err := f.st.Watchlist(ctx, u.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int64]bool{}
		for _, it := range items {
			out[it.ID] = true
		}
		return out
	}

	// The hidden library.
	if got := ids(f.member); got[f.privateFilm] || !got[f.openFilm] {
		t.Errorf("member's watchlist %v: want the open film and not the private one", got)
	}
	if got := ids(f.other); !got[f.privateFilm] || !got[f.openFilm] {
		t.Errorf("control: unrestricted watchlist %v, want both", got)
	}

	// The rating ceiling, lowered after the title went on the list.
	if _, err := f.st.db.Exec(`UPDATE media_item SET content_rating = 'R' WHERE id = ?`, f.openFilm); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetMaxContentRating(ctx, f.other.ID, "PG"); err != nil {
		t.Fatal(err)
	}
	if got := ids(f.other); got[f.openFilm] {
		t.Errorf("watchlist under a PG ceiling still holds an R film: %v", got)
	}

	// The limit counts only what is shown: one slot, one visible title.
	items, err := f.st.Watchlist(ctx, f.member.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != f.openFilm {
		t.Errorf("member's watchlist at limit 1 = %v, want the open film: the hidden title must not take the slot", items)
	}
}
