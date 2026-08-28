package store

import (
	"context"
	"testing"
)

/*
 * Watching something twice.
 *
 * `watched` is a boolean and a boolean is wrong about how people watch things:
 * a film seen twenty times and a film seen once carried the same record. These
 * pin the rule that makes the difference visible — the count moves on the
 * *transition* from unfinished to finished, and never on the level.
 *
 * The distinction is the whole feature. A player posts progress every five
 * seconds, so "watched" arrives true dozens of times for one viewing; a rule
 * that counted those would be counting heartbeats.
 */

func watchCount(t *testing.T, st *Store, itemID int64) int {
	t.Helper()
	it, err := st.GetItem(context.Background(), itemID, "local")
	if err != nil {
		t.Fatal(err)
	}
	if it.Progress == nil {
		return 0
	}
	return it.Progress.WatchCount
}

func watchedItem(t *testing.T, st *Store) int64 {
	t.Helper()
	lib := mustLibrary(t, st)
	if _, err := st.UpsertItem(context.Background(), file(lib.ID, `C:\m\friday.mkv`, "Friday")); err != nil {
		t.Fatal(err)
	}
	known, _ := st.KnownFiles(context.Background(), lib.ID)
	return known[`C:\m\friday.mkv`].ID
}

func TestFinishingSomethingCountsOnce(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	id := watchedItem(t, st)

	if err := st.SaveProgress(ctx, id, "local", 500, false); err != nil {
		t.Fatal(err)
	}
	if got := watchCount(t, st, id); got != 0 {
		t.Fatalf("count = %d before finishing, want 0", got)
	}

	if err := st.SaveProgress(ctx, id, "local", 90000, true); err != nil {
		t.Fatal(err)
	}
	if got := watchCount(t, st, id); got != 1 {
		t.Fatalf("count = %d after finishing, want 1", got)
	}
}

// The assertion that separates a viewing from a heartbeat.
func TestProgressWhileAlreadyFinishedDoesNotCount(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	id := watchedItem(t, st)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.SaveProgress(ctx, id, "local", 90000, true))
	// The player keeps posting every five seconds through the credits.
	for range 10 {
		must(st.SaveProgress(ctx, id, "local", 90000, true))
	}
	if got := watchCount(t, st, id); got != 1 {
		t.Errorf("count = %d after one viewing and ten heartbeats, want 1", got)
	}
}

/*
 * The case the feature exists for.
 *
 * Nothing detects a restart. Starting the film again posts an early position,
 * which writes `watched = 0`, and finishing it writes 1 again — so the edge is
 * already there and simply was not being counted.
 */
func TestWatchingTwiceCountsTwice(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	id := watchedItem(t, st)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.SaveProgress(ctx, id, "local", 90000, true))
	must(st.SaveProgress(ctx, id, "local", 300, false)) // started it again
	must(st.SaveProgress(ctx, id, "local", 90000, true))

	if got := watchCount(t, st, id); got != 2 {
		t.Errorf("count = %d after two viewings, want 2", got)
	}
}

/*
 * Marking something unwatched puts it back on the list; it is not a claim never
 * to have seen it.
 *
 * So the tally survives, and a title can read "not watched" while carrying a
 * count. Getting this wrong destroys a number that took years to accumulate,
 * which is exactly the kind of quiet loss this project keeps guarding against.
 */
func TestMarkingUnwatchedKeepsTheCount(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	id := watchedItem(t, st)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.SaveProgress(ctx, id, "local", 90000, true))
	must(st.SaveProgress(ctx, id, "local", 90000, true)) // still one viewing
	must(st.SaveProgress(ctx, id, "local", 0, false))    // marked unwatched

	it, err := st.GetItem(ctx, id, "local")
	if err != nil {
		t.Fatal(err)
	}
	if it.Progress.Watched {
		t.Error("still watched after being marked unwatched")
	}
	if it.Progress.WatchCount != 1 {
		t.Errorf("count = %d after marking unwatched, want it kept at 1", it.Progress.WatchCount)
	}
}

// Marking something finished that was never played is a viewing nobody
// recorded at the time, so the row is born at one rather than at zero.
func TestMarkingWatchedWithoutPlayingCountsOnce(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	id := watchedItem(t, st)

	if err := st.SaveProgress(ctx, id, "local", 0, true); err != nil {
		t.Fatal(err)
	}
	if got := watchCount(t, st, id); got != 1 {
		t.Errorf("count = %d for a row born watched, want 1", got)
	}
}

// Counts belong to a person, not to a title: two accounts watching the same
// film keep their own tallies.
func TestWatchCountsArePerAccount(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	id := watchedItem(t, st)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.SaveProgress(ctx, id, "local", 90000, true))
	must(st.SaveProgress(ctx, id, "local", 300, false))
	must(st.SaveProgress(ctx, id, "local", 90000, true))
	must(st.SaveProgress(ctx, id, "georgia", 90000, true))

	if got := watchCount(t, st, id); got != 2 {
		t.Errorf("local count = %d, want 2", got)
	}
	other, err := st.GetItem(ctx, id, "georgia")
	if err != nil {
		t.Fatal(err)
	}
	if other.Progress.WatchCount != 1 {
		t.Errorf("second account count = %d, want 1", other.Progress.WatchCount)
	}
}

// The listing path carries it too, so a grid and a detail page cannot disagree
// about how many times something has been watched.
func TestAttachProgressCarriesTheCount(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	id := watchedItem(t, st)

	if err := st.SaveProgress(ctx, id, "local", 90000, true); err != nil {
		t.Fatal(err)
	}
	items, _, err := st.ListItems(ctx, ItemFilter{})
	if err != nil {
		t.Fatal(err)
	}
	// A listing carries no progress until it is asked to — this is the call a
	// grid render makes, and the one that must not drop the count.
	if err := st.AttachProgress(ctx, items, "local"); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, it := range items {
		if it.ID != id {
			continue
		}
		found = true
		if it.Progress == nil || it.Progress.WatchCount != 1 {
			t.Errorf("listed item carried count %v, want 1", it.Progress)
		}
	}
	if !found {
		t.Fatal("item missing from the listing")
	}
}
