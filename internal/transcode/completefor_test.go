package transcode

import (
	"math"
	"testing"

	"lancast/internal/probe"
)

func encodeFilm() Options {
	return Options{
		Duration: 7553.568,
		StartAt:  1323,
		Decision: probe.Decision{
			Method: probe.Transcode, VideoAction: "encode", AudioAction: "encode",
			SourceFrameRate: 23.976,
		},
	}
}

func TestAnEncodedFilmIsListedWhole(t *testing.T) {
	media, seg, ok := completeFor(encodeFilm())
	if !ok {
		t.Fatal("an encoded film with a known length kept the growing playlist WebView2 cannot play")
	}
	// What is left after the start offset, not the whole film: the encode
	// begins at -ss, and its first segment is the offset.
	if math.Abs(media-6230.568) > 0.001 {
		t.Errorf("media = %.3f, want the 6230.568s left after starting at 1323s", media)
	}
	if math.Abs(seg-6.006) > 0.001 {
		t.Errorf("segment = %.4f, want 6.006", seg)
	}
}

// The cuts in a copied video track fall on the source's own keyframes, which
// nothing here has read, so it cannot be listed in advance.
func TestACopiedVideoTrackKeepsTheGrowingPlaylist(t *testing.T) {
	o := encodeFilm()
	o.Decision.VideoAction = "copy"
	if _, _, ok := completeFor(o); ok {
		t.Error("a copied video track was listed whole from keyframes nobody placed")
	}
}

func TestWhatCannotBeListedWholeIsNot(t *testing.T) {
	cases := map[string]func(*Options){
		"unknown length":   func(o *Options) { o.Duration = 0 },
		"started past end": func(o *Options) { o.StartAt = o.Duration },
		"audio only":       func(o *Options) { o.Decision.AudioOnly = true },
		"a live channel":   func(o *Options) { o.Live = true },
	}
	for name, change := range cases {
		o := encodeFilm()
		change(&o)
		if _, _, ok := completeFor(o); ok {
			t.Errorf("%s: listed whole", name)
		}
	}
}
