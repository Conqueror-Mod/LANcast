package store

import (
	"context"
	"testing"
)

/*
 * The profile counts viewings, not rows.
 *
 * Revision 31 gave `playback_state` a tally of how many times a title has been
 * finished, and the statistics went on summing the flag beside it. So the
 * number the tally exists to produce was thrown away at the moment it was read:
 * a film watched twenty times reported one finish and one runtime, which is
 * precisely what a boolean could already say.
 *
 * The distinction these pin down is that `Finished` and `Viewings` answer
 * different questions and must not collapse into each other. Somebody who has
 * seen twelve films, one of them nine times, has finished twelve things and sat
 * through twenty.
 */

// withRuntime gives a title a known duration, which is what the time arithmetic
// is about. A title with no runtime is its own case and has its own test.
func withRuntime(t *testing.T, st *Store, item int64, ms int64) {
	t.Helper()
	if err := st.UpdateItemMetadata(context.Background(), item, ItemMetadata{DurationMS: &ms}); err != nil {
		t.Fatal(err)
	}
}

// rewatch drives a title from unfinished to finished, which is the transition
// SaveProgress counts. Going straight to finished twice records one viewing,
// which is the behaviour revision 31 chose and not an accident to work around.
func rewatch(t *testing.T, st *Store, item int64, times int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < times; i++ {
		if err := st.SaveProgress(ctx, item, "u1", 1000, false); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveProgress(ctx, item, "u1", 0, true); err != nil {
			t.Fatal(err)
		}
	}
}

func TestViewingsCountsEveryTimeAndFinishedCountsTitles(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	_, a, b, _ := seedLibrary(t, st)

	rewatch(t, st, a, 3)
	rewatch(t, st, b, 1)

	got, err := st.ProfileStatistics(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Finished != 2 {
		t.Errorf("finished = %d, want 2 — two distinct titles", got.Finished)
	}
	if got.Viewings != 4 {
		t.Errorf("viewings = %d, want 4 — three of one and one of the other", got.Viewings)
	}
}

/*
 * The reading that made this worth doing.
 *
 * Before, a rewatched film contributed one runtime however often it was seen,
 * so a profile under-reported the person it was describing — the same failure
 * the boolean had, one layer up.
 */
func TestTimeWatchedCountsEveryViewing(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	_, a, _, _ := seedLibrary(t, st)
	withRuntime(t, st, a, 90*60*1000)

	rewatch(t, st, a, 1)
	once, err := st.ProfileStatistics(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if once.WatchedMS <= 0 {
		t.Fatal("setup: a finished title recorded no time")
	}

	rewatch(t, st, a, 2)
	thrice, err := st.ProfileStatistics(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if thrice.WatchedMS != once.WatchedMS*3 {
		t.Errorf("three viewings = %dms, want %dms — three times one",
			thrice.WatchedMS, once.WatchedMS*3)
	}
}

/*
 * A finished title must not have its last showing counted twice.
 *
 * The tally already includes the viewing that finished it, so adding the saved
 * position as well would inflate every finished title by however far past the
 * end the player last reported.
 */
func TestAFinishedTitleDoesNotAlsoCountItsPosition(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	_, a, _, _ := seedLibrary(t, st)
	withRuntime(t, st, a, 90*60*1000)

	rewatch(t, st, a, 1)
	clean, err := st.ProfileStatistics(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	// The player keeps posting through the credits, which is ordinary.
	if err := st.SaveProgress(ctx, a, "u1", 5_000_000, true); err != nil {
		t.Fatal(err)
	}
	after, err := st.ProfileStatistics(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if after.WatchedMS != clean.WatchedMS {
		t.Errorf("a position posted on a finished title changed the total: %d then %d",
			clean.WatchedMS, after.WatchedMS)
	}
	if after.Viewings != 1 {
		t.Errorf("viewings = %d, want 1 — posting watched again is not a new viewing", after.Viewings)
	}
}

/*
 * Clearing history must not quietly return a rewatcher's tally to the number of
 * titles they finished.
 *
 * The rows being destroyed are the only place `watch_count` lives, which is the
 * fault the banked totals exist to prevent, arriving through the door revision
 * 31 opened.
 */
func TestResetKeepsTheViewingTally(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	_, a, b, _ := seedLibrary(t, st)

	rewatch(t, st, a, 5)
	rewatch(t, st, b, 1)

	before, err := st.ProfileStatistics(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if before.Viewings != 6 {
		t.Fatalf("setup: viewings = %d, want 6", before.Viewings)
	}

	if _, err := st.ResetHistory(ctx, "u1", HistoryAll, 0); err != nil {
		t.Fatal(err)
	}

	after, err := st.ProfileStatistics(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if after.Viewings != before.Viewings {
		t.Errorf("viewings after a reset = %d, want %d", after.Viewings, before.Viewings)
	}
	if after.WatchedMS != before.WatchedMS {
		t.Errorf("time watched after a reset = %d, want %d", after.WatchedMS, before.WatchedMS)
	}
}

/*
 * A title with no known runtime is counted once, whatever the tally says.
 *
 * There is no measurement of how long one viewing of it was, so multiplying the
 * position by the count would invent time rather than report it — upward, which
 * is the worse direction: a total that grows on its own is harder to disbelieve
 * than one that is missing.
 */
func TestAnUnknownRuntimeIsNotMultiplied(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	_, a, _, _ := seedLibrary(t, st)

	// No withRuntime here: that is the point.
	if err := st.SaveProgress(ctx, a, "u1", 600_000, false); err != nil {
		t.Fatal(err)
	}
	rewatch(t, st, a, 3)

	got, err := st.ProfileStatistics(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Viewings != 3 {
		t.Errorf("viewings = %d, want 3 — the tally is still counted", got.Viewings)
	}
	if got.WatchedMS != 0 {
		t.Errorf("time watched = %d, want 0 — a finished title with no runtime and no saved position contributes nothing rather than a guess", got.WatchedMS)
	}
}
