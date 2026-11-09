package store

import (
	"context"
	"path/filepath"
	"testing"
)

/*
 * Totals by kind (profile-2-plan §4c, step 4).
 *
 * "Time watched" counted songs, and every song played was one more title
 * started. These pin the split: each kind on its own line, the lines adding up
 * to the totals exactly, and a reset's banked amount kept as its own line
 * rather than vanishing or being guessed into a kind.
 */

type kindFixture struct {
	st   *Store
	lib  *Library
	user string
	n    int
}

func newKindFixture(t *testing.T) *kindFixture {
	t.Helper()
	st := openTestStore(t)
	lib, err := st.CreateLibrary(context.Background(), "Everything", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &kindFixture{st: st, lib: lib, user: "u1"}
}

// play records one title of a kind: how long it is, how many times it was
// finished, and where an unfinished one stopped.
func (f *kindFixture) play(t *testing.T, kind string, durationMS int64, finishes int, positionMS int64) int64 {
	t.Helper()
	ctx := context.Background()
	f.n++
	name := kind + string(rune('a'+f.n))
	id, err := f.st.UpsertItem(ctx, ScanFile{
		LibraryID: f.lib.ID, Path: filepath.Join(f.lib.Path, name+".mkv"), Kind: "movie",
		Title: name, SortTitle: name, Container: "mkv", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.db.ExecContext(ctx,
		`UPDATE media_item SET kind = ?, duration_ms = ? WHERE id = ?`, kind, durationMS, id); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SaveProgress(ctx, id, f.user, positionMS, finishes > 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.db.ExecContext(ctx,
		`UPDATE playback_state SET watch_count = ?, updated_at = ? WHERE item_id = ? AND user_id = ?`,
		finishes, 1_700_000_000+f.n, id, f.user); err != nil {
		t.Fatal(err)
	}
	return id
}

func byKind(st ProfileStats) map[string]KindTotals {
	out := map[string]KindTotals{}
	for _, k := range st.ByKind {
		out[k.Kind] = k
	}
	return out
}

func TestTotalsSplitByKindAndAddUp(t *testing.T) {
	ctx := context.Background()
	f := newKindFixture(t)
	f.play(t, "movie", 6_000_000, 2, 0)       // a film seen twice: 200 min
	f.play(t, "movie", 6_000_000, 0, 600_000) // a film abandoned at 10 min
	f.play(t, "episode", 1_800_000, 1, 0)     // an episode: 30 min
	f.play(t, "track", 200_000, 3, 0)         // a song on repeat
	f.play(t, "book_part", 3_600_000, 1, 0)   // an audiobook chapter

	st, err := f.st.ProfileStatistics(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	k := byKind(st)
	if got := k["movie"]; got.Started != 2 || got.Finished != 1 || got.Viewings != 2 ||
		got.WatchedMS != 2*6_000_000+600_000 {
		t.Errorf("movie = %+v", got)
	}
	if got := k["track"]; got.Started != 1 || got.Viewings != 3 || got.WatchedMS != 600_000 {
		t.Errorf("track = %+v; a song is listening, and on its own line", got)
	}
	if got := k["book_part"]; got.WatchedMS != 3_600_000 {
		t.Errorf("book_part = %+v", got)
	}

	var started, finished, viewings int
	var ms int64
	for _, x := range st.ByKind {
		started += x.Started
		finished += x.Finished
		viewings += x.Viewings
		ms += x.WatchedMS
	}
	if started != st.Started || finished != st.Finished || viewings != st.Viewings || ms != st.WatchedMS {
		t.Errorf("kinds add up to %d/%d/%d/%d, totals say %d/%d/%d/%d",
			started, finished, viewings, ms, st.Started, st.Finished, st.Viewings, st.WatchedMS)
	}
	if st.Earlier != nil {
		t.Errorf("earlier = %+v with nothing banked", st.Earlier)
	}
	// Most time first, so the page leads with what the account does most.
	if st.ByKind[0].Kind != "movie" {
		t.Errorf("first kind = %q, want the one with the most time", st.ByKind[0].Kind)
	}
}

func TestAnEmptyAccountHasNoKindsAndNoStart(t *testing.T) {
	st, err := newKindFixture(t).st.ProfileStatistics(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if st.ByKind == nil || len(st.ByKind) != 0 || st.FirstAt != nil {
		t.Errorf("stats = %+v, want an empty list and no first_at", st)
	}
}

// A reset's banked amount was never split by kind. It is reported as its own
// line, so the totals do not shrink and nothing is guessed into a kind.
func TestBankedTotalsAreTheirOwnLine(t *testing.T) {
	ctx := context.Background()
	f := newKindFixture(t)
	f.play(t, "movie", 6_000_000, 1, 0)
	before, err := f.st.ProfileStatistics(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.ResetHistory(ctx, f.user, HistoryAll, 0); err != nil {
		t.Fatal(err)
	}
	f.play(t, "track", 200_000, 1, 0)

	after, err := f.st.ProfileStatistics(ctx, f.user)
	if err != nil {
		t.Fatal(err)
	}
	if after.Earlier == nil || after.Earlier.WatchedMS != before.WatchedMS || after.Earlier.Kind != "" {
		t.Fatalf("earlier = %+v, want the banked film's time and no kind", after.Earlier)
	}
	if len(after.ByKind) != 1 || after.ByKind[0].Kind != "track" {
		t.Errorf("by_kind = %+v, want only the live song", after.ByKind)
	}
	if after.WatchedMS != before.WatchedMS+200_000 {
		t.Errorf("total = %d, want the banked film plus the song", after.WatchedMS)
	}
}

func TestMostPlayedIsPerGroupAndNeedsTwoPlays(t *testing.T) {
	ctx := context.Background()
	f := newKindFixture(t)
	film := f.play(t, "movie", 6_000_000, 3, 0)
	ep := f.play(t, "episode", 1_800_000, 2, 0)
	f.play(t, "movie", 6_000_000, 1, 0) // once: not "most played"
	songs := make([]int64, 0, 7)
	for i := 0; i < 7; i++ {
		songs = append(songs, f.play(t, "track", 200_000, 10+i, 0))
	}

	got, err := f.st.MostPlayed(ctx, f.user, 5)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, p := range got {
		ids = append(ids, p.Item.ID)
	}
	// Films and episodes first, most plays first; then five songs, not seven,
	// so an album on repeat cannot push the films off.
	want := []int64{film, ep, songs[6], songs[5], songs[4], songs[3], songs[2]}
	if len(ids) != len(want) {
		t.Fatalf("most played = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("most played = %v, want %v", ids, want)
		}
	}
	if got[0].Plays != 3 {
		t.Errorf("plays = %d, want 3", got[0].Plays)
	}
}
