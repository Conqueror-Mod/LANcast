package store

import (
	"context"
	"path/filepath"
	"testing"
)

/*
 * A search finds an album and a song by name, not only the artist.
 *
 * A music library's top level is its artists, and a search held to the top
 * level answered "nothing matches" for every album and track typed by name —
 * in the library's own box and in Search everything alike.
 */
func seedBand(t *testing.T, st *Store) (lib *Library, artist, album, track int64) {
	t.Helper()
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "Music", "music", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	artist, err = st.EnsureDerivedContainer(ctx, lib.ID, "artist",
		"k::artist=Nine Inch Nails", "Nine Inch Nails", "nine inch nails", nil)
	if err != nil {
		t.Fatal(err)
	}
	album, err = st.EnsureDerivedContainer(ctx, lib.ID, "album",
		"k::album=The Fragile", "The Fragile", "fragile", &artist)
	if err != nil {
		t.Fatal(err)
	}
	track = seedPending(t, st, lib.ID, "track",
		filepath.Join("m", "day.flac"), "The Day the World Went Away")
	if err := st.SetParent(ctx, track, &album); err != nil {
		t.Fatal(err)
	}
	return lib, artist, album, track
}

func searchIDs(t *testing.T, st *Store, f ItemFilter) []int64 {
	t.Helper()
	items, _, err := st.ListItems(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	var out []int64
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func TestSearchFindsATrackAndAnAlbumByName(t *testing.T) {
	st := queueStore(t)
	lib, _, album, track := seedBand(t, st)

	for _, scope := range []struct {
		name    string
		library int64
	}{{"in the library", lib.ID}, {"across every library", 0}} {
		t.Run(scope.name, func(t *testing.T) {
			got := searchIDs(t, st, ItemFilter{LibraryID: scope.library, TopLevel: true,
				Query: "the day the world went away"})
			if len(got) != 1 || got[0] != track {
				t.Errorf("searching a song's title returned %v, want just the track %d", got, track)
			}
			got = searchIDs(t, st, ItemFilter{LibraryID: scope.library, TopLevel: true, Query: "fragile"})
			if len(got) != 1 || got[0] != album {
				t.Errorf("searching an album's title returned %v, want just the album %d", got, album)
			}
		})
	}
}

// Typing a band's name shows the band first, then its records, then songs.
func TestSearchPutsTheArtistBeforeItsAlbumsAndSongs(t *testing.T) {
	st := queueStore(t)
	ctx := context.Background()
	lib, artist, _, _ := seedBand(t, st)
	// Titles that sort ahead of the artist's, so only the kind ordering can
	// put the artist first.
	album, err := st.EnsureDerivedContainer(ctx, lib.ID, "album",
		"k::album=Nails A", "A Nails Record", "a nails record", &artist)
	if err != nil {
		t.Fatal(err)
	}
	track := seedPending(t, st, lib.ID, "track", filepath.Join("m", "a.flac"), "A Nails Song")
	if err := st.SetParent(ctx, track, &album); err != nil {
		t.Fatal(err)
	}

	got := searchIDs(t, st, ItemFilter{LibraryID: lib.ID, TopLevel: true, Query: "nails"})
	want := []int64{artist, album, track}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want artist, then album, then track: %v", got, want)
		}
	}
}

// The grid itself is unchanged: without a query it still lists artists only.
func TestBrowsingMusicStillListsArtistsOnly(t *testing.T) {
	st := queueStore(t)
	lib, artist, _, _ := seedBand(t, st)
	got := searchIDs(t, st, ItemFilter{LibraryID: lib.ID, TopLevel: true})
	if len(got) != 1 || got[0] != artist {
		t.Errorf("browsing the music grid returned %v, want only the artist %d", got, artist)
	}
}

/*
 * The shuffle behind the home page's Unwatched shelf holds still for one seed
 * and moves for another. A shelf that reshuffled on every refetch would move
 * tiles out from under the pointer.
 */
