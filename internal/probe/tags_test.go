package probe

import "testing"

// Every shape here was taken from a real file in the test library, not from a
// specification — taggers do not agree with each other and the specification
// does not bind them.
func TestParseTagsRealWorldShapes(t *testing.T) {
	t.Run("mp3 lowercase keys, fractional track, malformed disc", func(t *testing.T) {
		got := parseTags(map[string]string{
			"artist":       "Erika Eigen",
			"album":        "Stanley Kubrick's Clockwork Orange",
			"album_artist": "Various Artists",
			"title":        "I Want to Marry a Lighthouse Keeper",
			"track":        "11/15",
			"disc":         "/0",
			"genre":        "Soundtrack",
			"date":         "1989",
		})
		if got.Title != "I Want to Marry a Lighthouse Keeper" {
			t.Errorf("Title = %q", got.Title)
		}
		if got.AlbumArtist != "Various Artists" {
			t.Errorf("AlbumArtist = %q — the compilation case", got.AlbumArtist)
		}
		if got.Track != 11 {
			t.Errorf("Track = %d, want 11 from %q", got.Track, "11/15")
		}
		if got.Disc != 0 {
			t.Errorf("Disc = %d, want 0 — %q has no numerator", got.Disc, "/0")
		}
		if got.Year != 1989 {
			t.Errorf("Year = %d, want 1989", got.Year)
		}
	})

	t.Run("flac uppercase keys, spaced album artist, full date", func(t *testing.T) {
		got := parseTags(map[string]string{
			"TITLE":        "Mystery Fish",
			"ARTIST":       "Aesop Rock",
			"ALBUM ARTIST": "Aesop Rock",
			"album_artist": "Aesop Rock",
			"ALBUM":        "The Impossible Kid",
			"track":        "1",
			"GENRE":        "Hip-Hop",
			"DATE":         "2016-04-29",
		})
		if got.Title != "Mystery Fish" {
			t.Errorf("Title = %q — uppercase keys must resolve", got.Title)
		}
		if got.AlbumArtist != "Aesop Rock" {
			t.Errorf("AlbumArtist = %q", got.AlbumArtist)
		}
		if got.Track != 1 {
			t.Errorf("Track = %d, want 1 from a bare number", got.Track)
		}
		if got.Year != 2016 {
			t.Errorf("Year = %d, want 2016 from %q", got.Year, "2016-04-29")
		}
	})
}

// "ALBUM ARTIST", "album_artist" and "Album-Artist" are one field. A file
// carrying more than one spelling must not depend on map iteration order.
func TestParseTagsSeparatorsAndCaseAreOneField(t *testing.T) {
	for _, key := range []string{"ALBUM ARTIST", "album_artist", "Album-Artist", "AlbumArtist", "albumartist"} {
		got := parseTags(map[string]string{key: "Various Artists"})
		if got.AlbumArtist != "Various Artists" {
			t.Errorf("key %q did not resolve to AlbumArtist (got %q)", key, got.AlbumArtist)
		}
	}
}

func TestParseTagsIsDeterministicAcrossSpellings(t *testing.T) {
	in := map[string]string{
		"ALBUM ARTIST": "Aesop Rock",
		"album_artist": "Aesop Rock",
		"ARTIST":       "Aesop Rock",
	}
	first := parseTags(in)
	for i := 0; i < 50; i++ {
		if parseTags(in) != first {
			t.Fatal("parseTags returned different results for the same input")
		}
	}
}

func TestParseTagsEmptyAndJunk(t *testing.T) {
	if !parseTags(nil).Empty() {
		t.Error("nil tags should be Empty")
	}
	if !parseTags(map[string]string{}).Empty() {
		t.Error("no tags should be Empty")
	}
	// Whitespace-only values carry nothing.
	if !parseTags(map[string]string{"title": "   ", "artist": ""}).Empty() {
		t.Error("blank values should be Empty")
	}
	// A file with only a genre is still untagged for grouping purposes.
	if !parseTags(map[string]string{"genre": "Hip-Hop"}).Empty() {
		t.Error("a genre alone does not identify a track")
	}
	// But a title alone does.
	if parseTags(map[string]string{"title": "Xtal"}).Empty() {
		t.Error("a title makes a track identifiable")
	}
}

func TestLeadingNumber(t *testing.T) {
	cases := map[string]int{
		"11/15": 11, "1": 1, "07": 7, "/0": 0, "": 0,
		"  3 / 12 ": 3, "abc": 0, "-2": 0, "0/0": 0,
	}
	for in, want := range cases {
		if got := leadingNumber(in); got != want {
			t.Errorf("leadingNumber(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestLeadingYear(t *testing.T) {
	cases := map[string]int{
		"1989": 1989, "2016-04-29": 2016, "2016/04/29": 2016,
		"": 0, "89": 0, "abcd": 0, "0000": 0,
	}
	for in, want := range cases {
		if got := leadingYear(in); got != want {
			t.Errorf("leadingYear(%q) = %d, want %d", in, got, want)
		}
	}
}

// A lyrics tag runs to kilobytes. It must never reach the Tags struct.
func TestParseTagsIgnoresBulkFields(t *testing.T) {
	huge := make([]byte, 64<<10)
	for i := range huge {
		huge[i] = 'x'
	}
	got := parseTags(map[string]string{
		"title":   "Mystery Fish",
		"LYRICS":  string(huge),
		"COMMENT": string(huge),
	})
	if got.Title != "Mystery Fish" {
		t.Errorf("Title = %q", got.Title)
	}
	// Nothing in Tags is big enough to hold it; this asserts the shape rather
	// than the size, which is the point — only named fields are read.
	if got.Genre != "" || got.Artist != "" {
		t.Errorf("bulk tags leaked into %+v", got)
	}
}
