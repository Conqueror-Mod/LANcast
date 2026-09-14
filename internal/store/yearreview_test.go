package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

/*
 * A year, out of a history that was never designed to be one.
 *
 * The tests that matter are the ones about what this deliberately does *not*
 * claim. The appeal of the page is that it is computed from data that never
 * left the machine and is not a marketing artefact, and the fastest way to
 * throw that away is a number that is quietly inflated.
 */

type yearFixture struct {
	st   *Store
	user *User
	lib  *Library
	root string
}

func seedYear(t *testing.T) yearFixture {
	t.Helper()
	ctx := context.Background()
	st := openTestStore(t)
	root := t.TempDir()
	lib, err := st.CreateLibrary(ctx, "Films", "movie", root)
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUser(ctx, "", "watcher", "hash", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	return yearFixture{st: st, user: u, lib: lib, root: root}
}

// played records a title as played at a local wall-clock time, which is the
// calendar the year boundaries are drawn on.
func (f yearFixture) played(t *testing.T, title string, when time.Time, durationMS int64, finished bool, positionMS int64) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := f.st.UpsertItem(ctx, ScanFile{
		LibraryID: f.lib.ID, Path: filepath.Join(f.root, title+".mkv"), Kind: "movie",
		Title: title, SortTitle: title, Container: "mkv", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if durationMS > 0 {
		if err := f.st.UpdateItemMetadata(ctx, id, ItemMetadata{DurationMS: &durationMS}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.st.SaveProgress(ctx, id, f.user.ID, positionMS, finished); err != nil {
		t.Fatal(err)
	}
	// SaveProgress stamps now; these tests are about *when*, so the stamp is
	// set explicitly rather than by waiting a year.
	if _, err := f.st.db.ExecContext(ctx,
		`UPDATE playback_state SET updated_at = ? WHERE item_id = ? AND user_id = ?`,
		when.Unix(), id, f.user.ID); err != nil {
		t.Fatal(err)
	}
	return id
}

func local(y int, m time.Month, d, h int) time.Time {
	return time.Date(y, m, d, h, 0, 0, 0, time.Local)
}

func TestAYearCountsWhatWasPlayedInIt(t *testing.T) {
	f := seedYear(t)
	f.played(t, "Arrival", local(2025, time.March, 4, 20), 7_200_000, true, 7_200_000)
	f.played(t, "Dune", local(2025, time.July, 9, 21), 9_000_000, false, 1_800_000)
	f.played(t, "Last Year's Film", local(2024, time.May, 1, 19), 5_400_000, true, 5_400_000)

	got, err := f.st.YearInReview(context.Background(), f.user.ID, 2025)
	if err != nil {
		t.Fatal(err)
	}
	if got.Titles != 2 {
		t.Errorf("titles = %d, want 2", got.Titles)
	}
	if got.Finished != 1 || got.Abandoned != 1 {
		t.Errorf("finished/abandoned = %d/%d, want 1/1", got.Finished, got.Abandoned)
	}
}

func TestTimeSpentCountsOneViewingAndSaysSoByBeingLow(t *testing.T) {
	/*
	 * The decision this file exists to protect.
	 *
	 * `watch_count` is real, and the *dates* of those viewings are not: only
	 * the most recent play is recorded. Multiplying would attribute every
	 * rewatch to the year of the last one, which is how a total grows on its
	 * own — and a number that is quietly inflated is exactly what this page is
	 * meant not to be.
	 */
	f := seedYear(t)
	ctx := context.Background()
	id := f.played(t, "Rewatched", local(2025, time.February, 2, 20), 6_000_000, true, 6_000_000)
	/*
	 * Watched four more times, all of them recorded as one row.
	 *
	 * A rewatch is the 0→1 transition, which is what the tally counts — so a
	 * repeat means starting it again and finishing it again, not saying
	 * "finished" five times. Saving the same finished state over and over is
	 * one viewing, correctly.
	 */
	for i := 0; i < 4; i++ {
		if err := f.st.SaveProgress(ctx, id, f.user.ID, 1000, false); err != nil {
			t.Fatal(err)
		}
		if err := f.st.SaveProgress(ctx, id, f.user.ID, 6_000_000, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.st.db.ExecContext(ctx,
		`UPDATE playback_state SET updated_at = ? WHERE item_id = ?`,
		local(2025, time.February, 2, 20).Unix(), id); err != nil {
		t.Fatal(err)
	}

	got, err := f.st.YearInReview(ctx, f.user.ID, 2025)
	if err != nil {
		t.Fatal(err)
	}
	if got.WatchedMS != 6_000_000 {
		t.Errorf("watched = %d, want one viewing (6000000) rather than five", got.WatchedMS)
	}

	// And the lifetime figure, which is not claiming a year, still multiplies.
	stats, err := f.st.ProfileStatistics(ctx, f.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.WatchedMS <= got.WatchedMS {
		t.Errorf("lifetime = %d and the year = %d; the lifetime total counts every viewing and should be larger",
			stats.WatchedMS, got.WatchedMS)
	}
}

func TestAnUnfinishedTitleCountsHowFarYouGot(t *testing.T) {
	// Summing the runtime of everything opened reports eleven hours for eleven
	// films abandoned in their first minute — the same rule the lifetime
	// figure keeps, and the reason this one is worth stating twice.
	f := seedYear(t)
	f.played(t, "Put Down", local(2025, time.April, 1, 20), 9_000_000, false, 60_000)

	got, err := f.st.YearInReview(context.Background(), f.user.ID, 2025)
	if err != nil {
		t.Fatal(err)
	}
	if got.WatchedMS != 60_000 {
		t.Errorf("watched = %d, want the minute actually spent", got.WatchedMS)
	}
}

func TestEveryMonthIsPresentIncludingTheQuietOnes(t *testing.T) {
	// A chart with the empty months missing is a chart that lies about the
	// shape of the year, which is the one thing this view is really for.
	f := seedYear(t)
	f.played(t, "Only Thing", local(2025, time.November, 20, 20), 6_000_000, true, 6_000_000)

	got, err := f.st.YearInReview(context.Background(), f.user.ID, 2025)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Months) != 12 {
		t.Fatalf("months = %d, want 12", len(got.Months))
	}
	for i, m := range got.Months {
		if m.Month != i+1 {
			t.Fatalf("month %d is labelled %d", i+1, m.Month)
		}
	}
	if got.Months[10].Titles != 1 {
		t.Errorf("November = %d, want 1", got.Months[10].Titles)
	}
	if got.Months[0].Titles != 0 {
		t.Errorf("January = %d, want 0", got.Months[0].Titles)
	}
}

func TestAYearEndsWhereTheHouseholdSaysItDoes(t *testing.T) {
	/*
	 * Local time, not UTC.
	 *
	 * Eight in the evening on New Year's Eve belongs to the year they were in
	 * when they watched it. In any US timezone the UTC date is already the
	 * first of January, which is the trap this project has hit before in both
	 * directions — a date built from UTC components resolves to tomorrow every
	 * evening.
	 */
	f := seedYear(t)
	f.played(t, "New Year's Eve", local(2025, time.December, 31, 20), 6_000_000, true, 6_000_000)

	got, err := f.st.YearInReview(context.Background(), f.user.ID, 2025)
	if err != nil {
		t.Fatal(err)
	}
	if got.Titles != 1 {
		t.Errorf("2025 held %d titles, want the New Year's Eve film", got.Titles)
	}
	next, err := f.st.YearInReview(context.Background(), f.user.ID, 2026)
	if err != nil {
		t.Fatal(err)
	}
	if next.Titles != 0 {
		t.Errorf("2026 held %d titles, want none: the film was watched in 2025 local time", next.Titles)
	}
}

func TestTheFirstAndLastThingOfTheYear(t *testing.T) {
	f := seedYear(t)
	f.played(t, "Opened With", local(2025, time.January, 3, 20), 6_000_000, true, 6_000_000)
	f.played(t, "Middle", local(2025, time.June, 3, 20), 6_000_000, true, 6_000_000)
	f.played(t, "Closed With", local(2025, time.December, 20, 20), 6_000_000, true, 6_000_000)

	got, err := f.st.YearInReview(context.Background(), f.user.ID, 2025)
	if err != nil {
		t.Fatal(err)
	}
	if got.First == nil || got.First.Title != "Opened With" {
		t.Errorf("first = %v, want Opened With", got.First)
	}
	if got.Last == nil || got.Last.Title != "Closed With" {
		t.Errorf("last = %v, want Closed With", got.Last)
	}
}

func TestAYearWithNothingInItIsNotAnError(t *testing.T) {
	// A perfectly ordinary answer for a year somebody did not use the server,
	// and the page needs to be able to say so rather than fail.
	f := seedYear(t)
	got, err := f.st.YearInReview(context.Background(), f.user.ID, 2019)
	if err != nil {
		t.Fatal(err)
	}
	if got.Titles != 0 || got.First != nil || got.Last != nil {
		t.Errorf("an empty year answered %+v", got)
	}
	if len(got.Months) != 12 {
		t.Error("an empty year still has twelve months")
	}
}

func TestOnlyYearsWithHistoryAreOffered(t *testing.T) {
	f := seedYear(t)
	f.played(t, "A", local(2024, time.May, 1, 20), 6_000_000, true, 6_000_000)
	f.played(t, "B", local(2025, time.May, 1, 20), 6_000_000, true, 6_000_000)

	years, err := f.st.WatchYears(context.Background(), f.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(years) != 2 || years[0] != 2025 || years[1] != 2024 {
		t.Errorf("years = %v, want [2025 2024]", years)
	}
}

func TestAYearIsOneAccountsOwn(t *testing.T) {
	/*
	 * Viewing is private by default (ADR 0035). Somebody else's history must
	 * not turn up in your year, and there is deliberately no variant of this
	 * that takes another account's id.
	 */
	f := seedYear(t)
	ctx := context.Background()
	other, err := f.st.CreateUser(ctx, "", "somebody else", "hash", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	id := f.played(t, "Theirs", local(2025, time.March, 1, 20), 6_000_000, true, 6_000_000)
	if err := f.st.SaveProgress(ctx, id, other.ID, 6_000_000, true); err != nil {
		t.Fatal(err)
	}
	// Their row needs a date in the same year, or this compares 2025 with now.
	if _, err := f.st.db.ExecContext(ctx,
		`UPDATE playback_state SET updated_at = ? WHERE item_id = ? AND user_id = ?`,
		local(2025, time.March, 1, 21).Unix(), id, other.ID); err != nil {
		t.Fatal(err)
	}

	got, err := f.st.YearInReview(ctx, other.ID, 2025)
	if err != nil {
		t.Fatal(err)
	}
	if got.Titles != 1 {
		t.Fatalf("the other account's own year = %d titles, want 1", got.Titles)
	}
	// One title each, not two: the same film played by two people is one row
	// per person and must not be counted twice for either.
	mine, err := f.st.YearInReview(ctx, f.user.ID, 2025)
	if err != nil {
		t.Fatal(err)
	}
	if mine.Titles != 1 {
		t.Errorf("my year = %d titles, want only my own", mine.Titles)
	}
}
