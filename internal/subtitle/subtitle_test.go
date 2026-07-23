package subtitle

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestClassifyCodec(t *testing.T) {
	text := []string{"subrip", "ass", "ssa", "webvtt", "mov_text", "SubRip"}
	for _, c := range text {
		if got := ClassifyCodec(c); got != Text {
			t.Errorf("ClassifyCodec(%q) = %q, want text", c, got)
		}
	}

	// The formats from the Event Horizon screenshot: images, not characters.
	bitmap := []string{"hdmv_pgs_subtitle", "pgssub", "dvd_subtitle", "dvdsub", "vobsub"}
	for _, c := range bitmap {
		if got := ClassifyCodec(c); got != Bitmap {
			t.Errorf("ClassifyCodec(%q) = %q, want bitmap", c, got)
		}
	}

	// Unknown must not be optimistically attempted: a conversion that fails
	// mid-playback is worse than an honest "unavailable".
	if got := ClassifyCodec("something_new"); got != Unknown {
		t.Errorf("ClassifyCodec of an unknown codec = %q, want unknown", got)
	}
}

func TestUnsupportedReasonIsActionable(t *testing.T) {
	reason := UnsupportedReason("hdmv_pgs_subtitle")
	if reason == "" {
		t.Fatal("no reason given for a bitmap track")
	}
	if !strings.Contains(reason, "search") {
		t.Errorf("reason %q does not point at the way forward", reason)
	}
	if UnsupportedReason("subrip") != "" {
		t.Error("a supported codec produced an unsupported reason")
	}
}

const sampleSRT = `1
00:00:01,000 --> 00:00:04,500
2040

2
00:00:05,250 --> 00:00:09,750
Deep space research vessel
'Event Horizon'

3
00:01:02,5 --> 00:01:04,75
Short millisecond fields
`

func TestSRTToVTT(t *testing.T) {
	out, err := SRTToVTT(strings.NewReader(sampleSRT))
	if err != nil {
		t.Fatalf("SRTToVTT: %v", err)
	}
	got := string(out)

	if !strings.HasPrefix(got, "WEBVTT\n") {
		t.Fatalf("output does not start with the WebVTT signature:\n%s", got)
	}

	// The comma is the whole game: a browser silently drops a track whose
	// timings use SRT's comma separator.
	if strings.Contains(got, ",") && strings.Contains(got, "-->") {
		for _, line := range strings.Split(got, "\n") {
			if strings.Contains(line, "-->") && strings.Contains(line, ",") {
				t.Errorf("timing line kept an SRT comma: %q", line)
			}
		}
	}
	if !strings.Contains(got, "00:00:01.000 --> 00:00:04.500") {
		t.Errorf("expected timing not found:\n%s", got)
	}

	// "5" means 500 milliseconds, not 5. Getting this wrong shifts every cue.
	if !strings.Contains(got, "00:01:02.500 --> 00:01:04.750") {
		t.Errorf("short millisecond fields were not padded:\n%s", got)
	}

	// Multi-line cue text must survive.
	if !strings.Contains(got, "Deep space research vessel\n'Event Horizon'") {
		t.Errorf("multi-line cue was mangled:\n%s", got)
	}

	// Cue counters are SRT bookkeeping and add nothing to WebVTT.
	if strings.Contains(got, "\n1\n00:00:01") {
		t.Error("SRT cue number leaked into the output")
	}
}

func TestSRTToVTTHandlesCRLFAndBOM(t *testing.T) {
	body := "\ufeff1\r\n00:00:01,000 --> 00:00:02,000\r\nHello\r\n\r\n"
	out, err := SRTToVTT(strings.NewReader(body))
	if err != nil {
		t.Fatalf("SRTToVTT: %v", err)
	}
	got := string(out)
	if !strings.HasPrefix(got, "WEBVTT") {
		t.Errorf("BOM broke the header:\n%q", got)
	}
	if strings.Contains(got, "\r") {
		t.Error("carriage returns survived")
	}
	if !strings.Contains(got, "Hello") {
		t.Errorf("cue text lost:\n%s", got)
	}
}

func TestSRTToVTTRejectsNonSubtitle(t *testing.T) {
	if _, err := SRTToVTT(strings.NewReader("this is just prose\nwith no timings")); err == nil {
		t.Error("SRTToVTT accepted a file with no cues")
	}
}

