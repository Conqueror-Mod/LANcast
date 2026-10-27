package pdf_test

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"testing"

	"lancast/internal/comic/comictest"
	"lancast/internal/comic/pdf"
)

// red is a page's picture's red channel at its top-left corner: the page
// number the builder painted into it.
func red(t *testing.T, body []byte) int {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r, _, _, _ := img.At(1, 1).RGBA()
	return int(r >> 8)
}

func near(a, b int) bool { return a-b <= 2 && b-a <= 2 }

// index reads a built file's pages and checks every one is its own picture,
// in order.
func index(t *testing.T, file []byte, n int, ctype string) []pdf.Page {
	t.Helper()
	pages, err := pdf.Index(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != n {
		t.Fatalf("%d pages, want %d", len(pages), n)
	}
	for i, p := range pages {
		if p.Err != "" {
			continue
		}
		if p.ContentType() != ctype {
			t.Fatalf("page %d is %s, want %s", i, p.ContentType(), ctype)
		}
		body, err := pdf.ReadPage(bytes.NewReader(file), p)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		if got := red(t, body); !near(got, i+1) {
			t.Fatalf("page %d shows page %d's picture", i, got)
		}
	}
	return pages
}

// The common case: a JPEG a page, a plain cross-reference table, objects
// written out of page order, a two-level page tree.
func TestEachPageIsItsJPEGInOrder(t *testing.T) {
	index(t, comictest.PDF(t, 9, comictest.PDFOptions{}), 9, "image/jpeg")
}

// A JPEG is served as the file holds it, not decoded and re-encoded.
func TestAJPEGPageIsTheFilesOwnBytes(t *testing.T) {
	file := comictest.PDF(t, 2, comictest.PDFOptions{})
	pages, err := pdf.Index(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := pdf.ReadPage(bytes.NewReader(file), pages[0])
	if !bytes.Contains(file, body) || !bytes.HasPrefix(body, []byte{0xff, 0xd8}) {
		t.Fatal("the page is not the JPEG in the file")
	}
}

// PDF 1.5's shape: page objects compressed into an object stream, the index a
// stream with a PNG predictor on it, and lossless pictures made into PNGs.
func TestCompressedObjectsAndLosslessPages(t *testing.T) {
	index(t, comictest.PDF(t, 7, comictest.PDFOptions{Compressed: true, Flate: true}), 7, "image/png")
	index(t, comictest.PDF(t, 4, comictest.PDFOptions{Compressed: true}), 4, "image/jpeg")
}

// A lossless page comes out exactly, gradient and all: the predictor is undone
// row by row, not approximately.
func TestALosslessPageIsExact(t *testing.T) {
	file := comictest.PDF(t, 1, comictest.PDFOptions{Flate: true})
	pages, _ := pdf.Index(bytes.NewReader(file), int64(len(file)))
	body, err := pdf.ReadPage(bytes.NewReader(file), pages[0])
	if err != nil {
		t.Fatal(err)
	}
	img, _, _ := image.Decode(bytes.NewReader(body))
	for _, y := range []int{0, 1, 30, 59} {
		r, g, b, _ := img.At(17, y).RGBA()
		if r>>8 != 1 || g>>8 != 80 || int(b>>8) != 120+y {
			t.Fatalf("row %d is %d,%d,%d", y, r>>8, g>>8, b>>8)
		}
	}
}

// Offsets a few bytes wrong are common in the wild; the file is read anyway,
// by finding its objects, as every viewer does.
func TestABrokenIndexIsRebuilt(t *testing.T) {
	index(t, comictest.PDF(t, 6, comictest.PDFOptions{BreakXref: true}), 6, "image/jpeg")
	index(t, comictest.PDF(t, 6, comictest.PDFOptions{BreakXref: true, Compressed: true}), 6, "image/jpeg")
}

// A page of text is a page, numbered in its place, that says why it is not
// shown; the pages either side of it are unaffected.
func TestATextPageIsKeptInPlaceAndSaysWhy(t *testing.T) {
	pages := index(t, comictest.PDF(t, 5, comictest.PDFOptions{TextPage: 3}), 5, "image/jpeg")
	if !strings.Contains(pages[2].Err, "text or drawing") {
		t.Fatalf("page 3: %q", pages[2].Err)
	}
	if _, err := pdf.ReadPage(bytes.NewReader(nil), pages[2]); err == nil {
		t.Fatal("an unreadable page read")
	}
}

// Resources set on the page tree reach the pages beneath it.
func TestInheritedResources(t *testing.T) {
	index(t, comictest.PDF(t, 3, comictest.PDFOptions{Inherit: true}), 3, "image/jpeg")
}

// A publisher's mark beside the page is ignored; a page cut into two equal
// pictures is not guessed at.
func TestALogoIsIgnoredAndHalvesAreRefused(t *testing.T) {
	index(t, comictest.PDF(t, 3, comictest.PDFOptions{Logo: true}), 3, "image/jpeg")
	file := comictest.PDF(t, 2, comictest.PDFOptions{Halves: true})
	pages, err := pdf.Index(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range pages {
		if !strings.Contains(p.Err, "several pictures") {
			t.Fatalf("page %d: %q", i, p.Err)
		}
	}
}

func TestAnEncryptedFileIsRefused(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		file := comictest.PDF(t, 2, comictest.PDFOptions{Encrypt: true, Compressed: compressed})
		if _, err := pdf.Index(bytes.NewReader(file), int64(len(file))); !errors.Is(err, pdf.ErrEncrypted) {
			t.Fatalf("compressed=%v: %v", compressed, err)
		}
	}
}

// A page tree that lists itself ends rather than recursing for ever.
func TestAPageTreeLoopEnds(t *testing.T) {
	index(t, comictest.PDF(t, 4, comictest.PDFOptions{Loop: true}), 4, "image/jpeg")
}

func TestNotAPDF(t *testing.T) {
	good := comictest.PDF(t, 3, comictest.PDFOptions{})
	for name, file := range map[string][]byte{
		"empty":     nil,
		"text":      []byte("hello, this is not a pdf"),
		"truncated": good[:len(good)/3],
		"header":    []byte("%PDF-1.7\n%%EOF\n"),
	} {
		if _, err := pdf.Index(bytes.NewReader(file), int64(len(file))); err == nil {
			t.Errorf("%s: read as a PDF", name)
		}
	}
}

// A file from anywhere is read by this, so no file may panic it or keep it
// busy: the fuzzer starts from every shape above.
func FuzzIndex(f *testing.F) {
	t := &testing.T{}
	for _, o := range []comictest.PDFOptions{
		{}, {Compressed: true, Flate: true}, {BreakXref: true}, {TextPage: 1, Inherit: true}, {Loop: true, Logo: true},
	} {
		f.Add(comictest.PDF(t, 2, o))
	}
	f.Fuzz(func(t *testing.T, file []byte) {
		pages, err := pdf.Index(bytes.NewReader(file), int64(len(file)))
		if err != nil {
			return
		}
		for _, p := range pages {
			_, _ = pdf.ReadPage(bytes.NewReader(file), p)
		}
	})
}
