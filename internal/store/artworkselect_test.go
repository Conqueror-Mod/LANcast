package store

import (
	"context"
	"path/filepath"
	"testing"
)

/*
 * Correcting a match has to change the picture.
 *
 * Reported from the desktop app: a show matched to the wrong title was fixed by
 * hand and kept its old thumbnail. The cause is that item_artwork's primary key
 * includes artwork_id, so INSERT OR REPLACE inserts a *second* row whenever the
 * new image has a different hash — which is every corrected match — leaving two
 * rows selected for one kind. Both readers assign in row order with no ORDER BY,
 * so the winner is whatever SQLite returns last.
 */
func TestNewArtworkReplacesThePreviousSelection(t *testing.T) {
	ctx := context.Background()
	st := queueStore(t)
	lib, err := st.CreateLibrary(ctx, "TV", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := seedPending(t, st, lib.ID, "show", filepath.Join("tv", "Storm"), "Storm of the Century")

	if err := st.PutArtwork(ctx, id, "hash-wrong", "poster", "http://x/wrong.jpg", 10, 15, 100); err != nil {
		t.Fatal(err)
	}
	if err := st.PutArtwork(ctx, id, "hash-right", "poster", "http://x/right.jpg", 10, 15, 100); err != nil {
		t.Fatal(err)
	}

	art, err := st.ItemArtwork(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if art == nil || art.Poster != "hash-right" {
		t.Errorf("poster = %v, want hash-right — the corrected match kept the old image", art)
	}

	// The grid reads through a different query, and the two must not be able to
	// disagree: that is what "whichever row came last" allows.
	items := []Item{{ID: id}}
	if err := st.AttachArtwork(ctx, items); err != nil {
		t.Fatal(err)
	}
	if items[0].Artwork == nil || items[0].Artwork.Poster != "hash-right" {
		t.Errorf("grid poster = %v, want hash-right", items[0].Artwork)
	}

	// Exactly one selected poster, or the ambiguity is merely hidden by row
	// order rather than removed.
	var n int
	if err := st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM item_artwork WHERE item_id = ? AND kind = 'poster' AND selected = 1`,
		id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("selected posters = %d, want exactly 1", n)
	}
}

// Kinds are independent: a new poster must not deselect the fanart.
func TestReplacingOneKindLeavesTheOthers(t *testing.T) {
	ctx := context.Background()
	st := queueStore(t)
	lib, err := st.CreateLibrary(ctx, "TV", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := seedPending(t, st, lib.ID, "show", filepath.Join("tv", "Show"), "A Show")

	if err := st.PutArtwork(ctx, id, "fan-1", "fanart", "http://x/f.jpg", 20, 10, 200); err != nil {
		t.Fatal(err)
	}
	if err := st.PutArtwork(ctx, id, "poster-1", "poster", "http://x/p1.jpg", 10, 15, 100); err != nil {
		t.Fatal(err)
	}
	if err := st.PutArtwork(ctx, id, "poster-2", "poster", "http://x/p2.jpg", 10, 15, 100); err != nil {
		t.Fatal(err)
	}

	art, err := st.ItemArtwork(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if art.Poster != "poster-2" {
		t.Errorf("poster = %q, want poster-2", art.Poster)
	}
	if art.Fanart != "fan-1" {
		t.Errorf("fanart = %q, want fan-1 — replacing a poster cleared another kind", art.Fanart)
	}
}

// Re-storing the same image is not a change and must stay selected.
func TestRestoringTheSameArtworkIsIdempotent(t *testing.T) {
	ctx := context.Background()
	st := queueStore(t)
	lib, err := st.CreateLibrary(ctx, "TV", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := seedPending(t, st, lib.ID, "show", filepath.Join("tv", "Same"), "Same")

	for i := 0; i < 3; i++ {
		if err := st.PutArtwork(ctx, id, "hash-same", "poster", "http://x/p.jpg", 10, 15, 100); err != nil {
			t.Fatal(err)
		}
	}

	art, err := st.ItemArtwork(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if art == nil || art.Poster != "hash-same" {
		t.Errorf("poster = %v, want hash-same still selected", art)
	}
}
