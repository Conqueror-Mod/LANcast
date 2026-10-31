// Package comic reads comic book archives (ADR 0101): a .cbz is a zip of page
// images, and its pages are its image entries in name order.
package comic

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"unicode"
)

// Limits on what an archive may claim. A comic has tens of pages, a thick
// omnibus a few thousand; a page is a few megabytes. Anything past these is
// not a comic, and a page is read into memory only up to MaxPageBytes.
const (
	MaxPages     = 5000
	MaxPageBytes = 64 << 20
)

// ErrNoPages is an archive with no page images in it.
var ErrNoPages = errors.New("comic: no pages in the archive")

var pageTypes = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png",
	".gif": "image/gif", ".webp": "image/webp",
}

// ContentType is a page's MIME type, from its name.
func ContentType(name string) string {
	return pageTypes[strings.ToLower(path.Ext(name))]
}

// isPage reports whether a zip entry is a page: an image, not a directory, not
// macOS's resource-fork shadow folder, not a hidden file.
func isPage(f *zip.File) bool {
	return isPageName(f.Name, f.FileInfo().IsDir())
}

// isPageName is isPage for any archive's entry, by name: a .cb7's entries are
// judged by the same rules as a .cbz's.
func isPageName(name string, dir bool) bool {
	if dir || ContentType(name) == "" {
		return false
	}
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(name, "__MACOSX/") || strings.Contains(name, "/__MACOSX/") {
		return false
	}
	return !strings.HasPrefix(path.Base(name), ".")
}

// Pages lists an archive's pages in reading order.
func Pages(zr *zip.Reader) ([]*zip.File, error) {
	var pages []*zip.File
	for _, f := range zr.File {
		if isPage(f) {
			pages = append(pages, f)
		}
	}
	if len(pages) == 0 {
		return nil, ErrNoPages
	}
	if len(pages) > MaxPages {
		return nil, fmt.Errorf("comic: %d pages is more than %d", len(pages), MaxPages)
	}
	sort.SliceStable(pages, func(i, j int) bool { return naturalLess(pages[i].Name, pages[j].Name) })
	return pages, nil
}

// Open opens a .cbz.
func Open(p string) (*zip.ReadCloser, error) {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return nil, fmt.Errorf("comic: %w", err)
	}
	return zr, nil
}

// ReadPage reads one page into memory, refusing one that claims, or turns out,
// to be bigger than MaxPageBytes.
func ReadPage(f *zip.File) ([]byte, error) {
	if f.UncompressedSize64 > MaxPageBytes {
		return nil, fmt.Errorf("comic: page %s is too large", f.Name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("comic: %w", err)
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, MaxPageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("comic: %w", err)
	}
	if len(b) > MaxPageBytes {
		return nil, fmt.Errorf("comic: page %s is too large", f.Name)
	}
	return b, nil
}

/*
 * naturalLess orders names the way people number pages: "page2" before
 * "page10", compared folder by folder, case-insensitively. Archives are
 * usually zero-padded, but enough are not that a plain sort would put page
 * 10 after page 1 in a real one.
 */
func naturalLess(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	for a != "" && b != "" {
		ra, rb := rune(a[0]), rune(b[0])
		if unicode.IsDigit(ra) && unicode.IsDigit(rb) {
			na, restA := digits(a)
			nb, restB := digits(b)
			ta, tb := strings.TrimLeft(na, "0"), strings.TrimLeft(nb, "0")
			if len(ta) != len(tb) {
				return len(ta) < len(tb)
			}
			if ta != tb {
				return ta < tb
			}
			a, b = restA, restB
			continue
		}
		if ra != rb {
			// A folder separator sorts before anything else, so a folder's
			// pages stay together ahead of a sibling with a longer name.
			if ra == '/' {
				return true
			}
			if rb == '/' {
				return false
			}
			return ra < rb
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func digits(s string) (string, string) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return s[:i], s[i:]
}
