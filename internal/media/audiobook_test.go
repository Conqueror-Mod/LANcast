package media

import (
	"path/filepath"
	"testing"
)

// The test library's own layout and tags, as read from its files on
// 2026-11-03 (ADR 0116): folder names carry the title, series, number and
// abridgement; the tags carry almost nothing, and what they carry is often
// wrong.
func testBooks(t *testing.T) (string, map[string]Book) {
	t.Helper()
	root := filepath.FromSlash("/lib/TEST AUDIOBOOK LIBRARY")
	p := func(s string) string { return filepath.Join(root, filepath.FromSlash(s)) }
	files := []BookFile{
		{Path: p("Alien Voices/01 - Spock vs Q.mp3"),
			Tags: BookTags{Title: "Spock vs. Q", Artist: "Alien Voices", Album: "performed live", Year: 1999}},
		{Path: p("Alien Voices/02 - Spock vs Q - The Sequel.mp3"),
			Tags: BookTags{Title: "Spock vs. Q: The Sequel", Artist: "Alien Voices", Album: "performed live", Year: 2000}},
		{Path: p("Captain Sulu Adventures/02 - Cacophony - A Captain Sulu Adventure/TOS - Captain Sulu Adventure II - Cacophony.mp3"),
			Tags: BookTags{Title: "Cacophony", Artist: "Star Trek"}},
		// Track tags out of order with the filenames, to prove they win.
		{Path: p("Deep Space Nine/01 - Emissary (Abridged Novelization)/Emissary 2of4.mp3"),
			Tags: BookTags{Title: "Emissary 2of4", Artist: "Star Trek", Track: 2}},
		{Path: p("Deep Space Nine/01 - Emissary (Abridged Novelization)/Emissary 1of4.mp3"),
			Tags: BookTags{Title: "Emissary 1of4", Artist: "Star Trek", Track: 1}},
		{Path: p("Deep Space Nine/02 - Fallen Heroes/STDS9 - Fallen Heroes disc 10.mp3"),
			Tags: BookTags{Title: "DS9 - Fallen Heroes", Artist: "Star Trek", Album: "Star Trek"}},
		{Path: p("Deep Space Nine/02 - Fallen Heroes/STDS9 - Fallen Heroes disc 2.mp3"),
			Tags: BookTags{Title: "DS9 - Fallen Heroes", Artist: "Star Trek", Album: "Star Trek"}},
		{Path: p("Deep Space Nine/06 - Millennium, Book 1 - The Fall of Terok Nor - Abridged/Fall of Terok Nor 1of4.mp3"),
			Tags: BookTags{Title: "Fallen of Terok Nor 1of4", Artist: "Star Trek", Track: 1}},
		{Path: p("Deep Space Nine/07 - Objective Bajor - Abridged/Peel, John - Objective Bajor - 1of7.mp3"),
			Tags: BookTags{Title: "Objective Bajor", Artist: "Star Trek", AlbumArtist: "Peel, John", Track: 1}},
		{Path: p("Gateways/Gateways - What Lay Beyond - Abridged/Gateways_Bk7_What Lay Beyond (1 of 4).mp3"),
			Tags: BookTags{Title: "Gateways Bk7 What Lay Beyond", Artist: "Star Trek", Track: 1}},
	}
	got := map[string]Book{}
	for _, b := range GroupBooks(root, files) {
		got[b.Title] = b
	}
	return root, got
}

func TestFolderOfPartsIsOneBook(t *testing.T) {
	_, books := testBooks(t)
	b, ok := books["Emissary"]
	if !ok {
		t.Fatalf("no book titled Emissary; got %v", keys(books))
	}
	if b.Series != "Deep Space Nine" || b.Number != 1 || b.Edition != "Abridged" {
		t.Fatalf("series/number/edition = %q/%d/%q, want Deep Space Nine/1/Abridged",
			b.Series, b.Number, b.Edition)
	}
	if len(b.Parts) != 2 || filepath.Base(b.Parts[0].Path) != "Emissary 1of4.mp3" {
		t.Fatalf("parts not in track order: %+v", b.Parts)
	}
	// "Emissary 1of4" and "Emissary 2of4" both say the book's name.
	if b.Parts[0].Title != "Part 1" || b.Parts[1].Title != "Part 2" {
		t.Fatalf("part titles = %q, %q, want Part 1, Part 2", b.Parts[0].Title, b.Parts[1].Title)
	}
	// The author falls back to the artist tag, as agreed.
	if b.Author != "Star Trek" {
		t.Fatalf("author = %q, want the artist tag", b.Author)
	}
}

func TestUntrackedPartsSortAsAPersonReads(t *testing.T) {
	_, books := testBooks(t)
	b := books["Fallen Heroes"]
	if len(b.Parts) != 2 || filepath.Base(b.Parts[0].Path) != "STDS9 - Fallen Heroes disc 2.mp3" {
		t.Fatalf("disc 2 must come before disc 10: %+v", b.Parts)
	}
	if b.Edition != "" {
		t.Fatalf("an unmarked book is not abridged: %q", b.Edition)
	}
}

