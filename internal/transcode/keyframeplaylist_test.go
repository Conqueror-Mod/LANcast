package transcode

import (
	"reflect"
	"strings"
	"testing"

	"lancast/internal/probe"
)

func TestKeyframeCutsAtTheFirstKeyframePastEachBoundary(t *testing.T) {
	keys := []float64{0, 2.002, 4.004, 6.006, 8.008, 10.01, 12.012, 14.014}
	got := KeyframeCuts(keys, 0, 6)
	if want := []float64{0, 6.006, 12.012}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestKeyframeCutsCountBoundariesFromTheStartNotTheLastCut(t *testing.T) {
	// 7s past the start cuts the first segment; the next boundary is 12s from
	// the start, not 13s, so 12.5 cuts and 12.9 would not have mattered.
	keys := []float64{0, 7, 12.5, 13, 19}
	got := KeyframeCuts(keys, 0, 6)
	if want := []float64{0, 7, 12.5, 19}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestALongGapMakesOneLongSegmentAndTheNextKeyframeCutsAtOnce(t *testing.T) {
	// From 1 to 14 passes two boundaries (6 and 12). One cut at 14; the next
	// boundary is then 12, already passed, so 15 cuts too.
	keys := []float64{0, 1, 14, 15, 16}
	got := KeyframeCuts(keys, 0, 6)
	if want := []float64{0, 14, 15}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAKeyframeExactlyOnABoundaryCuts(t *testing.T) {
	got := KeyframeCuts([]float64{0, 3, 6, 9, 12}, 0, 6)
	if want := []float64{0, 6, 12}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestKeyframeCutsFromAResumeCountFromItsKeyframe(t *testing.T) {
	keys := []float64{0, 5, 10, 15, 20, 25, 30}
	got := KeyframeCuts(keys, 12, 6)
	// Boundaries at 16, 22 and 28 from the keyframe at 10.
	if want := []float64{10, 20, 25, 30}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCopyStartIsJustPastTheKeyframe(t *testing.T) {
	keys := []float64{0, 2, 4.5, 7}
	k, seek, ok := CopyStart(keys, 5)
	if !ok || k != 4.5 || seek != 4.5+copySeekPast {
		t.Fatalf("got %v %v %v", k, seek, ok)
	}
	// The seek, less ffmpeg's 3/23s, is still on the same keyframe.
	if seek-3.0/23 < k {
		t.Fatal("the heuristic would land on the keyframe before")
	}
}

func TestCopyStartRefusesKeyframesTooCloseTogether(t *testing.T) {
	if _, _, ok := CopyStart([]float64{0, 4, 4.1}, 4); ok {
		t.Fatal("a seek cannot sit between keyframes 0.1s apart")
	}
	if _, _, ok := CopyStart([]float64{5, 10}, 2); ok {
		t.Fatal("a start before the first keyframe has no keyframe")
	}
}

func TestKeyframePlaylistListsEachSegmentFinished(t *testing.T) {
	p, ok := KeyframePlaylist([]float64{10, 16.5, 23}, 30, "/api/stream/1/hls/s/", probe.SegmentsMPEGTS)
	if !ok {
		t.Fatal("no playlist")
	}
	for _, want := range []string{
		"#EXT-X-TARGETDURATION:7\n",
		"#EXT-X-PLAYLIST-TYPE:VOD\n",
		"#EXTINF:6.500000,\n/api/stream/1/hls/s/seg00000.ts\n",
		"#EXTINF:6.500000,\n/api/stream/1/hls/s/seg00001.ts\n",
		"#EXTINF:7.000000,\n/api/stream/1/hls/s/seg00002.ts\n",
		"#EXT-X-ENDLIST\n",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("missing %q in\n%s", want, p)
		}
	}
	if strings.Contains(p, "EXT-X-MAP") {
		t.Error("an MPEG-TS playlist has no init segment")
	}
	if ListedCount(p) != 3 {
		t.Errorf("lists %d", ListedCount(p))
	}
}

func TestKeyframePlaylistRefusesMediaThatEndsBeforeItsLastCut(t *testing.T) {
	if _, ok := KeyframePlaylist([]float64{0, 6, 12}, 12, "", probe.SegmentsMPEGTS); ok {
		t.Fatal("listed a last segment of no length")
	}
	if _, ok := KeyframePlaylist(nil, 12, "", probe.SegmentsMPEGTS); ok {
		t.Fatal("listed nothing")
	}
}
