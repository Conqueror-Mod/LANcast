package scan

import (
	"context"
	"testing"

	"lancast/internal/store"
)

/*
 * A photo loose at a second location's own root stays top-level.
 *
 * Galleries are folders and nothing else (ADR 0028), so a photo sitting
 * directly in a location's root belongs to no gallery. The check for that is a
 * comparison against the root — and against the library's *first* location it
 * can never match for a photo in the second, so instead of staying top-level
 * the photo is grouped into a gallery named after that drive's own folder
 * (ADR 0034).
 */
func TestPhotoLooseInASecondLocationGetsNoGallery(t *testing.T) {
	ctx := context.Background()
	sc, st := newScanner(t)
	lib, root := pictureFixture(t, st)

	second := t.TempDir()
	if _, err := st.AddRoot(ctx, lib.ID, second); err != nil {
		t.Fatal(err)
	}

	// One properly foldered photo in the first location, one loose in the
	// second. Only the first should produce a gallery.
	writeFile(t, root, "Holiday/beach.jpg", 10)
	writeFile(t, second, "loose.jpg", 10)

	scanAndWait(t, sc, lib)

	galleries, _, err := st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID, Kind: "gallery"})
	if err != nil {
		t.Fatal(err)
	}
	if len(galleries) != 1 {
		var names []string
		for _, g := range galleries {
			names = append(names, g.Title)
		}
		t.Fatalf("galleries = %v, want only Holiday — the loose photo invented one", names)
	}
	if galleries[0].Title != "Holiday" {
		t.Errorf("gallery = %q, want Holiday", galleries[0].Title)
	}

	// And the loose photo is genuinely top-level rather than merely ungrouped.
	photos, _, err := st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID, Kind: "photo"})
	if err != nil {
		t.Fatal(err)
	}
	for _, ph := range photos {
		if ph.Title == "loose" && ph.ParentID != nil {
			t.Error("the loose photo was parented to a gallery")
		}
	}
}

// A gallery folder in a second location groups exactly as one in the first.
func TestGalleryInASecondLocationGroups(t *testing.T) {
	ctx := context.Background()
	sc, st := newScanner(t)
	lib, _ := pictureFixture(t, st)

	second := t.TempDir()
	if _, err := st.AddRoot(ctx, lib.ID, second); err != nil {
		t.Fatal(err)
	}
	writeFile(t, second, "Wedding/one.jpg", 10)
	writeFile(t, second, "Wedding/two.jpg", 10)

	scanAndWait(t, sc, lib)

	galleries, _, err := st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID, Kind: "gallery"})
	if err != nil {
		t.Fatal(err)
	}
	if len(galleries) != 1 || galleries[0].Title != "Wedding" {
		t.Fatalf("galleries = %v, want one named Wedding", galleries)
	}
}
