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

// Narrators come from the composer tag, every one of them, and a library read
// before the pass knew about them is read once more even when nothing on disk
// moved (ADR 0116 step 3). A credits lock keeps them as they are.
func TestBookPassReadsNarrators(t *testing.T) {
	sc, st := newScanner(t)
	tags := &bookTags{byName: map[string]probe.Tags{
		"Q-Squared 1.mp3": {Title: "Q-Squared 1", Composer: "John de Lancie; Leonard Nimoy", Track: 1},
		"Q-Squared 2.mp3": {Title: "Q-Squared 2", Composer: "John de Lancie", Track: 2},
	}}
	sc.SetTagReader(tags)
	ctx := context.Background()
	root := t.TempDir()
	lib, err := st.CreateLibrary(ctx, "Books", "audiobooks", root)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "The Next Generation", "Q-Squared")
	writeFile(t, dir, "Q-Squared 1.mp3", 10)
	writeFile(t, dir, "Q-Squared 2.mp3", 10)
	scanAndWait(t, sc, *lib)

	top, _, err := st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID, TopLevel: true})
	if err != nil || len(top) != 1 {
		t.Fatalf("books = %+v, err %v", top, err)
	}
	book := top[0].ID
	narrators := func() []string {
		t.Helper()
		credits, err := st.Credits(ctx, book)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range credits {
			if c.Role == store.RoleNarrator {
				out = append(out, c.Name)
			}
		}
		return out
	}
	if got := narrators(); len(got) != 2 || got[0] != "John de Lancie" || got[1] != "Leonard Nimoy" {
		t.Fatalf("narrators = %q, want John de Lancie then Leonard Nimoy", got)
	}

	// A library read before narrators existed: no credits, an older marker.
	if err := st.SetBookNarrators(ctx, book, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.SetBookTagsRev(ctx, lib.ID, 0); err != nil {
		t.Fatal(err)
	}
	if p := scanAndWait(t, sc, *lib); p.ItemsChanged != 0 {
		t.Fatalf("rescan changed %d files", p.ItemsChanged)
	}
	if got := narrators(); len(got) != 2 {
		t.Fatalf("an unchanged library was not read for narrators: %q", got)
	}

	// Locked, the credits stay as they are whatever the tags say next.
	if err := st.LockField(ctx, book, "credits"); err != nil {
		t.Fatal(err)
	}
	tags.mu.Lock()
	tags.byName["Q-Squared 1.mp3"] = probe.Tags{Title: "Q-Squared 1", Composer: "Somebody Else", Track: 1}
	tags.byName["Q-Squared 2.mp3"] = probe.Tags{Title: "Q-Squared 2", Composer: "Somebody Else", Track: 2}
	tags.mu.Unlock()
	if err := st.SetBookTagsRev(ctx, lib.ID, 0); err != nil {
		t.Fatal(err)
	}
	scanAndWait(t, sc, *lib)
	if got := narrators(); len(got) != 2 || got[0] != "John de Lancie" {
		t.Fatalf("a credits lock was overwritten: %q", got)
	}
}

// A book whose files sit in `Audio` inside the book's folder is named by the
// book's folder. A library scanned before that rule existed has books titled
// "Audio"; its next scan re-titles them in place, on the same row, though
// nothing on disk moved, and leaves a title somebody locked as they left it.
func TestRescanRetitlesBookInAudioFolder(t *testing.T) {
	sc, st := newScanner(t)
	sc.SetTagReader(&bookTags{byName: map[string]probe.Tags{}})
	ctx := context.Background()
	root := t.TempDir()
	lib, err := st.CreateLibrary(ctx, "Books", "audiobooks", root)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "Read-Along Adventures", "01 - Star Trek the Motion Picture (Read-Along)", "Audio")
	writeFile(t, dir, "01.mp3", 10)
	writeFile(t, dir, "02.mp3", 10)
	writeFile(t, filepath.Dir(dir), "Star_Trek_The_Motion_Picture.swf", 10)

	book := func() store.Item {
		t.Helper()
		top, _, err := st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID, TopLevel: true})
		if err != nil || len(top) != 1 {
			t.Fatalf("books = %+v, err %v", top, err)
		}
		return top[0]
	}
	// Read back before each rescan, as the server does: a library that has
	// never been scanned is always read in full, so the skip this test is
	// about only arises with ScannedAt set.
	rescan := func() Progress {
		t.Helper()
		l, err := st.GetLibrary(ctx, lib.ID)
		if err != nil {
			t.Fatal(err)
		}
		return scanAndWait(t, sc, *l)
	}
	const want = "Star Trek the Motion Picture (Read-Along)"
	scanAndWait(t, sc, *lib)
	first := book()
	if first.Title != want || deref(first.Series) != "Read-Along Adventures" || deref2(first.Episode) != 1 {
		t.Fatalf("book = %q, series %q, number %d", first.Title, deref(first.Series), deref2(first.Episode))
	}

	// As the live library has it: titled by the inner folder, read by the
	// pass before this rule.
	if err := st.SetBookDetails(ctx, first.ID, store.BookDetails{Title: "Audio", SortTitle: "audio"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetBookTagsRev(ctx, lib.ID, 1); err != nil {
		t.Fatal(err)
	}
	if p := rescan(); p.ItemsChanged != 0 {
		t.Fatalf("rescan changed %d files", p.ItemsChanged)
	}
	if got := book(); got.ID != first.ID || got.Title != want {
		t.Fatalf("after rescan: id %d title %q, want id %d title %q", got.ID, got.Title, first.ID, want)
	}

	// A locked title stays, whatever the folders say.
	if err := st.SetBookDetails(ctx, first.ID, store.BookDetails{Title: "My Name For It", SortTitle: "my name for it"}); err != nil {
		t.Fatal(err)
	}
	if err := st.LockField(ctx, first.ID, "title"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetBookTagsRev(ctx, lib.ID, 1); err != nil {
		t.Fatal(err)
	}
	rescan()
	if got := book(); got.Title != "My Name For It" {
		t.Fatalf("a locked title was overwritten: %q", got.Title)
	}
}