func TestRandomSortIsStableForOneSeed(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	for i := 0; i < 30; i++ {
		name := string(rune('a'+i%26)) + string(rune('a'+i/26))
		if _, err := st.UpsertItem(ctx, file(lib.ID, `C:\m\`+name+`.mkv`, name)); err != nil {
			t.Fatal(err)
		}
	}
	order := func(seed int64) []int64 {
		return searchIDs(t, st, ItemFilter{LibraryID: lib.ID, TopLevel: true, Sort: "random", Seed: seed})
	}
	a, again, other := order(7), order(7), order(8)
	if len(a) != 30 {
		t.Fatalf("a shuffled listing returned %d of 30 items", len(a))
	}
	same := func(x, y []int64) bool {
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	}
	if !same(a, again) {
		t.Error("the same seed gave two different orders")
	}
	if same(a, other) {
		t.Error("a different seed gave the same order; the shelf would never change")
	}
	title := searchIDs(t, st, ItemFilter{LibraryID: lib.ID, TopLevel: true})
	if same(a, title) {
		t.Error("the shuffle came back in title order")
	}
}

/*
 * An episode is found by its own title, and a show's name finds the show
 * alone. An episode carries its show's name as its series, so matching series
 * for children too would answer "Futurama" with the show and every episode.
 */
func TestSearchFindsAnEpisodeByTitleButNotEveryEpisodeByShow(t *testing.T) {
	ctx := context.Background()
	st := queueStore(t)
	lib, err := st.CreateLibrary(ctx, "TV", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	show, _, err := st.EnsureShow(ctx, lib.ID, filepath.Join(lib.Path, "Futurama"), "Futurama", "futurama")
	if err != nil {
		t.Fatal(err)
	}
	series := "Futurama"
	var eps []int64
	for i, title := range []string{"Space Pilot 3000", "The Series Has Landed"} {
		s, e := 1, i+1
		id, err := st.UpsertItem(ctx, ScanFile{
			LibraryID: lib.ID, Path: filepath.Join(lib.Path, "Futurama", title+".mkv"), Kind: "episode",
			Title: title, SortTitle: "futurama", Series: &series, Season: &s, Episode: &e,
			Container: "mkv", SizeBytes: 1, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetParent(ctx, id, &show); err != nil {
			t.Fatal(err)
		}
		eps = append(eps, id)
	}

	got := searchIDs(t, st, ItemFilter{TopLevel: true, Query: "space pilot"})
	if len(got) != 1 || got[0] != eps[0] {
		t.Errorf("searching an episode's title returned %v, want just that episode %d", got, eps[0])
	}
	got = searchIDs(t, st, ItemFilter{TopLevel: true, Query: "futurama"})
	if len(got) != 1 || got[0] != show {
		t.Errorf("searching the show's name returned %v, want the show alone", got)
	}
}

/*
 * A listing by kind offers only what is there.
 *
 * New Music asks for kind=artist, which skipped the missing rule the top-level
 * grid carries. An artist whose every track had been renamed away stayed on
 * the shelf as a dead tile.
 */
func TestAListingByKindLeavesOutMissingRows(t *testing.T) {
	ctx := context.Background()
	st := queueStore(t)
	_, artist, _, track := seedBand(t, st)
	if err := st.MarkMissing(ctx, []int64{track}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE media_item SET missing = 1 WHERE id = ?`, artist); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"artist", "track"} {
		if got := searchIDs(t, st, ItemFilter{Kind: kind, ExcludeMissing: true}); len(got) != 0 {
			t.Errorf("kind=%s listed %v, all of them missing", kind, got)
		}
		// Unasked, the rows are still there: kept, never deleted.
		if got := searchIDs(t, st, ItemFilter{Kind: kind}); len(got) != 1 {
			t.Errorf("kind=%s without the flag listed %v, want the missing row", kind, got)
		}
	}
}
