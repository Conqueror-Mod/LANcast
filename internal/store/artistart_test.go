package store

import (
	"context"
	"testing"
)

// putPoster gives an item a poster, the way the cover-art worker does.
func putPoster(t *testing.T, st *Store, itemID int64, hash string) {
	t.Helper()
	if err := st.PutArtwork(context.Background(), itemID, hash, "poster", "", 600, 600, 1000); err != nil {
		t.Fatalf("PutArtwork: %v", err)
	}
}

// artistOf returns an album's parent artist id.
func artistOf(t *testing.T, st *Store, albumID int64) int64 {
	t.Helper()
	var id int64
	err := st.db.QueryRow(`SELECT parent_id FROM media_item WHERE id = ?`, albumID).Scan(&id)
	if err != nil {
		t.Fatalf("parent of album %d: %v", albumID, err)
	}
	return id
}

func loadArtist(t *testing.T, st *Store, artistID int64) Item {
	t.Helper()
	items := []Item{{ID: artistID, Kind: "artist"}}
	if err := st.AttachArtwork(context.Background(), items); err != nil {
		t.Fatalf("AttachArtwork: %v", err)
	}
	return items[0]
}

func TestArtistBorrowsAnAlbumPoster(t *testing.T) {
	st := newStore(t)
	lib := musicLibrary(t, st)
	albumID := seedAlbum(t, st, lib, "An Artist", "A Record")
	putPoster(t, st, albumID, "album-hash")

	got := loadArtist(t, st, artistOf(t, st, albumID))
	if got.Artwork == nil || got.Artwork.Poster != "album-hash" {
		t.Fatalf("Artwork = %+v, want the album's poster", got.Artwork)
	}
	// Reported rather than hidden, so a client can treat it as the placeholder
	// it is and a provider's real image can supersede it later.
	if !got.Artwork.Inherited {
		t.Error("Inherited is false on a borrowed poster")
	}
}

// An artist with an image of its own must keep it. This is what makes the
// fallback safe to leave in place once a provider supplies real artist images.
func TestOwnArtistPosterIsNotOverwritten(t *testing.T) {
	st := newStore(t)
	lib := musicLibrary(t, st)
	albumID := seedAlbum(t, st, lib, "An Artist", "A Record")
	artistID := artistOf(t, st, albumID)
	putPoster(t, st, albumID, "album-hash")
	putPoster(t, st, artistID, "real-artist-photo")

	got := loadArtist(t, st, artistID)
	if got.Artwork.Poster != "real-artist-photo" {
		t.Errorf("Poster = %q, want the artist's own image", got.Artwork.Poster)
	}
	if got.Artwork.Inherited {
		t.Error("Inherited is true on an artist's own image")
	}
}

// The tile must not change its face between two scans that found the same
// thing, so the choice is deterministic: most tracks, then sort title, then id.
func TestBorrowedPosterPrefersTheAlbumWithMostTracks(t *testing.T) {
	st := newStore(t)
	lib := musicLibrary(t, st)

	single := seedAlbum(t, st, lib, "An Artist", "A Stray Single")
	record := seedAlbum(t, st, lib, "An Artist", "The Real Record")
	putPoster(t, st, single, "single-hash")
	putPoster(t, st, record, "record-hash")

	seedTrack(t, st, lib, single, `C:\m\single.mp3`, "Single", 1, 1)
	for i := 1; i <= 8; i++ {
		seedTrack(t, st, lib, record, `C:\m\rec`+string(rune('0'+i))+`.mp3`, "Track", 1, i)
	}

	got := loadArtist(t, st, artistOf(t, st, record))
	if got.Artwork == nil || got.Artwork.Poster != "record-hash" {
		t.Errorf("Poster = %+v, want the eight-track record over the stray single", got.Artwork)
	}
}

func TestBorrowedPosterIsStableAcrossReads(t *testing.T) {
	st := newStore(t)
	lib := musicLibrary(t, st)
	a := seedAlbum(t, st, lib, "An Artist", "Album A")
	b := seedAlbum(t, st, lib, "An Artist", "Album B")
	putPoster(t, st, a, "hash-a")
	putPoster(t, st, b, "hash-b")
	artistID := artistOf(t, st, a)

	first := loadArtist(t, st, artistID).Artwork.Poster
	for i := 0; i < 5; i++ {
		if got := loadArtist(t, st, artistID).Artwork.Poster; got != first {
			t.Fatalf("read %d gave %q, first gave %q — the tile would flicker", i, got, first)
		}
	}
}

// An artist whose albums all came back artless stays blank, rather than
// inventing something. Nothing to borrow is a real answer.
func TestArtistWithNoAlbumArtStaysBare(t *testing.T) {
	st := newStore(t)
	lib := musicLibrary(t, st)
	albumID := seedAlbum(t, st, lib, "An Artist", "A Record")

	got := loadArtist(t, st, artistOf(t, st, albumID))
	if got.Artwork != nil && got.Artwork.Poster != "" {
		t.Errorf("Artwork = %+v, want nothing borrowed when no album has art", got.Artwork)
	}
}

// Albums only, and present ones. An unmounted drive marks items missing rather
// than deleting them, and a tile should not keep wearing the cover of a record
// that is no longer there.
func TestMissingAlbumsAreNotBorrowedFrom(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)
	gone := seedAlbum(t, st, lib, "An Artist", "Gone Record")
	putPoster(t, st, gone, "gone-hash")
	if err := st.MarkMissing(ctx, []int64{gone}); err != nil {
		t.Fatal(err)
	}

	got := loadArtist(t, st, artistOf(t, st, gone))
	if got.Artwork != nil && got.Artwork.Poster != "" {
		t.Errorf("Artwork = %+v, want nothing borrowed from a missing album", got.Artwork)
	}
}

// The fallback must not touch anything that is not an artist — a film with no
// poster does not go looking through its children.
func TestNonArtistItemsAreUntouched(t *testing.T) {
	st := newStore(t)
	films := mustLibrary(t, st)
	id := seedItem(t, st, films, `C:\m\a.mkv`)

	items := []Item{{ID: id, Kind: "movie"}}
	if err := st.AttachArtwork(context.Background(), items); err != nil {
		t.Fatalf("AttachArtwork: %v", err)
	}
	if items[0].Artwork != nil && items[0].Artwork.Inherited {
		t.Error("a movie inherited a poster")
	}
}
