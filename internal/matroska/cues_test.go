package matroska

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

// el writes one element: its ID as written in the specification, an eight-byte
// size, and the body.
func el(id uint64, body ...[]byte) []byte {
	var b []byte
	switch {
	case id > 0xFFFFFF:
		b = append(b, byte(id>>24), byte(id>>16), byte(id>>8), byte(id))
	case id > 0xFFFF:
		b = append(b, byte(id>>16), byte(id>>8), byte(id))
	case id > 0xFF:
		b = append(b, byte(id>>8), byte(id))
	default:
		b = append(b, byte(id))
	}
	var n int
	for _, p := range body {
		n += len(p)
	}
	b = append(b, 0x01, 0, 0, byte(n>>32), byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	for _, p := range body {
		b = append(b, p...)
	}
	return b
}

func u(id, v uint64) []byte {
	return el(id, []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

func header(doc string) []byte { return el(idEBML, el(idDocType, []byte(doc))) }

func tracks(entries ...[2]uint64) []byte {
	var b [][]byte
	for _, e := range entries {
		b = append(b, el(idTrackEntry, u(idTrackNumber, e[0]), u(idTrackType, e[1])))
	}
	return el(idTracks, b...)
}

func cues(track uint64, ticks ...uint64) []byte {
	var b [][]byte
	for _, t := range ticks {
		b = append(b, el(idCuePoint, u(idCueTime, t), el(idCueTrackPos, u(idCueTrack, track))))
	}
	return el(idCues, b...)
}

func cluster(n int) []byte { return el(idCluster, make([]byte, n)) }

// seekHead points at the elements given, by their offsets in the segment.
func seekHead(at map[uint64]uint64) []byte {
	var b [][]byte
	for id, pos := range at {
		b = append(b, el(idSeek, u(idSeekID, id), u(idSeekPosition, pos)))
	}
	return el(idSeekHead, b...)
}

func file(segment ...[]byte) []byte {
	return append(header("matroska"), el(idSegment, segment...)...)
}

func TestKeyframesFromAnIndexAfterTheClusters(t *testing.T) {
	// The usual layout: seek head, info, tracks, clusters, then the index, which
	// the seek head points at. Its size is fixed, so the offset can be worked out
	// before it is written.
	info := el(idInfo, u(idTimestampScale, 1000000))
	trk := tracks([2]uint64{1, 1}, [2]uint64{2, 2})
	sh := seekHead(map[uint64]uint64{idCues: 0})
	cl := append(cluster(5000), cluster(5000)...)
	pos := uint64(len(sh) + len(info) + len(trk) + len(cl))
	sh = seekHead(map[uint64]uint64{idCues: pos})
	data := file(sh, info, trk, cl, cues(1, 0, 2002, 4004, 6006))

	got, err := Keyframes(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if want := []float64{0, 2.002, 4.004, 6.006}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestKeyframesFoundByWalkingWithoutASeekHead(t *testing.T) {
	data := file(tracks([2]uint64{1, 1}), cluster(100), cluster(100), cues(1, 0, 5000, 10000))
	got, err := Keyframes(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if want := []float64{0, 5, 10}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestKeyframesUseTheFilesTimestampScale(t *testing.T) {
	// A tick of 100µs rather than the default millisecond.
	data := file(el(idInfo, u(idTimestampScale, 100000)), tracks([2]uint64{1, 1}), cues(1, 0, 60060))
	got, err := Keyframes(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if want := []float64{0, 6.006}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestKeyframesAreTheVideoTracksOnly(t *testing.T) {
	// Audio is track 1 and video track 2; the audio's cues are not keyframes.
	idx := el(idCues,
		el(idCuePoint, u(idCueTime, 0), el(idCueTrackPos, u(idCueTrack, 1))),
		el(idCuePoint, u(idCueTime, 1000), el(idCueTrackPos, u(idCueTrack, 2))),
		el(idCuePoint, u(idCueTime, 3000), el(idCueTrackPos, u(idCueTrack, 1))),
		el(idCuePoint, u(idCueTime, 4000), el(idCueTrackPos, u(idCueTrack, 2))),
	)
	data := file(tracks([2]uint64{1, 2}, [2]uint64{2, 1}), idx)
	got, err := Keyframes(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if want := []float64{1, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestKeyframesAreSortedAndDeduplicated(t *testing.T) {
	data := file(tracks([2]uint64{1, 1}), cues(1, 4000, 0, 2000, 2000))
	got, err := Keyframes(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if want := []float64{0, 2, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestNotMatroska(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":     nil,
		"mp4":       []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom"),
		"other doc": append(header("lancast"), el(idSegment, tracks([2]uint64{1, 1}), cues(1, 0, 1000))...),
	} {
		if _, err := Keyframes(bytes.NewReader(data)); !errors.Is(err, ErrNotMatroska) {
			t.Errorf("%s: got %v, want ErrNotMatroska", name, err)
		}
	}
}

func TestWebMIsRead(t *testing.T) {
	data := append(header("webm"), el(idSegment, tracks([2]uint64{1, 1}), cues(1, 0, 1000))...)
	if _, err := Keyframes(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
}

func TestNoIndex(t *testing.T) {
	for name, data := range map[string][]byte{
		"no cues":         file(tracks([2]uint64{1, 1}), cluster(10)),
		"no video track":  file(tracks([2]uint64{1, 2}), cues(1, 0, 1000)),
		"one keyframe":    file(tracks([2]uint64{1, 1}), cues(1, 0)),
		"cues for others": file(tracks([2]uint64{1, 1}), cues(7, 0, 1000)),
	} {
		if _, err := Keyframes(bytes.NewReader(data)); !errors.Is(err, ErrNoIndex) {
			t.Errorf("%s: got %v, want ErrNoIndex", name, err)
		}
	}
}

func TestASizeThatOverrunsItsParentIsRefused(t *testing.T) {
	data := file(tracks([2]uint64{1, 1}), cues(1, 0, 1000))
	// Claim the cues run a gigabyte; the file is a few hundred bytes.
	i := bytes.Index(data, []byte{0x1C, 0x53, 0xBB, 0x6B})
	copy(data[i+4:], []byte{0x01, 0, 0, 0, 0x40, 0, 0, 0})
	if _, err := Keyframes(bytes.NewReader(data)); err == nil {
		t.Fatal("an element larger than its file was read")
	}
}

func TestUnknownSizeSegmentRunsToTheEnd(t *testing.T) {
	body := append(tracks([2]uint64{1, 1}), cues(1, 0, 1000)...)
	seg := append([]byte{0x18, 0x53, 0x80, 0x67, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, body...)
	data := append(header("matroska"), seg...)
	if _, err := Keyframes(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
}

func FuzzKeyframes(f *testing.F) {
	f.Add(file(tracks([2]uint64{1, 1}), cues(1, 0, 2002, 4004)))
	f.Add(file(seekHead(map[uint64]uint64{idCues: 40}), tracks([2]uint64{1, 1}), cluster(10), cues(1, 0, 1)))
	f.Add(header("webm"))
	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := Keyframes(bytes.NewReader(data))
		if err != nil {
			return
		}
		if len(got) < 2 {
			t.Fatalf("returned %d keyframes without an error", len(got))
		}
		for i := 1; i < len(got); i++ {
			if !(got[i] > got[i-1]) {
				t.Fatalf("not strictly increasing at %d: %v", i, got)
			}
		}
	})
}
