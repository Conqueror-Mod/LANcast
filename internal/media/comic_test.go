package media

import (
	"path/filepath"
	"testing"
)

func TestComicTitleDropsTheTagsAndKeepsTheYear(t *testing.T) {
	for _, tc := range []struct {
		name  string
		title string
		year  int
	}{
		{"Saga 012 (2014) (Digital) (Group).cbz", "Saga 012", 2014},
		{"Saga 012 (Digital) (2014).cbz", "Saga 012", 2014},
		{"Bone_v01_[2005].cbz", "Bone v01", 2005},
		{"Watchmen.cbz", "Watchmen", 0},
		// A bracket that is the whole name is not a tag to strip.
		{"(untitled).cbz", "(untitled)", 0},
		// A number in the middle is part of the title.
		{"2000 AD 1999.cbz", "2000 AD 1999", 0},
	} {
		title, year := ComicTitle(filepath.Join("lib", tc.name))
		if title != tc.title || year != tc.year {
			t.Errorf("%s: got %q %d, want %q %d", tc.name, title, year, tc.title, tc.year)
		}
	}
}

func TestCBZAndPDFAreComics(t *testing.T) {
	for name, want := range map[string]bool{
		"a.cbz": true, "a.CBZ": true, "a.pdf": true, "a.PDF": true, "a.cbr": false, "a.zip": false,
	} {
		if IsComic(name) != want {
			t.Errorf("%s: %v", name, !want)
		}
	}
}

// The comics a library cannot read are named, so a scan can count them; a
// comic it reads, and a file that is no comic at all, are not.
func TestUnreadComicFormats(t *testing.T) {
	for name, want := range map[string]bool{
		"a.cbr": true, "a.CB7": true, "a.epub": true, "a.cbz": false, "a.pdf": false, "a.txt": false, "a.jpg": false,
	} {
		if IsUnreadComic(name) != want {
			t.Errorf("%s: %v", name, !want)
		}
	}
}

func TestComicSeriesIsTheFolderBelowTheRoot(t *testing.T) {
	root := filepath.Join("C:", "Comics")
	if s := ComicSeries(root, filepath.Join(root, "Saga", "Saga 001.cbz")); s != "Saga" {
		t.Errorf("in a folder: %q", s)
	}
	if s := ComicSeries(root, filepath.Join(root, "Watchmen.cbz")); s != "" {
		t.Errorf("at the root: %q", s)
	}
}

func TestAComicsLibraryParsesComics(t *testing.T) {
	root := filepath.Join("C:", "Comics")
	p := filepath.Join(root, "Saga", "Saga 001 (2012).cbz")
	if !IsScannable(p, LibraryComics) || IsScannable(filepath.Join(root, "x.mkv"), LibraryComics) {
		t.Fatal("a comics library takes .cbz and .pdf, and not video")
	}
	info := Parse(root, p, LibraryComics)
	if info.Kind != KindComic || info.Title != "Saga 001" || info.Year != 2012 || info.Series != "Saga" {
		t.Fatalf("%+v", info)
	}
}
