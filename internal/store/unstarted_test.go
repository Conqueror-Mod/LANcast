package store

import (
	"context"
	"path/filepath"
	"testing"
)

/*
 * status=unstarted: nothing begun in the item or in anything it holds.
 *
 * The home page's Unwatched shelf for a show library needs "you have not
 * started this show", and watched=false cannot answer it -- a show has no play
 * state of its own, so every show passes that filter.
 */
func TestUnstartedMeansNoEpisodeBegun(t *testing.T) {
	ctx := context.Background()
	st := queueStore(t)
	lib, err := st.CreateLibrary(ctx, "TV", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mkShow := func(name string) (show, episode int64) {
		t.Helper()
		show, _, err := st.EnsureShow(ctx, lib.ID, filepath.Join(lib.Path, name), name, name)
		if err != nil {
			t.Fatal(err)
		}
		season, _, err := st.EnsureSeason(ctx, lib.ID, show, 1,
			filepath.Join(lib.Path, name, "S1"), "Season 1", "season 1")
		if err != nil {
			t.Fatal(err)
		}
		s, e := 1, 1
		episode, err = st.UpsertItem(ctx, ScanFile{
			LibraryID: lib.ID, Path: filepath.Join(lib.Path, name, "S1", "e1.mkv"), Kind: "episode",
			Title: "Pilot", SortTitle: name, Series: &name, Season: &s, Episode: &e,
			Container: "mkv", SizeBytes: 1, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetParent(ctx, episode, &season); err != nil {
			t.Fatal(err)
		}
		return show, episode
	}
	fresh, _ := mkShow("fresh")
	begun, begunEp := mkShow("begun")
	finished, finishedEp := mkShow("finished")
	opened, openedEp := mkShow("opened")

	if err := st.SaveProgress(ctx, begunEp, "u1", 60_000, false); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProgress(ctx, finishedEp, "u1", 0, true); err != nil {
		t.Fatal(err)
	}
	// Opened and closed at once: a row with nothing in it began nothing.
	if err := st.SaveProgress(ctx, openedEp, "u1", 0, false); err != nil {
		t.Fatal(err)
	}

	got := map[int64]bool{}
	for _, id := range searchIDs(t, st, ItemFilter{LibraryID: lib.ID, TopLevel: true, Unstarted: true, UserID: "u1"}) {
		got[id] = true
	}
	if !got[fresh] || !got[opened] {
		t.Errorf("unstarted shows missing: fresh=%v opened=%v", got[fresh], got[opened])
	}
	if got[begun] || got[finished] {
		t.Errorf("a show with an episode begun passed: begun=%v finished=%v", got[begun], got[finished])
	}

	// Another account's viewing is not this one's.
	other := searchIDs(t, st, ItemFilter{LibraryID: lib.ID, TopLevel: true, Unstarted: true, UserID: "u2"})
	if len(other) != 4 {
		t.Errorf("for an account that has watched nothing, unstarted = %v, want all four", other)
	}
}
