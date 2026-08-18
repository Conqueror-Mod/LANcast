package store

import (
	"context"
	"path/filepath"
	"testing"
)

/*
 * Continue, and the promise it makes: never backwards.
 *
 * The bug being designed against is Plex's, described from daily use — press
 * continue on a long-running show and land three episodes back, on something
 * already watched. These tests hold the rule that prevents it, including the
 * two shapes that look identical until you skip an episode.
 */

// seedShow builds a show with two seasons of three episodes and returns the
// show id plus the episodes in playing order.
func seedShow(t *testing.T, st *Store) (int64, []int64) {
	t.Helper()
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "Shows", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	show, err := st.UpsertItem(ctx, ScanFile{
		LibraryID: lib.ID, Path: filepath.Join(lib.Path, "Show"), Kind: "show",
		Title: "A Show", SortTitle: "a show", MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	var eps []int64
	for _, se := range [][2]int{{1, 1}, {1, 2}, {1, 3}, {2, 1}, {2, 2}, {2, 3}} {
		season, episode := se[0], se[1]
		id, err := st.UpsertItem(ctx, ScanFile{
			LibraryID: lib.ID,
			Path:      filepath.Join(lib.Path, "Show", "s", "e.mkv"),
			Kind:      "episode", Title: "Episode", SortTitle: "episode",
			Season: &season, Episode: &episode,
			Container: "mkv", SizeBytes: 1, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		// Parented straight to the show, which the shape rules allow and which
		// this query has to handle alongside the season-in-between case.
		if _, err := st.db.ExecContext(ctx,
			`UPDATE media_item SET parent_id = ?, path = ? WHERE id = ?`,
			show, filepath.Join(lib.Path, "Show", "s"+string(rune('0'+season))+"e"+string(rune('0'+episode))+".mkv"), id); err != nil {
			t.Fatal(err)
		}
		eps = append(eps, id)
	}
	return show, eps
}

func markWatched(t *testing.T, st *Store, itemID int64, user string) {
	t.Helper()
	if _, err := st.db.ExecContext(context.Background(), `
		INSERT INTO playback_state (item_id, user_id, position_ms, watched, updated_at)
		VALUES (?, ?, 0, 1, ?)
		ON CONFLICT(item_id, user_id) DO UPDATE SET watched = 1, updated_at = excluded.updated_at`,
		itemID, user, 1000); err != nil {
		t.Fatal(err)
	}
}

func markInProgress(t *testing.T, st *Store, itemID int64, user string, at int64) {
	t.Helper()
	if _, err := st.db.ExecContext(context.Background(), `
		INSERT INTO playback_state (item_id, user_id, position_ms, watched, updated_at)
		VALUES (?, ?, 60000, 0, ?)
		ON CONFLICT(item_id, user_id) DO UPDATE SET position_ms = 60000, watched = 0, updated_at = excluded.updated_at`,
		itemID, user, at); err != nil {
		t.Fatal(err)
	}
}

func TestContinueStartsAtTheBeginningOfAnUntouchedShow(t *testing.T) {
	st := openTestStore(t)
	show, eps := seedShow(t, st)

	next, err := st.NextEpisodeFor(context.Background(), show, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if next.Item == nil || next.Item.ID != eps[0] {
		t.Fatalf("next = %v, want the first episode", next.Item)
	}
	if next.Resume || next.Exhausted {
		t.Error("an untouched show is neither a resume nor exhausted")
	}
}

/*
 * The assertion this file exists for.
 *
 * Episode 5 was skipped and everything through 13 watched. "Earliest unwatched"
 * answers 5 — which is the backtracking bug written as a query, and exactly the
 * complaint about Plex. Progress only moves forward.
 */
func TestContinueNeverGoesBackwardsOverASkippedEpisode(t *testing.T) {
	st := openTestStore(t)
	show, eps := seedShow(t, st)

	// Watched s1e1, skipped s1e2, watched s1e3 and s2e1.
	markWatched(t, st, eps[0], "u1")
	markWatched(t, st, eps[2], "u1")
	markWatched(t, st, eps[3], "u1")

	next, err := st.NextEpisodeFor(context.Background(), show, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if next.Item == nil {
		t.Fatal("no next episode")
	}
	if next.Item.ID == eps[1] {
		t.Fatal("continue went back to the skipped episode, which is the bug")
	}
	if next.Item.ID != eps[4] {
		t.Errorf("next = episode %v, want the one after the furthest watched", next.Item.ID)
	}
}

// An episode part-way through wins over the numbering: it is what is being
// watched right now.
func TestContinueResumesWhatIsInProgress(t *testing.T) {
	st := openTestStore(t)
	show, eps := seedShow(t, st)

	markWatched(t, st, eps[0], "u1")
	markInProgress(t, st, eps[1], "u1", 5000)

	next, err := st.NextEpisodeFor(context.Background(), show, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if next.Item == nil || next.Item.ID != eps[1] {
		t.Fatalf("next = %v, want the in-progress episode", next.Item)
	}
	if !next.Resume {
		t.Error("an in-progress episode should be reported as a resume")
	}
	if next.Item.Progress == nil || next.Item.Progress.PositionMS == 0 {
		t.Error("a resume without a position is just a play")
	}
}

// Two part-watched episodes: the one touched most recently is where you were.
func TestContinuePrefersTheMostRecentlyTouched(t *testing.T) {
	st := openTestStore(t)
	show, eps := seedShow(t, st)

	markInProgress(t, st, eps[4], "u1", 9000) // later
	markInProgress(t, st, eps[1], "u1", 1000) // earlier

	next, err := st.NextEpisodeFor(context.Background(), show, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if next.Item == nil || next.Item.ID != eps[4] {
		t.Fatalf("next = %v, want the most recently played", next.Item)
	}
}

// A finished show says so rather than silently replaying the finale.
func TestContinueReportsAFinishedShowAsExhausted(t *testing.T) {
	st := openTestStore(t)
	show, eps := seedShow(t, st)
	for _, id := range eps {
		markWatched(t, st, id, "u1")
	}

	next, err := st.NextEpisodeFor(context.Background(), show, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if !next.Exhausted {
		t.Error("a fully watched show is exhausted")
	}
	if next.Item != nil {
		t.Errorf("exhausted, but offered %v", next.Item.ID)
	}
}

// Progress is per user: one household member finishing a season must not move
// anybody else's place in it.
func TestContinueIsPerUser(t *testing.T) {
	st := openTestStore(t)
	show, eps := seedShow(t, st)
	for _, id := range eps {
		markWatched(t, st, id, "u1")
	}

	next, err := st.NextEpisodeFor(context.Background(), show, "u2")
	if err != nil {
		t.Fatal(err)
	}
	if next.Exhausted || next.Item == nil || next.Item.ID != eps[0] {
		t.Fatalf("second user got %+v, want the first episode", next)
	}
}

// The queue buttons order episodes the same way Continue does, or "next" and
// "the queue" disagree about what follows what.
func TestEpisodesAreQueuedInPlayingOrder(t *testing.T) {
	st := openTestStore(t)
	show, eps := seedShow(t, st)

	got, err := st.EpisodesOf(context.Background(), show)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(eps) {
		t.Fatalf("got %d episodes, want %d", len(got), len(eps))
	}
	for i := range eps {
		if got[i].ID != eps[i] {
			t.Fatalf("episode %d is %d, want %d — queue order does not match play order",
				i, got[i].ID, eps[i])
		}
	}
}
