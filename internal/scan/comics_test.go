package scan

import (
	"context"
	"path/filepath"
	"testing"

	"lancast/internal/comic/comictest"
	"lancast/internal/store"
)

// A comics library takes .cbz files as comics, titled from the filename, and
// nothing else (ADR 0101).
func TestComicsLibraryScansCBZ(t *testing.T) {
	sc, st := newScanner(t)
	root := t.TempDir()
	lib, err := st.CreateLibrary(context.Background(), "Comics", "comics", root)
	if err != nil {
		t.Fatal(err)
	}
	comictest.Write(t, filepath.Join(root, "Saga", "Saga 001 (2012) (Digital).cbz"), 3)
	comictest.Write(t, filepath.Join(root, "Watchmen.cbz"), 2)
	// Not comics: a RAR archive, a readme, and a film somebody left here. The
	// S01E02 name is the trap: no comic is an episode.
	writeFile(t, root, "Bone.cbr", 10)
	writeFile(t, root, "readme.txt", 10)
	writeFile(t, root, "Show S01E02.mkv", 10)

	p := scanAndWait(t, sc, *lib)
	if p.FilesSeen != 2 {
		t.Errorf("FilesSeen = %d, want 2", p.FilesSeen)
	}
	items, _, err := st.ListItems(context.Background(), store.ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]store.Item{}
	for _, it := range items {
		if it.Kind != "comic" {
			t.Errorf("%s is a %s", it.Title, it.Kind)
		}
		byName[filepath.Base(it.Path)] = it
	}
	saga := byName["Saga 001 (2012) (Digital).cbz"]
	if saga.Title != "Saga 001" || saga.Year == nil || *saga.Year != 2012 {
		t.Errorf("saga: %q %v", saga.Title, saga.Year)
	}
	if saga.Series == nil || *saga.Series != "Saga" {
		t.Errorf("saga series: %v", saga.Series)
	}
	if len(items) != 2 {
		t.Errorf("%d items, want 2", len(items))
	}

	// No metadata provider and no ffprobe will ever be handed a comic.
	pending, err := st.PendingEnrichment(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range pending {
		if it.Kind == "comic" {
			t.Errorf("%s is queued for enrichment", it.Title)
		}
	}
	probes, err := st.PendingProbe(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range probes {
		if it.Kind == "comic" {
			t.Errorf("%s is queued for ffprobe", it.Title)
		}
	}
	covers, err := st.PendingComicCovers(context.Background(), 100)
	if err != nil || len(covers) != 2 {
		t.Errorf("covers pending: %d %v", len(covers), err)
	}
}
