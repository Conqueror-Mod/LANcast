package scan

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"lancast/internal/probe"
	"lancast/internal/store"
)

// bookTags is fakeTags made safe for the book pass, which reads concurrently.
type bookTags struct {
	mu     sync.Mutex
	byName map[string]probe.Tags
}

func (b *bookTags) ReadTags(_ context.Context, path string) (probe.Tags, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.byName[filepath.Base(path)], nil
}

// An audiobooks library makes one book per folder of parts, titled and placed
// from the folder, with its parts in order beneath it (ADR 0116).
func TestAudiobooksLibraryMakesBooksOfFolders(t *testing.T) {
	sc, st := newScanner(t)
	sc.SetTagReader(&bookTags{byName: map[string]probe.Tags{
		"Emissary 1of2.mp3": {Title: "Emissary 1of2", Artist: "Star Trek", Track: 1},
		"Emissary 2of2.mp3": {Title: "Emissary 2of2", Artist: "Star Trek", Track: 2},
		"Standalone.m4b":    {Title: "Standalone", AlbumArtist: "Peel, John"},
	}})
	ctx := context.Background()
	root := t.TempDir()
	lib, err := st.CreateLibrary(ctx, "Books", "audiobooks", root)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "Deep Space Nine", "01 - Emissary (Abridged Novelization)")
	writeFile(t, dir, "Emissary 2of2.mp3", 10)
	writeFile(t, dir, "Emissary 1of2.mp3", 10)
	writeFile(t, root, "Standalone.m4b", 10)
	// A film somebody left here is counted and not indexed.
	writeFile(t, root, "Show S01E02.mkv", 10)

	p := scanAndWait(t, sc, *lib)
	if p.FilesSeen != 3 || p.SkippedKind != 1 {
		t.Fatalf("FilesSeen = %d, SkippedKind = %d, want 3 and 1", p.FilesSeen, p.SkippedKind)
	}

	top, _, err := st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID, TopLevel: true})
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]store.Item{}
	for _, it := range top {
		if it.Kind != "book" {
			t.Errorf("top level holds a %s (%q); only books belong there", it.Kind, it.Title)
		}
		byTitle[it.Title] = it
	}
	if len(top) != 2 {
		t.Fatalf("%d top-level items, want 2 books: %v", len(top), byTitle)
	}

	em, ok := byTitle["Emissary"]
	if !ok {
		t.Fatalf("no Emissary book: %v", byTitle)
	}
	if deref(em.Series) != "Deep Space Nine" || deref2(em.Episode) != 1 ||
		deref(em.Edition) != "Abridged" || deref(em.Artist) != "Star Trek" {
		t.Errorf("emissary = series %q, number %d, edition %q, author %q",
			deref(em.Series), deref2(em.Episode), deref(em.Edition), deref(em.Artist))
	}
	parts, _, err := st.ListItems(ctx, store.ItemFilter{ParentID: &em.ID, Sort: "track"})
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || filepath.Base(parts[0].Path) != "Emissary 1of2.mp3" ||
		parts[0].Title != "Part 1" || deref2(parts[1].Episode) != 2 {
		t.Fatalf("parts out of order or misnamed: %+v", parts)
	}

	if sa := byTitle["Standalone"]; deref(sa.Artist) != "John Peel" {
		t.Errorf("standalone author = %q, want John Peel", deref(sa.Artist))
	}

	// A rescan with nothing moved leaves the same books, and a moved file
	// follows its folder.
	if p := scanAndWait(t, sc, *lib); p.ItemsChanged != 0 {
		t.Fatalf("unchanged rescan changed %d items", p.ItemsChanged)
	}
	if err := os.Remove(filepath.Join(root, "Standalone.m4b")); err != nil {
		t.Fatal(err)
	}
	scanAndWait(t, sc, *lib)
	top, _, _ = st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID, TopLevel: true})
	if len(top) != 1 || top[0].Title != "Emissary" {
		t.Fatalf("a book whose only file went is still listed: %+v", top)
	}
}

// A music library still skips an .m4b, so a book cannot land among songs.
func TestMusicLibrarySkipsM4B(t *testing.T) {
	sc, st := newScanner(t)
	root := t.TempDir()
	lib, err := st.CreateLibrary(context.Background(), "Music", "music", root)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "Book.m4b", 10)
	if p := scanAndWait(t, sc, *lib); p.FilesSeen != 0 {
		t.Fatalf("FilesSeen = %d, want 0", p.FilesSeen)
	}
}
