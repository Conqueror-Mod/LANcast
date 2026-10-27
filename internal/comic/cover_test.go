package comic_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/comic"
	"lancast/internal/comic/comictest"
	"lancast/internal/store"
)

type fakeStore struct {
	pending []store.Item
	posters map[int64]string
	checked map[int64]bool
}

func (f *fakeStore) PendingComicCovers(_ context.Context, _ int) ([]store.Item, error) {
	var out []store.Item
	for _, it := range f.pending {
		if !f.checked[it.ID] {
			out = append(out, it)
		}
	}
	return out, nil
}

func (f *fakeStore) PutArtwork(_ context.Context, id int64, hash, kind, _ string, _, _ int, _ int64) error {
	if kind == "poster" {
		f.posters[id] = hash
	}
	return nil
}

func (f *fakeStore) MarkArtworkChecked(_ context.Context, id int64) error {
	f.checked[id] = true
	return nil
}

type fakeCache struct{ bodies map[string][]byte }

func (c *fakeCache) Put(body []byte) (string, int, int, int64, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return "", 0, 0, 0, err
	}
	h := fmt.Sprintf("h%d", len(c.bodies))
	c.bodies[h] = body
	return h, cfg.Width, cfg.Height, int64(len(body)), nil
}

func TestTheCoverIsTheFirstPage(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.cbz")
	comictest.Write(t, good, 12)
	broken := filepath.Join(dir, "broken.cbz")
	os.WriteFile(broken, []byte("not a zip"), 0o644)

	st := &fakeStore{
		pending: []store.Item{{ID: 1, Path: good}, {ID: 2, Path: broken}},
		posters: map[int64]string{}, checked: map[int64]bool{},
	}
	cache := &fakeCache{bodies: map[string][]byte{}}
	w := comic.NewCoverWorker(st, cache, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !st.checked[1] || !st.checked[2] {
		t.Fatalf("both rows must be stamped, broken one included: %v", st.checked)
	}
	if _, ok := st.posters[2]; ok {
		t.Fatal("a broken archive got a poster")
	}
	img, _, err := image.Decode(bytes.NewReader(cache.bodies[st.posters[1]]))
	if err != nil {
		t.Fatal(err)
	}
	// Page 1, not page 10, though "p10.png" sorts first as plain text and
	// "p12.png" was written into the archive first.
	r, _, _, _ := img.At(5, 5).RGBA()
	if got := int(r >> 8); got < 0 || got > 4 {
		t.Fatalf("cover's red channel is %d, want about 1 (page one)", got)
	}
}

// A PDF's cover is its first page that is a picture: one that opens on a page
// of text (a title, a notice) takes the next (ADR 0102).
func TestAPDFsCoverIsItsFirstPicture(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.pdf")
	comictest.WritePDF(t, plain, 3, comictest.PDFOptions{})
	notice := filepath.Join(dir, "notice.pdf")
	comictest.WritePDF(t, notice, 3, comictest.PDFOptions{TextPage: 1, Compressed: true})
	for path, want := range map[string]int{plain: 1, notice: 2} {
		body, err := comic.Cover(path)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(path), err)
		}
		img, _, err := image.Decode(bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if r, _, _, _ := img.At(5, 5).RGBA(); int(r>>8)-want > 3 || want-int(r>>8) > 3 {
			t.Fatalf("%s: cover is page %d, want %d", filepath.Base(path), r>>8, want)
		}
	}
}

// A PDF with no picture on any page is a book of words, not a comic, and is
// refused when opened rather than read as a run of blank pages.
func TestAPDFOfTextIsNotAComic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "essay.pdf")
	comictest.WritePDF(t, path, 1, comictest.PDFOptions{TextPage: 1})
	if b, err := comic.OpenBook(path); err == nil {
		b.Close()
		t.Fatal("a PDF of text opened as a comic")
	}
}