// Files named .vtt are frequently SRT with the extension changed. A browser
// rejects the whole track without the header and says nothing.
func TestEnsureVTTHeader(t *testing.T) {
	proper := []byte("WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHi\n")
	if got := EnsureVTTHeader(proper); string(got) != string(proper) {
		t.Error("a valid WebVTT file was modified")
	}

	srtInDisguise := []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n")
	got := string(EnsureVTTHeader(srtInDisguise))
	if !strings.HasPrefix(got, "WEBVTT") {
		t.Errorf("no header added:\n%s", got)
	}
	if strings.Contains(got, "00:00:01,000") {
		t.Errorf("SRT timings were not converted:\n%s", got)
	}

	headerless := []byte("00:00:01.000 --> 00:00:02.000\nHi\n")
	if !strings.HasPrefix(string(EnsureVTTHeader(headerless)), "WEBVTT") {
		t.Error("header not added to headerless WebVTT")
	}
}

func TestLooksLikeText(t *testing.T) {
	if !LooksLikeText([]byte("1\n00:00:01,000 --> ")) {
		t.Error("plain text rejected")
	}
	if LooksLikeText([]byte{0x00, 0x01, 0x02}) {
		t.Error("binary accepted as text")
	}
	if LooksLikeText(nil) {
		t.Error("empty input accepted")
	}
}

func TestFindSidecars(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Event Horizon (1997).mkv")
	os.WriteFile(video, []byte("x"), 0o644)

	files := []string{
		"Event Horizon (1997).srt",
		"Event Horizon (1997).en.srt",
		"Event Horizon (1997).fr.forced.srt",
		"Event Horizon (1997).es.ass",
		"Some Other Film.srt", // must not be picked up
		"notes.txt",           // not a subtitle
	}
	for _, f := range files {
		os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644)
	}

	got := FindSidecars(video)
	if len(got) != 4 {
		names := []string{}
		for _, s := range got {
			names = append(names, filepath.Base(s.Path))
		}
		sort.Strings(names)
		t.Fatalf("found %d sidecars (%v), want 4", len(got), names)
	}

	byLang := map[string]Sidecar{}
	for _, s := range got {
		byLang[s.Language+"/"+s.Format] = s
	}
	if _, ok := byLang["en/srt"]; !ok {
		t.Errorf("English srt not detected: %+v", got)
	}
	if s, ok := byLang["fr/srt"]; !ok || !s.Forced {
		t.Errorf("forced French track not detected correctly: %+v", s)
	}
	if _, ok := byLang["es/ass"]; !ok {
		t.Errorf("Spanish ass not detected: %+v", got)
	}

	for _, s := range got {
		if strings.Contains(s.Path, "Some Other Film") {
			t.Error("a subtitle for a different film was claimed")
		}
	}
}

// Ripping tools commonly drop subtitles in a Subs/ directory, where the
// filename is the language rather than the film.
func TestFindSidecarsInSubsDirectory(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Film.mkv")
	os.WriteFile(video, []byte("x"), 0o644)

	subs := filepath.Join(dir, "Subs")
	os.MkdirAll(subs, 0o755)
	os.WriteFile(filepath.Join(subs, "English.srt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(subs, "French.forced.srt"), []byte("x"), 0o644)

	got := FindSidecars(video)
	if len(got) != 2 {
		t.Fatalf("found %d in Subs/, want 2: %+v", len(got), got)
	}

	var sawForced bool
	for _, s := range got {
		if s.Forced {
			sawForced = true
		}
		if s.Language == "" {
			t.Errorf("language not derived from %q", filepath.Base(s.Path))
		}
	}
	if !sawForced {
		t.Error("forced flag not detected in Subs/")
	}
}

func TestFindSidecarsNone(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Lonely.mkv")
	os.WriteFile(video, []byte("x"), 0o644)

	if got := FindSidecars(video); len(got) != 0 {
		t.Errorf("found %d sidecars where there are none: %+v", len(got), got)
	}
}

func TestLanguageNormalization(t *testing.T) {
	for _, in := range []string{"eng", "en", "English", "ENGLISH"} {
		if got := NormalizeLanguage(in); got != "en" {
			t.Errorf("NormalizeLanguage(%q) = %q, want en", in, got)
		}
	}
	if got := NormalizeLanguage("klingon"); got != "klingon" {
		t.Errorf("an unknown language was mangled: %q", got)
	}
	if got := DisplayLanguage("fre"); got != "French" {
		t.Errorf("DisplayLanguage(fre) = %q", got)
	}
	if got := DisplayLanguage(""); got != "Unknown" {
		t.Errorf("DisplayLanguage(\"\") = %q", got)
	}
}

func TestIsSidecar(t *testing.T) {
	yes := []string{"a.srt", "b.VTT", "c.ass", "d.ssa", "e.sub"}
	no := []string{"a.mkv", "b.txt", "c.nfo", "d"}
	for _, p := range yes {
		if !IsSidecar(p) {
			t.Errorf("IsSidecar(%q) = false", p)
		}
	}
	for _, p := range no {
		if IsSidecar(p) {
			t.Errorf("IsSidecar(%q) = true", p)
		}
	}
}
