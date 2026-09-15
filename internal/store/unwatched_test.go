package store

import (
	"context"
	"path/filepath"
	"testing"
)

/*
 * What a show tile is allowed to claim you have finished.
 *
 * The mark this feeds is a tick in the corner of a poster, and the whole value
 * of a tick is that it is trustworthy: one that appears an episode early is
 * worse than no tick, because somebody stops looking. So the assertions here
 * are mostly about the cases where it must *not* appear.
 *
 * Reusing seedShow from nextepisode_test on purpose. That fixture parents its
 * episodes straight to the show, which is the looser of the two shapes the
 * schema allows — and it is the shape a naive two-level query gets wrong.
 */

func unwatchedOf(t *testing.T, st *Store, showID int64, user string) *int {
	t.Helper()
	items := []Item{{ID: showID, Kind: "show"}}
	if err := st.AttachUnwatchedEpisodes(context.Background(), items, user); err != nil {
		t.Fatal(err)
	}
	return items[0].UnwatchedEpisodes
}

func TestAShowNobodyHasStartedHasEveryEpisodeLeft(t *testing.T) {
	st := openTestStore(t)
	show, eps := seedShow(t, st)

	got := unwatchedOf(t, st, show, "u1")
	if got == nil {
		t.Fatal("a show with episodes reported nothing; the tile cannot tell a series from a film")
	}
	if *got != len(eps) {
		t.Errorf("unwatched = %d, want %d", *got, len(eps))
	}
}

func TestAShowWatchedThroughReportsNothingLeft(t *testing.T) {
	st := openTestStore(t)
	show, eps := seedShow(t, st)
	for _, id := range eps {
		markWatched(t, st, id, "u1")
	}

	got := unwatchedOf(t, st, show, "u1")
	if got == nil {
		t.Fatal("a fully watched show reported nothing rather than zero")
	}
	if *got != 0 {
		t.Errorf("unwatched = %d, want 0", *got)
	}
}

func TestOneEpisodeShortIsNotFinished(t *testing.T) {
	/*
	 * The case the tick exists to get right. Five of six watched is the state
	 * somebody is in the evening before they finish a series, and a tile that
	 * called it done would be wrong at the exact moment it is being read.
	 */
	st := openTestStore(t)
	show, eps := seedShow(t, st)
	for _, id := range eps[:len(eps)-1] {
		markWatched(t, st, id, "u1")
	}

	got := unwatchedOf(t, st, show, "u1")
	if got == nil || *got != 1 {
		t.Fatalf("unwatched = %v, want 1", got)
	}
}

func TestAnEpisodeInProgressIsStillUnwatched(t *testing.T) {
	/*
	 * A saved position is not a viewing. This matches the continue-watching
	 * semantics, and the two must agree: a show ticked as finished while
	 * Continue Watching still offers it an episode is two parts of the screen
	 * contradicting each other with nothing failing.
	 */
	st := openTestStore(t)
	show, eps := seedShow(t, st)
	for _, id := range eps[:len(eps)-1] {
		markWatched(t, st, id, "u1")
	}
	// The last one started and abandoned: a row exists, watched is 0.
	if _, err := st.db.ExecContext(context.Background(), `
		INSERT INTO playback_state (item_id, user_id, position_ms, watched, updated_at)
		VALUES (?, ?, 600000, 0, ?)`, eps[len(eps)-1], "u1", 2000); err != nil {
		t.Fatal(err)
	}

	got := unwatchedOf(t, st, show, "u1")
	if got == nil || *got != 1 {
		t.Fatalf("unwatched = %v, want 1 — a part-watched episode is not a seen one", got)
	}
}

func TestOneAccountsViewingIsNotAnother(t *testing.T) {
	// The count is per account, like every other playback question. A parent
	// finishing a series must not tick it for the child.
	st := openTestStore(t)
	show, eps := seedShow(t, st)
	for _, id := range eps {
		markWatched(t, st, id, "u1")
	}

	if got := unwatchedOf(t, st, show, "u2"); got == nil || *got != len(eps) {
		t.Fatalf("unwatched for the other account = %v, want %d", got, len(eps))
	}
}

