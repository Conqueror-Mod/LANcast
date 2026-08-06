package store

import (
	"context"
	"testing"
)

// titles reads back the listing in order, which is the whole subject here.
func titles(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
	}
	return out
}

func sameOrder(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// An album plays in the order the record plays, which is not the order its
// titles happen to fall in alphabetically.
//
// The titles here are chosen to be exactly reversed: without the "track" sort
// this listing comes back Apple, Mango, Zebra — the default orders by
// sort_title first, and a track keeps its own title rather than inheriting a
// container's, so tracks never tie and never fall through to season/episode
// the way episodes do.
func TestAlbumTracksListInTrackOrder(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)
	albumID := seedAlbum(t, st, lib, "Some Band", "Some Album")

	seedTrack(t, st, lib, albumID, lib.Path+"/1.flac", "Zebra", 1, 1)
	seedTrack(t, st, lib, albumID, lib.Path+"/2.flac", "Mango", 1, 2)
	seedTrack(t, st, lib, albumID, lib.Path+"/3.flac", "Apple", 1, 3)

	items, _, err := st.ListItems(ctx, ItemFilter{
		LibraryID: lib.ID, ParentID: &albumID, Sort: "track",
	})
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	want := []string{"Zebra", "Mango", "Apple"}
	if got := titles(items); !sameOrder(got, want) {
		t.Errorf("track order = %v, want %v", got, want)
	}
}

// Disc leads track. A two-disc set whose second disc opens with track 1 must
// not put that track ahead of disc one — the failure a plain track-number sort
// produces, and the reason disc is stored at all.
func TestMultiDiscAlbumOrdersByDiscThenTrack(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)
	albumID := seedAlbum(t, st, lib, "Some Band", "A Double Record")

	// Seeded out of order, and titled so that alphabetical order is the exact
	// reverse of playing order — a test whose titles happen to sort the right
	// way passes without the sort doing anything.
	seedTrack(t, st, lib, albumID, lib.Path+"/d2t1.flac", "Bravo", 2, 1)
	seedTrack(t, st, lib, albumID, lib.Path+"/d1t2.flac", "Charlie", 1, 2)
	seedTrack(t, st, lib, albumID, lib.Path+"/d2t2.flac", "Alpha", 2, 2)
	seedTrack(t, st, lib, albumID, lib.Path+"/d1t1.flac", "Delta", 1, 1)

	items, _, err := st.ListItems(ctx, ItemFilter{
		LibraryID: lib.ID, ParentID: &albumID, Sort: "track",
	})
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	// Disc one in track order, then disc two — not both discs' track 1 first,
	// which is what a sort that forgot disc would produce.
	want := []string{"Delta", "Charlie", "Bravo", "Alpha"}
	if got := titles(items); !sameOrder(got, want) {
		t.Errorf("multi-disc order = %v, want %v", got, want)
	}
}

// The guard on the fix itself.
//
// The tempting way to order tracks is to lead the *default* with season and
// episode, since movies tie on both and would not notice. Episodes would: they
// share a series sort title, so a cross-show listing would come back with every
// show's season 1 interleaved ahead of any season 2. This pins the default
// listing so that change cannot be made quietly.
func TestDefaultSortKeepsEpisodesUnderTheirSeries(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib, err := st.CreateLibrary(ctx, "Shows", "show", t.TempDir())
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	// Two shows, each with a season 1 and a season 2 episode. Sort title is the
	// series, which is what the scanner writes for an episode.
	ep := func(path, series, title string, season, episode int) {
		t.Helper()
		if _, err := st.UpsertItem(ctx, ScanFile{
			LibraryID: lib.ID, Path: path, Kind: "episode",
			Title: title, SortTitle: series,
			Series: &series, Season: &season, Episode: &episode,
			Container: "mkv", SizeBytes: 1000, MTime: 500,
		}); err != nil {
			t.Fatalf("UpsertItem(%q): %v", path, err)
		}
	}
	ep(lib.Path+"/alpha-s02e01.mkv", "alpha", "Alpha S2", 2, 1)
	ep(lib.Path+"/beta-s01e01.mkv", "beta", "Beta S1", 1, 1)
	ep(lib.Path+"/alpha-s01e01.mkv", "alpha", "Alpha S1", 1, 1)
	ep(lib.Path+"/beta-s02e01.mkv", "beta", "Beta S2", 2, 1)

	items, _, err := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, Kind: "episode"})
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	// Each show's episodes stay together and in season order — not both shows'
	// season 1 followed by both shows' season 2.
	want := []string{"Alpha S1", "Alpha S2", "Beta S1", "Beta S2"}
	if got := titles(items); !sameOrder(got, want) {
		t.Errorf("default episode order = %v, want %v", got, want)
	}
}
