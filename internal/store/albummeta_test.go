package store

import (
	"context"
	"testing"
)

// setTrackYear puts a year on a track, the way applyTrackTags does from a date
// tag, so the album can derive one from it.
func setTrackYear(t *testing.T, st *Store, id int64, year int) {
	t.Helper()
	if _, err := st.db.ExecContext(context.Background(),
		`UPDATE media_item SET year = ? WHERE id = ?`, year, id); err != nil {
		t.Fatalf("set year: %v", err)
	}
}

func albumRow(t *testing.T, st *Store, id int64) Item {
	t.Helper()
	items, _, err := st.ListItems(context.Background(), ItemFilter{Kind: "album"})
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	for _, it := range items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("album %d not found", id)
	return Item{}
}

// The album detail page's whole problem: the row had a title and nothing else,
// so the page showed a cover over empty space and the Year sort had nothing to
// sort by. Both facts live one row away.
func TestAlbumTakesArtistAndYearFromItsTracks(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)
	albumID := seedAlbum(t, st, lib, "Between the Buried and Me", "Colors II")

	a := seedTrack(t, st, lib, albumID, lib.Path+"/1.mp3", "Fix the Error", 1, 1)
	b := seedTrack(t, st, lib, albumID, lib.Path+"/2.mp3", "Revolution in Limbo", 1, 3)
	setTrackYear(t, st, a, 2021)
	setTrackYear(t, st, b, 2022) // a single tagged with its own date

	if _, err := st.FillAlbumMetadata(ctx, lib.ID); err != nil {
		t.Fatalf("FillAlbumMetadata: %v", err)
	}

	got := albumRow(t, st, albumID)
	if got.Artist == nil || *got.Artist != "Between the Buried and Me" {
		t.Errorf("album artist = %v, want the parent artist's name", got.Artist)
	}
	// The earliest year is the record's; a later single should not move it.
	if got.Year == nil || *got.Year != 2021 {
		t.Errorf("album year = %v, want 2021 (earliest of its tracks)", got.Year)
	}
}

// A library of YouTube rips has tracks with no year at all. That must leave the
// album's year empty rather than writing a zero, which would sort as if the
// record came out in year nought.
func TestAlbumWithNoDatedTracksGetsNoYear(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)
	albumID := seedAlbum(t, st, lib, "Some Band", "Untagged Rips")
	seedTrack(t, st, lib, albumID, lib.Path+"/1.mp3", "One", 0, 0)

	if _, err := st.FillAlbumMetadata(ctx, lib.ID); err != nil {
		t.Fatalf("FillAlbumMetadata: %v", err)
	}

	got := albumRow(t, st, albumID)
	if got.Year != nil {
		t.Errorf("album year = %v, want nil when no track carries one", *got.Year)
	}
	// The artist is still known: it comes from the parent row, not the tags.
	if got.Artist == nil || *got.Artist != "Some Band" {
		t.Errorf("album artist = %v, want the parent artist even with no dates", got.Artist)
	}
}

// A rescan re-derives, so an album that had nothing is fixed by adding one
// correctly tagged track — without a special case for "was empty before".
func TestRescanFillsAnAlbumThatHadNothing(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)
	albumID := seedAlbum(t, st, lib, "Some Band", "A Record")
	seedTrack(t, st, lib, albumID, lib.Path+"/1.mp3", "Untagged", 0, 0)

	if _, err := st.FillAlbumMetadata(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	if got := albumRow(t, st, albumID); got.Year != nil {
		t.Fatalf("precondition: album already has year %v", *got.Year)
	}

	dated := seedTrack(t, st, lib, albumID, lib.Path+"/2.mp3", "Dated", 1, 2)
	setTrackYear(t, st, dated, 1998)
	if _, err := st.FillAlbumMetadata(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}

	if got := albumRow(t, st, albumID); got.Year == nil || *got.Year != 1998 {
		t.Errorf("album year after rescan = %v, want 1998", got.Year)
	}
}

// The permanent rule: a locked field is never overwritten, by anything
// (CLAUDE.md). An operator who corrected an album's year keeps it.
func TestFillAlbumMetadataRespectsALock(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)
	albumID := seedAlbum(t, st, lib, "Some Band", "A Record")
	tr := seedTrack(t, st, lib, albumID, lib.Path+"/1.mp3", "One", 1, 1)
	setTrackYear(t, st, tr, 2005)

	// The operator says the record is from 1999 and locks it.
	if _, err := st.db.ExecContext(ctx,
		`UPDATE media_item SET year = 1999 WHERE id = ?`, albumID); err != nil {
		t.Fatal(err)
	}
	if err := st.LockField(ctx, albumID, "year"); err != nil {
		t.Fatalf("LockField: %v", err)
	}

	if _, err := st.FillAlbumMetadata(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}

	if got := albumRow(t, st, albumID); got.Year == nil || *got.Year != 1999 {
		t.Errorf("album year = %v, want the locked 1999 — a lock outranks derivation", got.Year)
	}
}
