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
