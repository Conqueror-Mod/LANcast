package optimize

import (
	"path/filepath"
	"testing"

	"lancast/internal/probe"
)

func hevc4k() *probe.Result {
	return &probe.Result{Container: "matroska", Streams: []probe.Stream{
		{Index: 0, Kind: probe.KindVideo, Codec: "hevc", Width: 3840, Height: 2160},
		{Index: 1, Kind: probe.KindAudio, Codec: "truehd", Channels: 8},
	}}
}

func copyOf() *probe.Result {
	// "mov", as ParseJSON reports a real MP4 — not ffprobe's raw list.
	return &probe.Result{Container: "mov", BitRate: 7_500_000, Streams: []probe.Stream{
		{Index: 0, Kind: probe.KindVideo, Codec: "h264", Width: 1920, Height: 1080},
		{Index: 1, Kind: probe.KindAudio, Codec: "aac", Channels: 2},
	}}
}

// The copy is the universal device's decision: a 4K HEVC TrueHD file must be
// converted for it, and the copy must not be.
func TestTheProfileConvertsWhatItShould(t *testing.T) {
	if d := probe.Decide(hevc4k(), Profile); d.Method != probe.Transcode {
		t.Errorf("a 4K HEVC original for the optimized profile = %v, want transcode", d.Method)
	}
	if d := probe.Decide(copyOf(), Profile); d.Method != probe.DirectPlay {
		t.Errorf("the copy for the optimized profile = %v (%s), want direct play", d.Method, d.Reason)
	}
}

func TestPreferOnlyWhenItTurnsAConversionIntoDirectPlay(t *testing.T) {
	browser := probe.BrowserProfile()
	orig := probe.Decide(hevc4k(), browser)
	if orig.Method == probe.DirectPlay {
		t.Fatal("fixture: the browser plays 4K HEVC TrueHD directly?")
	}
	d, ok := Prefer(orig, copyOf(), browser, -1)
	if !ok || d.Method != probe.DirectPlay {
		t.Errorf("Prefer for a browser = %v, %v; want the copy, direct", d.Method, ok)
	}
	// A device that plays the original directly keeps it.
	direct := probe.Decision{Method: probe.DirectPlay}
	if _, ok := Prefer(direct, copyOf(), browser, -1); ok {
		t.Error("preferred the copy over an original that plays directly")
	}
	// A chosen audio track keeps the original: the copy has one.
	if _, ok := Prefer(orig, copyOf(), browser, 3); ok {
		t.Error("preferred the copy when a particular audio track was asked for")
	}
	// No copy, no change.
	if _, ok := Prefer(orig, nil, browser, -1); ok {
		t.Error("preferred a copy that does not exist")
	}
}

func TestEstimate(t *testing.T) {
	// Two hours at 8 Mbit/s video plus 192 kbit/s audio ≈ 7.4 GB.
	if got := EstimateBytes(2 * 3600 * 1000); got < 7_000_000_000 || got > 7_500_000_000 {
		t.Errorf("two-hour estimate = %d", got)
	}
	if EstimateBytes(0) != 0 {
		t.Error("an unknown length estimated something")
	}
}

func TestTheFolderMayNotBeInsideALibrary(t *testing.T) {
	root := t.TempDir()
	roots := []string{root}
	if !Inside(root, roots) || !Inside(filepath.Join(root, "optimized"), roots) {
		t.Error("a library location, or a folder inside one, was allowed")
	}
	if Inside(t.TempDir(), roots) {
		t.Error("an unrelated folder was refused")
	}
	// A sibling that merely shares a prefix is not inside.
	if Inside(root+"-optimized", roots) {
		t.Error("a sibling sharing the name prefix was refused")
	}
}
