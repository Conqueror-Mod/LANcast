package api

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"lancast/internal/probe"
)

// mkvWithKeyframes writes the smallest Matroska file the index reader accepts:
// one video track and a cue per keyframe, in milliseconds.
func mkvWithKeyframes(t *testing.T, ms ...uint32) string {
	t.Helper()
	el := func(id []byte, body ...[]byte) []byte {
		n := 0
		for _, b := range body {
			n += len(b)
		}
		out := append(append([]byte{}, id...), 0x01, 0, 0, 0, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
		for _, b := range body {
			out = append(out, b...)
		}
		return out
	}
	u := func(id []byte, v uint32) []byte {
		return el(id, []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
	}
	var points [][]byte
	for _, m := range ms {
		points = append(points, el([]byte{0xBB}, u([]byte{0xB3}, m), el([]byte{0xB7}, u([]byte{0xF7}, 1))))
	}
	data := el([]byte{0x1A, 0x45, 0xDF, 0xA3}, el([]byte{0x42, 0x82}, []byte("matroska")))
	data = append(data, el([]byte{0x18, 0x53, 0x80, 0x67},
		el([]byte{0x16, 0x54, 0xAE, 0x6B}, el([]byte{0xAE}, u([]byte{0xD7}, 1), u([]byte{0x83}, 1))),
		el([]byte{0x1C, 0x53, 0xBB, 0x6B}, points...),
	)...)
	path := filepath.Join(t.TempDir(), "film.mkv")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func rokuCopy() probe.Decision {
	return probe.Decision{Method: probe.Remux, VideoAction: "copy", AudioAction: "copy", Segments: probe.SegmentsMPEGTS}
}

func TestARokuCopyIsListedFromTheKeyframeIndex(t *testing.T) {
	path := mkvWithKeyframes(t, 0, 4000, 8000, 12000, 16000)
	l, ok := keyframeListingFor(path, rokuCopy(), 0, 20)
	if !ok {
		t.Fatal("not listed")
	}
	if want := []float64{0, 8, 12}; l.seek != 0 || !reflect.DeepEqual(l.cuts, want) {
		t.Fatalf("seek %v cuts %v, want 0 and %v", l.seek, l.cuts, want)
	}
}

func TestARokuResumeStartsJustPastItsKeyframe(t *testing.T) {
	path := mkvWithKeyframes(t, 0, 4000, 8000, 12000, 16000)
	l, ok := keyframeListingFor(path, rokuCopy(), 9.5, 20)
	if !ok {
		t.Fatal("not listed")
	}
	if l.seek != 8.2 || l.cuts[0] != 8 {
		t.Fatalf("seek %v, first cut %v; want 8.2 and 8", l.seek, l.cuts[0])
	}
}

func TestOnlyARokuCopyIsListedFromTheIndex(t *testing.T) {
	path := mkvWithKeyframes(t, 0, 4000, 8000)
	fmp4 := rokuCopy()
	fmp4.Segments = ""
	encoded := rokuCopy()
	encoded.VideoAction = "encode"
	audio := rokuCopy()
	audio.AudioOnly = true
	for name, d := range map[string]probe.Decision{"fmp4": fmp4, "encoded": encoded, "audio only": audio} {
		if _, ok := keyframeListingFor(path, d, 0, 12); ok {
			t.Errorf("%s was listed from the index", name)
		}
	}
	if _, ok := keyframeListingFor(path, rokuCopy(), 0, 0); ok {
		t.Error("listed without a length")
	}
}

func TestAFileWithoutAnIndexIsListedAsBefore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "film.mp4")
	if err := os.WriteFile(path, []byte("\x00\x00\x00\x18ftypmp42"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := keyframeListingFor(path, rokuCopy(), 0, 12); ok {
		t.Fatal("an mp4 was listed from a Matroska index")
	}
	if _, ok := keyframeListingFor(filepath.Join(t.TempDir(), "gone.mkv"), rokuCopy(), 0, 12); ok {
		t.Fatal("a missing file was listed")
	}
}
