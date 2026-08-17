package store

import (
	"context"
	"path/filepath"
	"testing"
)

/*
 * A track wears its album's sleeve.
 *
 * Found by counting blank tiles on a real library: **8,443 tracks** had no
 * artwork while their album did — which is every music tile on the home page,
 * in Continue Listening, and in search, all rendering as empty rectangles
 * beside film posters that worked. A row of blanks next to a row of posters
 * does not read as "music has no covers", it reads as a broken page.
 */
func TestATrackInheritsItsAlbumPoster(t *testing.T) {
	ctx := context.Background()
	st := queueStore(t)
	lib, err := st.CreateLibrary(ctx, "Music", "music", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	artistID, err := st.EnsureDerivedContainer(ctx, lib.ID, "artist",
		"k::artist=Band", "Band", "band", nil)
	if err != nil {
		t.Fatal(err)
	}
	albumID, err := st.EnsureDerivedContainer(ctx, lib.ID, "album",
		"k::album=Record", "Record", "record", &artistID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PutArtwork(ctx, albumID, "sleeve", "poster", "http://x/s.jpg", 10, 10, 1); err != nil {
		t.Fatal(err)
	}

	trackID := seedPending(t, st, lib.ID, "track", filepath.Join("m", "one.flac"), "One")
	if err := st.SetParent(ctx, trackID, &albumID); err != nil {
		t.Fatal(err)
	}

	items := []Item{{ID: trackID, Kind: "track", ParentID: &albumID}}
	if err := st.AttachArtwork(ctx, items); err != nil {
		t.Fatal(err)
	}
	if items[0].Artwork == nil || items[0].Artwork.Poster != "sleeve" {
		t.Errorf("track artwork = %+v, want the album's sleeve", items[0].Artwork)
	}
}

// A child that has its own image keeps it. Inheritance fills a gap; it does not
// overwrite an answer the item already had.
func TestAChildWithItsOwnPosterKeepsIt(t *testing.T) {
	ctx := context.Background()
	st := queueStore(t)
	lib, err := st.CreateLibrary(ctx, "Music", "music", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	albumID, err := st.EnsureDerivedContainer(ctx, lib.ID, "album",
		"k::album=Record", "Record", "record", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PutArtwork(ctx, albumID, "sleeve", "poster", "http://x/s.jpg", 10, 10, 1); err != nil {
		t.Fatal(err)
	}

	trackID := seedPending(t, st, lib.ID, "track", filepath.Join("m", "two.flac"), "Two")
	if err := st.PutArtwork(ctx, trackID, "its-own", "poster", "http://x/o.jpg", 10, 10, 1); err != nil {
		t.Fatal(err)
	}

	items := []Item{{ID: trackID, Kind: "track", ParentID: &albumID}}
	if err := st.AttachArtwork(ctx, items); err != nil {
		t.Fatal(err)
	}
	if items[0].Artwork.Poster != "its-own" {
		t.Errorf("poster = %q, want the track's own image kept", items[0].Artwork.Poster)
	}
}

// An item with no parent is left alone rather than erroring or borrowing from
// something unrelated.
func TestAnOrphanIsUntouched(t *testing.T) {
	ctx := context.Background()
	st := queueStore(t)
	lib, err := st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := seedPending(t, st, lib.ID, "movie", filepath.Join("f", "a.mkv"), "A Film")

	items := []Item{{ID: id, Kind: "movie"}}
	if err := st.AttachArtwork(ctx, items); err != nil {
		t.Fatal(err)
	}
	if items[0].Artwork != nil && items[0].Artwork.Poster != "" {
		t.Errorf("a parentless item gained a poster from nowhere: %+v", items[0].Artwork)
	}
}
