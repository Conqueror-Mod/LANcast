package scan

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/comic/comictest"
	"lancast/internal/store"
)

// A comics library takes .cbz, .cb7 and .pdf files as comics, titled from
// the filename, and nothing else (ADR 0101, ADR 0102).
func TestComicsLibraryScansCBZAndPDF(t *testing.T) {
	sc, st := newScanner(t)
	root := t.TempDir()
	lib, err := st.CreateLibrary(context.Background(), "Comics", "comics", root)
	if err != nil {
		t.Fatal(err)
	}
	comictest.Write(t, filepath.Join(root, "Saga", "Saga 001 (2012) (Digital).cbz"), 3)
	comictest.Write(t, filepath.Join(root, "Watchmen.cbz"), 2)
	comictest.WritePDF(t, filepath.Join(root, "Star Courier", "Star Courier 001 (2024).pdf"), 4, comictest.PDFOptions{})
	cb7, err := os.ReadFile("../comic/testdata/twelve.cb7")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Bone 001 (1991).cb7"), cb7, 0o644); err != nil {
		t.Fatal(err)
	}
	// Not comics: a RAR archive, a readme, and a film somebody left here. The
	// S01E02 name is the trap: no comic is an episode.
	writeFile(t, root, "Bone.cbr", 10)
	writeFile(t, root, "readme.txt", 10)
	writeFile(t, root, "Show S01E02.mkv", 10)

	p := scanAndWait(t, sc, *lib)
	if p.FilesSeen != 4 {
		t.Errorf("FilesSeen = %d, want 4", p.FilesSeen)
	}
	// The .cbr and the film are counted, so a library of either says why it
	// is empty; the readme is not.
	if p.SkippedKind != 2 {
		t.Errorf("SkippedKind = %d, want 2", p.SkippedKind)
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
	if len(items) != 4 {
		t.Errorf("%d items, want 4", len(items))
	}
	if bone := byName["Bone 001 (1991).cb7"]; bone.Title != "Bone 001" || bone.Year == nil || *bone.Year != 1991 {
		t.Errorf("cb7: %q %v", bone.Title, bone.Year)
	}
	star := byName["Star Courier 001 (2024).pdf"]
	if star.Title != "Star Courier 001" || star.Series == nil || *star.Series != "Star Courier" {
		t.Errorf("pdf: %q %v", star.Title, star.Series)
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
	if err != nil || len(covers) != 4 {
		t.Errorf("covers pending: %d %v", len(covers), err)
	}
}
