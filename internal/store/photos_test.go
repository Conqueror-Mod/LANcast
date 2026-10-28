package store

import (
	"context"
	"testing"
)

// ffprobe is never handed a photo: the photo worker records its size as it
// is seen, and a probe written over it recorded a rotated photo's stored
// size, nothing for some JPEGs, and a RAW's TIFF thumbnail (ADR 0107). A film
// beside it is still probed, so the empty answer is the rule, not the store.
func TestPhotosAreNotProbed(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	lib, err := s.CreateLibrary(ctx, "Pictures", "picture", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []ScanFile{
		{LibraryID: lib.ID, Path: lib.Path + "/a.jpg", Kind: "photo", Title: "a", SortTitle: "a", Container: "jpg", SizeBytes: 1, MTime: 1},
		{LibraryID: lib.ID, Path: lib.Path + "/b.nef", Kind: "photo", Title: "b", SortTitle: "b", Container: "nef", SizeBytes: 1, MTime: 1},
		{LibraryID: lib.ID, Path: lib.Path + "/film.mkv", Kind: "movie", Title: "film", SortTitle: "film", Container: "mkv", SizeBytes: 1, MTime: 1},
	} {
		if _, err := s.UpsertItem(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	items, err := s.PendingProbe(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind != "movie" {
		t.Fatalf("pending probe: %d items, want only the film", len(items))
	}
}
