package comic

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lancast/internal/comic/pdf"
)

/*
 * A Book is a comic opened for reading, whichever file it is: a .cbz or a
 * .cb7, whose pages are the archive's images, or a .pdf, whose pages are the
 * one picture each page shows (ADR 0102). Everything past this point — the page routes,
 * the cover worker — asks for a page by its number and gets image bytes and
 * their type, and does not know which it had.
 */

// Book is an opened comic.
type Book interface {
	// Len is how many pages it has.
	Len() int
	// Page reads page n, numbered from 0, and says what type of image it is.
	Page(n int) ([]byte, string, error)
	Close() error
}

// UnreadablePage is a page that is there but is not a picture this can show,
// in words a reader can be told: "this page is text or drawing".
type UnreadablePage string

func (e UnreadablePage) Error() string { return string(e) }

// OpenBook opens the comic at path by its extension.
func OpenBook(path string) (Book, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return openPDF(path)
	case ".cb7":
		return openCB7(path)
	}
	zr, err := Open(path)
	if err != nil {
		return nil, err
	}
	pages, err := Pages(&zr.Reader)
	if err != nil {
		zr.Close()
		return nil, err
	}
	return &cbz{zr: zr, pages: pages}, nil
}

type cbz struct {
	zr    *zip.ReadCloser
	pages []*zip.File
}

func (b *cbz) Len() int { return len(b.pages) }

func (b *cbz) Page(n int) ([]byte, string, error) {
	if n < 0 || n >= len(b.pages) {
		return nil, "", fmt.Errorf("comic: no page %d", n)
	}
	body, err := ReadPage(b.pages[n])
	return body, ContentType(b.pages[n].Name), err
}

func (b *cbz) Close() error { return b.zr.Close() }

type pdfBook struct {
	f     *os.File
	pages []pdf.Page
}

func openPDF(path string) (Book, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("comic: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("comic: %w", err)
	}
	pages, err := pdf.Index(f, info.Size())
	if err != nil {
		f.Close()
		return nil, err
	}
	readable := 0
	for _, p := range pages {
		if p.Err == "" {
			readable++
		}
	}
	// A PDF with no page that is a picture is a book of text, not a comic,
	// and says so rather than opening to a run of blank pages.
	if readable == 0 {
		f.Close()
		return nil, fmt.Errorf("comic: no page of this PDF is a picture (%s)", pages[0].Err)
	}
	return &pdfBook{f: f, pages: pages}, nil
}

func (b *pdfBook) Len() int { return len(b.pages) }

func (b *pdfBook) Page(n int) ([]byte, string, error) {
	if n < 0 || n >= len(b.pages) {
		return nil, "", fmt.Errorf("comic: no page %d", n)
	}
	p := b.pages[n]
	if p.Err != "" {
		return nil, "", UnreadablePage(p.Err)
	}
	body, err := pdf.ReadPage(b.f, p)
	return body, p.ContentType(), err
}

func (b *pdfBook) Close() error { return b.f.Close() }