func TestNothingButAShowIsAnswered(t *testing.T) {
	/*
	 * The failure this guards is one this project has already shipped once, in
	 * the rating ceiling: a video question answered for kinds that never had
	 * video. Zero here would mean "finished", and a music library would come
	 * back with a tick on every album.
	 */
	st := openTestStore(t)
	show, _ := seedShow(t, st)

	items := []Item{
		{ID: show, Kind: "show"},
		{ID: 9001, Kind: "movie"},
		{ID: 9002, Kind: "album"},
		{ID: 9003, Kind: "photo"},
		{ID: 9004, Kind: "episode"},
	}
	if err := st.AttachUnwatchedEpisodes(context.Background(), items, "u1"); err != nil {
		t.Fatal(err)
	}
	for _, it := range items[1:] {
		if it.UnwatchedEpisodes != nil {
			t.Errorf("a %s was told it has %d episodes left", it.Kind, *it.UnwatchedEpisodes)
		}
	}
	if items[0].UnwatchedEpisodes == nil {
		t.Error("the show in the same page was skipped")
	}
}

func TestAShowWithNoEpisodesIsNotFinished(t *testing.T) {
	/*
	 * An empty series reports nothing rather than zero, and the distinction is
	 * the point: "no episodes left to watch" is a true sentence about a show
	 * with no episodes, and it means the opposite of what a tick would say.
	 * Real libraries carry these — a show matched by metadata whose files have
	 * not been scanned yet, or whose drive is unplugged.
	 */
	st := openTestStore(t)
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "Shows", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	empty, err := st.UpsertItem(ctx, ScanFile{
		LibraryID: lib.ID, Path: filepath.Join(lib.Path, "Empty"), Kind: "show",
		Title: "An Empty Show", SortTitle: "an empty show", MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := unwatchedOf(t, st, empty, "u1"); got != nil {
		t.Errorf("an empty show reported %d episodes left; it should report nothing", *got)
	}
}

func TestSeveralShowsAreCountedInOnePass(t *testing.T) {
	// The page-at-a-time contract. Two shows in one call must not be given each
	// other's totals, which is the mistake a GROUP BY keyed on the wrong column
	// makes.
	st := openTestStore(t)
	a, aEps := seedShow(t, st)
	b, bEps := seedShow(t, st)
	for _, id := range aEps {
		markWatched(t, st, id, "u1")
	}

	items := []Item{{ID: a, Kind: "show"}, {ID: b, Kind: "show"}}
	if err := st.AttachUnwatchedEpisodes(context.Background(), items, "u1"); err != nil {
		t.Fatal(err)
	}
	if items[0].UnwatchedEpisodes == nil || *items[0].UnwatchedEpisodes != 0 {
		t.Errorf("the watched show = %v, want 0", items[0].UnwatchedEpisodes)
	}
	if items[1].UnwatchedEpisodes == nil || *items[1].UnwatchedEpisodes != len(bEps) {
		t.Errorf("the untouched show = %v, want %d", items[1].UnwatchedEpisodes, len(bEps))
	}
}

func TestAMissingEpisodeIsNotCounted(t *testing.T) {
	/*
	 * Scanning marks missing, never deletes — so a show whose drive is
	 * unplugged keeps its rows. Counting those would leave a series you have
	 * finished permanently one short, which reads as the tick being broken
	 * rather than as a missing file.
	 */
	st := openTestStore(t)
	show, eps := seedShow(t, st)
	for _, id := range eps[:len(eps)-1] {
		markWatched(t, st, id, "u1")
	}
	if _, err := st.db.ExecContext(context.Background(),
		`UPDATE media_item SET missing = 1 WHERE id = ?`, eps[len(eps)-1]); err != nil {
		t.Fatal(err)
	}

	got := unwatchedOf(t, st, show, "u1")
	if got == nil || *got != 0 {
		t.Fatalf("unwatched = %v, want 0 — the only one left is not on disk", got)
	}
}
