package scan

import (
	"context"
	"slices"
	"testing"

	"lancast/internal/probe"
	"lancast/internal/store"
)

// One tag can hold several genres. ID3v2.4 separates them with NUL, most
// taggers write "; ". A slash is left alone: "AC/DC" is not two artists, and
// "Drum/Bass" is one genre to the people who write it that way.
func TestSplitGenres(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"Jazz", []string{"Jazz"}},
		{"Jazz; Soul", []string{"Jazz", "Soul"}},
		{"Jazz\x00Soul", []string{"Jazz", "Soul"}},
		{" Rock ;; rock; ROCK ", []string{"Rock"}},
		{"Drum/Bass", []string{"Drum/Bass"}},
	}
	for _, c := range cases {
		if got := splitGenres(c.in); !slices.Equal(got, c.want) {
			t.Errorf("splitGenres(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// End to end: a scan keeps the tag, and the album and artist above it can be
// found by it. Until ADR 0114 the tag was read from every file and dropped.
func TestAScanKeepsGenresAndRollsThemUp(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "A/one.mp3", 10)
	writeFile(t, root, "A/two.mp3", 10)
	sc.SetTagReader(&fakeTags{byName: map[string]probe.Tags{
		"one.mp3": {Title: "One", Artist: "Band", AlbumArtist: "Band", Album: "Record", Genre: "Jazz"},
		"two.mp3": {Title: "Two", Artist: "Band", AlbumArtist: "Band", Album: "Record", Genre: "Jazz; Soul"},
	}})
	scanAndWait(t, sc, lib)

	ctx := context.Background()
	for _, kind := range []string{"album", "artist"} {
		items, _, err := st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID, Kind: kind, Genres: []string{"Soul"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 {
			t.Errorf("%s in Soul: %d, want 1", kind, len(items))
		}
	}
	tr := trackByPath(t, st, lib.ID, "one.mp3")
	if g, _ := st.Genres(ctx, tr.ID); !slices.Equal(g, []string{"Jazz"}) {
		t.Errorf("track genres = %v, want [Jazz]", g)
	}
}