func TestAlbumArtistIsTheAuthorAndIsTurnedRound(t *testing.T) {
	_, books := testBooks(t)
	if a := books["Objective Bajor"].Author; a != "John Peel" {
		t.Fatalf("author = %q, want John Peel", a)
	}
}

func TestSeriesPrefixAndBookNumberFromTheFilename(t *testing.T) {
	_, books := testBooks(t)
	b, ok := books["What Lay Beyond"]
	if !ok {
		t.Fatalf("no book titled What Lay Beyond; got %v", keys(books))
	}
	if b.Series != "Gateways" || b.Number != 7 || b.Edition != "Abridged" {
		t.Fatalf("series/number/edition = %q/%d/%q, want Gateways/7/Abridged",
			b.Series, b.Number, b.Edition)
	}
}

// The leading number is the book's place in Deep Space Nine; "Book 1" is the
// Millennium sub-series, which stays in the title for now (ADR 0116 answer 4).
func TestLeadingNumberBeatsASubSeriesNumber(t *testing.T) {
	_, books := testBooks(t)
	b, ok := books["Millennium, Book 1 - The Fall of Terok Nor"]
	if !ok {
		t.Fatalf("got %v", keys(books))
	}
	if b.Number != 6 {
		t.Fatalf("number = %d, want 6", b.Number)
	}
}

// A leaf folder is one book even when its files are separate recordings:
// splitting by title would tear apart every rip named after its chapters. The
// parts keep their own titles, because those differ.
func TestLeafFolderOfDistinctTitlesIsStillOneBook(t *testing.T) {
	_, books := testBooks(t)
	b, ok := books["Alien Voices"]
	if !ok {
		t.Fatalf("got %v", keys(books))
	}
	if len(b.Parts) != 2 || b.Parts[0].Title != "Spock vs. Q" ||
		b.Parts[1].Title != "Spock vs. Q: The Sequel" {
		t.Fatalf("parts = %+v", b.Parts)
	}
	if b.Series != "" || b.Year != 1999 {
		t.Fatalf("series/year = %q/%d, want none/1999", b.Series, b.Year)
	}
}

func TestOneFileFolderTakesTheFolderName(t *testing.T) {
	_, books := testBooks(t)
	b, ok := books["Cacophony - A Captain Sulu Adventure"]
	if !ok {
		t.Fatalf("got %v", keys(books))
	}
	if b.Number != 2 || b.Series != "Captain Sulu Adventures" || len(b.Parts) != 1 {
		t.Fatalf("book = %+v", b)
	}
	if b.Parts[0].Title != b.Title {
		t.Fatalf("a one-part book's part is the book: %q", b.Parts[0].Title)
	}
}

// Files beside folders of books are books of their own, and so is a file at
// the root.
func TestLooseFilesAreTheirOwnBooks(t *testing.T) {
	root := filepath.FromSlash("/lib")
	p := func(s string) string { return filepath.Join(root, filepath.FromSlash(s)) }
	books := GroupBooks(root, []BookFile{
		{Path: p("Standalone.m4b"), Tags: BookTags{Title: "Standalone", AlbumArtist: "A. Writer"}},
		{Path: p("Series/03 - Third (Unabridged).mp3")},
		{Path: p("Series/04 - Fourth/part1.mp3")},
	})
	got := map[string]Book{}
	for _, b := range books {
		got[b.Title] = b
	}
	if b := got["Standalone"]; b.Series != "" || b.Author != "A. Writer" || b.Key == p("Standalone.m4b") {
		t.Fatalf("root book = %+v; its key must not be the file's own path", b)
	}
	if b := got["Third"]; b.Series != "Series" || b.Number != 3 || b.Edition != "" || b.Dir != "" {
		t.Fatalf("loose book = %+v", b)
	}
	if b := got["Fourth"]; b.Number != 4 || b.Dir != p("Series/04 - Fourth") {
		t.Fatalf("folder book = %+v", b)
	}
	if len(books) != 3 {
		t.Fatalf("books = %d, want 3", len(books))
	}
}

func TestPersonNameLeavesBandsAlone(t *testing.T) {
	for in, want := range map[string]string{
		"Peel, John":           "John Peel",
		"Earth, Wind & Fire":   "Earth, Wind & Fire",
		"Crosby, Stills, Nash": "Crosby, Stills, Nash",
		"Ursula K. Le Guin":    "Ursula K. Le Guin",
	} {
		if got := personName(in); got != want {
			t.Errorf("personName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBookAudioTakesM4BOnlyInAudiobooks(t *testing.T) {
	if !IsScannable("x.m4b", LibraryAudiobooks) || !IsScannable("x.mp3", LibraryAudiobooks) {
		t.Fatal("an audiobooks library takes .m4b and .mp3")
	}
	if IsScannable("x.m4b", LibraryMusic) {
		t.Fatal("a music library must not take .m4b")
	}
	if IsScannable("x.mkv", LibraryAudiobooks) {
		t.Fatal("an audiobooks library takes no video")
	}
}

func keys(m map[string]Book) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
