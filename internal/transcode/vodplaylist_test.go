package transcode

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// The length ffmpeg actually wrote, on a real 4K encode at 23.976fps: 49 of 49
// segments at 6.006000. The estimate is checked against that measurement, not
// against its own arithmetic.
func TestSegmentLengthMatchesWhatFFmpegWroteAtFilmRate(t *testing.T) {
	if got := SegmentLength(24000.0 / 1001.0); math.Abs(got-6.006) > 0.001 {
		t.Errorf("SegmentLength(23.976) = %.4f, want 6.006 as measured", got)
	}
}

func TestSegmentLengthAtCommonRates(t *testing.T) {
	cases := []struct {
		fps  float64
		want float64
	}{
		{24, 6},
		{25, 6},
		{30000.0 / 1001.0, 6.006},
		{50, 6},
		{60, 6},
	}
	for _, c := range cases {
		if got := SegmentLength(c.fps); math.Abs(got-c.want) > 0.001 {
			t.Errorf("SegmentLength(%v) = %.4f, want %.3f", c.fps, got, c.want)
		}
	}
}

// An unknown rate is not a guess dressed as a measurement: it falls back to the
// target ffmpeg is told to aim for.
func TestSegmentLengthWithoutAFrameRate(t *testing.T) {
	for _, fps := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if got := SegmentLength(fps); got != SegmentSeconds {
			t.Errorf("SegmentLength(%v) = %v, want %d", fps, got, SegmentSeconds)
		}
	}
}

func TestACompletePlaylistIsFinishedAndSaysSo(t *testing.T) {
	p := CompletePlaylist(60, 6, "/api/stream/7/hls/abc/")
	for _, want := range []string{
		"#EXT-X-PLAYLIST-TYPE:VOD",
		"#EXT-X-ENDLIST",
		`#EXT-X-MAP:URI="/api/stream/7/hls/abc/init.mp4"`,
		"/api/stream/7/hls/abc/seg00000.m4s",
		"/api/stream/7/hls/abc/seg00009.m4s",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("playlist lacks %q:\n%s", want, p)
		}
	}
	// A growing playlist is exactly what WebView2 cannot play.
	if strings.Contains(p, "EVENT") {
		t.Error("a complete playlist must not call itself EVENT")
	}
	if strings.Contains(p, "seg00010") {
		t.Error("listed a segment past the end of 60s of media")
	}
}

// The film's real length is what the element shows and where `ended` fires, so
// the listed durations must add up to it however the last segment falls.
func TestACompletePlaylistAddsUpToTheMedia(t *testing.T) {
	for _, total := range []float64{6230.568, 61.2, 63.5, 2, 6.006} {
		p := CompletePlaylist(total, 6.006, "")
		sum, n := 0.0, 0
		for _, line := range strings.Split(p, "\n") {
			if rest, ok := strings.CutPrefix(line, "#EXTINF:"); ok {
				d, err := strconv.ParseFloat(strings.TrimSuffix(rest, ","), 64)
				if err != nil {
					t.Fatalf("unparseable EXTINF %q", line)
				}
				sum += d
				n++
			}
		}
		if math.Abs(sum-total) > 0.001 {
			t.Errorf("total %.3f: durations sum to %.3f over %d segments", total, sum, n)
		}
	}
}

/*
 * The last segment is where an estimate that is not frame-exact would hurt.
 * Listing a sliver ffmpeg never writes fails the film in its last second; a
 * slightly long last segment costs nothing.
 */
func TestASliverAtTheEndIsFoldedIntoTheLastSegment(t *testing.T) {
	p := CompletePlaylist(60.4, 6, "")
	if strings.Contains(p, "seg00010") {
		t.Errorf("0.4s left over was listed as its own segment:\n%s", p)
	}
	if !strings.Contains(p, "#EXT-X-TARGETDURATION:7") {
		t.Errorf("target duration must cover the lengthened last segment:\n%s", p)
	}
}

func TestListedMatchesWholeNamesOnly(t *testing.T) {
	playlist := "#EXTM3U\n#EXTINF:6.006000,\nseg00000.m4s\n#EXTINF:6.006000,\nseg00001.m4s\n"
	if !Listed(playlist, "seg00001.m4s") {
		t.Error("a listed segment was not found")
	}
	if Listed(playlist, "seg00002.m4s") {
		t.Error("a segment ffmpeg has not closed was reported finished")
	}
	if Listed(playlist, "seg0000") {
		t.Error("a fragment of a name matched")
	}
}
