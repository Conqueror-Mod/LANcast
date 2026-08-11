package playlist

import (
	"path/filepath"
	"strings"
	"testing"
)

// One named test per dialect, asserting the result and not just that it parsed
// — the model decide_test.go sets. Every case here is a real shape a playlist
// arrives in, not a hypothetical.

func TestPlainM3U(t *testing.T) {
	in := "track one.mp3\ntrack two.mp3\n"
	got, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(got), got)
	}
	if got[0].Path != "track one.mp3" || got[1].Path != "track two.mp3" {
		t.Errorf("paths = %q, %q", got[0].Path, got[1].Path)
	}
	if got[0].Title != "" {
		t.Errorf("a plain M3U has no titles, got %q", got[0].Title)
	}
}

func TestExtendedM3U(t *testing.T) {
	in := "#EXTM3U\n#EXTINF:213,Bloodhound Gang - Fire Water Burn\nfwb.mp3\n"
	got, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if got[0].Seconds != 213 {
		t.Errorf("Seconds = %d, want 213", got[0].Seconds)
	}
	if got[0].Title != "Bloodhound Gang - Fire Water Burn" {
		t.Errorf("Title = %q", got[0].Title)
	}
}

// A title containing a comma is ordinary, and splitting on every comma would
// truncate it at the first one.
func TestTitleKeepsItsCommas(t *testing.T) {
	in := "#EXTINF:100,Earth, Wind & Fire - September\nsept.mp3\n"
	got, _ := Parse(strings.NewReader(in))
	if got[0].Title != "Earth, Wind & Fire - September" {
		t.Errorf("Title = %q", got[0].Title)
	}
}

// Real files carry a duration with no comma at all.
func TestExtinfWithoutTitle(t *testing.T) {
	in := "#EXTINF:97\nx.mp3\n"
	got, _ := Parse(strings.NewReader(in))
	if got[0].Seconds != 97 || got[0].Title != "" {
		t.Errorf("got %+v", got[0])
	}
}

// -1 is the convention for a stream of unknown length and must not be read as
// a parse failure that silently becomes zero.
func TestUnknownDuration(t *testing.T) {
	in := "#EXTINF:-1,Some Radio\nx.mp3\n"
	got, _ := Parse(strings.NewReader(in))
	if got[0].Seconds != -1 {
		t.Errorf("Seconds = %d, want -1", got[0].Seconds)
	}
}

// A malformed EXTINF must cost its own metadata and nothing else. Refusing the
// file would throw away every other entry over one bad line.
func TestMalformedExtinfDoesNotLoseTheEntry(t *testing.T) {
	in := "#EXTINF:not-a-number,Title Here\nsong.mp3\nplain.mp3\n"
	got, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Path != "song.mp3" || got[0].Seconds != 0 {
		t.Errorf("got %+v", got[0])
	}
	if got[0].Title != "Title Here" {
		t.Errorf("the title survives a bad duration, got %q", got[0].Title)
	}
}

func TestBOMAndBlankLinesAndComments(t *testing.T) {
	in := "\ufeff#EXTM3U\n\n# a comment\n\na.mp3\n\n"
	got, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "a.mp3" {
		t.Fatalf("got %+v", got)
	}
}

// The whole reason ErrHLS exists: LANcast writes these itself, and importing
// one produces a playlist of transcode fragments.
func TestHLSPlaylistIsRefused(t *testing.T) {
	in := "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:6\nseg0.m4s\n"
	if _, err := Parse(strings.NewReader(in)); err != ErrHLS {
		t.Fatalf("err = %v, want ErrHLS", err)
	}
}

// The tag can appear after ordinary-looking lines; the check must not be
// limited to the head of the file.
func TestHLSDetectedLate(t *testing.T) {
	in := "#EXTM3U\nseg0.m4s\n#EXT-X-ENDLIST\n"
	if _, err := Parse(strings.NewReader(in)); err != ErrHLS {
		t.Fatalf("err = %v, want ErrHLS", err)
	}
}

// ---- Resolve -------------------------------------------------------------

func TestResolveRelative(t *testing.T) {
	got, ok := Resolve(filepath.FromSlash("/music/album"), "track.mp3")
	if !ok {
		t.Fatal("not ok")
	}
	want := filepath.Clean(filepath.FromSlash("/music/album/track.mp3"))
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A playlist written on Windows, read wherever. Without the rewrite this is one
// filename containing backslashes on Linux, which resolves to nothing and looks
// like a missing file rather than a path that was never understood.
func TestResolveWindowsSeparators(t *testing.T) {
	got, ok := Resolve(filepath.FromSlash("/music"), `Album\Disc 1\03 song.mp3`)
	if !ok {
		t.Fatal("not ok")
	}
	want := filepath.Clean(filepath.FromSlash("/music/Album/Disc 1/03 song.mp3"))
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// An absolute path from another machine must stay absolute, so it is reported
// as missing rather than joined onto the playlist's directory to produce a path
// that never existed anywhere.
func TestResolveKeepsForeignAbsolutePath(t *testing.T) {
	got, ok := Resolve(filepath.FromSlash("/music"), `D:\Media\song.mp3`)
	if !ok {
		t.Fatal("not ok")
	}
	if strings.Contains(filepath.ToSlash(got), "/music/") {
		t.Errorf("a drive-letter path was joined onto the base: %q", got)
	}
}

func TestResolveRejectsURLs(t *testing.T) {
	for _, u := range []string{
		"http://example.com/stream.mp3",
		"https://example.com/a.mp3",
		"rtsp://box/live",
	} {
		if _, ok := Resolve("/music", u); ok {
			t.Errorf("%q resolved as a local file", u)
		}
	}
}

func TestResolveRejectsEmpty(t *testing.T) {
	if _, ok := Resolve("/music", "   "); ok {
		t.Error("whitespace resolved as a path")
	}
}

// A filename containing "://" is not a URL, and the scheme test must not be so
// eager that it drops one.
func TestResolveKeepsOddFilenames(t *testing.T) {
	if _, ok := Resolve("/music", "a.b://c.mp3"); !ok {
		t.Error("a dotted name before :// was treated as a scheme")
	}
}
