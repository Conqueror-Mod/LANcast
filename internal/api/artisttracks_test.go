package api

import (
	"context"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

// The route answers with an artist's tracks under `items`, reaching through
// its albums, and 404s for an id that does not exist. The order itself is
// store.TracksOf's and is tested there.
func TestArtistTracksReachThroughAlbums(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	artist, err := h.st.UpsertItem(ctx, store.ScanFile{
		LibraryID: h.lib.ID, Path: filepath.Join(h.dir, "Band"), Kind: "artist",
		Title: "Band", SortTitle: "Band",
	})
	if err != nil {
		t.Fatal(err)
	}
	album, err := h.st.UpsertItem(ctx, store.ScanFile{
		LibraryID: h.lib.ID, Path: filepath.Join(h.dir, "Band", "Record"), Kind: "album",
		Title: "Record", SortTitle: "Record",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetParent(ctx, album, &artist); err != nil {
		t.Fatal(err)
	}
	track := func(name string) int64 {
		t.Helper()
		id, err := h.st.UpsertItem(ctx, store.ScanFile{
			LibraryID: h.lib.ID, Path: filepath.Join(h.dir, name), Kind: "track",
			Title: name, SortTitle: name, Container: "flac", SizeBytes: 1, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	onAlbum := track("song.flac")
	if err := h.st.SetParent(ctx, onAlbum, &album); err != nil {
		t.Fatal(err)
	}
	loose := track("single.flac")
	if err := h.st.SetParent(ctx, loose, &artist); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Items []store.Item `json:"items"`
	}
	decode(t, h.do(t, "GET", "/api/items/"+itoa(artist)+"/tracks", nil), &body)
	got := map[int64]bool{}
	for _, it := range body.Items {
		got[it.ID] = true
	}
	if len(body.Items) != 2 || !got[onAlbum] || !got[loose] {
		t.Fatalf("tracks = %v, want the album's track %d and the loose single %d", got, onAlbum, loose)
	}

	wantError(t, h.do(t, "GET", "/api/items/999999/tracks", nil), 404, "not_found")
	wantError(t, h.do(t, "GET", "/api/items/abc/tracks", nil), 400, "bad_request")
}
