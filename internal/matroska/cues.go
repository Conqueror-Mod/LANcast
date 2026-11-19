/*
Package matroska reads one thing out of a Matroska file: where its video
keyframes are, from the file's own index (Cues).

# Why

A Roku gives up on a playlist request after about thirty seconds. A copied video
track could only be listed once ffmpeg had finished cutting it, because a
segment starts on a keyframe and the keyframes were the source's, at places
nothing here had read. A remux of a large film takes longer than thirty seconds,
so the TV asked, waited, gave up and asked again, measured at up to four times
for one film (ADR 0125, amended).

A Matroska file carries an index of its keyframes for seeking. Reading it is a
few small reads, against a remux that reads the whole file, so the server can
list every segment before ffmpeg has written one, as it already does for an
encode, whose keyframes it places itself.

# What it trusts

Nothing about the file. Every size is bounded before anything is allocated, an
element that claims to run past its parent is refused, and the reader is fuzzed.
A file it cannot read returns an error, and the caller lists the playlist the
way it did before.
*/
package matroska

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"sort"
)

// Element IDs, with their length markers, as the specification writes them.
const (
	idEBML           = 0x1A45DFA3
	idDocType        = 0x4282
	idSegment        = 0x18538067
	idSeekHead       = 0x114D9B74
	idSeek           = 0x4DBB
	idSeekID         = 0x53AB
	idSeekPosition   = 0x53AC
	idInfo           = 0x1549A966
	idTimestampScale = 0x2AD7B1
	idTracks         = 0x1654AE6B
	idTrackEntry     = 0xAE
	idTrackNumber    = 0xD7
	idTrackType      = 0x83
	idCues           = 0x1C53BB6B
	idCuePoint       = 0xBB
	idCueTime        = 0xB3
	idCueTrackPos    = 0xB7
	idCueTrack       = 0xF7
	idCluster        = 0x1F43B675
)

// Bounds on what one element may make this read into memory.
const (
	maxHeader   = 64 << 10 // the EBML header
	maxSeekHead = 1 << 20
	maxInfo     = 1 << 20
	maxTracks   = 16 << 20 // a track entry can carry codec private data
	maxCues     = 64 << 20 // about a million cue points
	// maxTopLevel is how many top-level elements are walked looking for the
	// index when no seek head points at it, clusters included.
	maxTopLevel = 200000
)

// unknownSize is an element size of all ones: "until the parent ends".
const unknownSize = math.MaxUint64

var (
	// ErrNotMatroska is a file that is not Matroska or WebM.
	ErrNotMatroska = errors.New("matroska: not a matroska file")
	// ErrNoIndex is a Matroska file without a usable keyframe index.
	ErrNoIndex = errors.New("matroska: no keyframe index for the video track")
	errBad     = errors.New("matroska: malformed element")
)

/*
Keyframes returns the times, in seconds from the start of the file's clock and
in order, of the first video track's keyframes as the file's index lists them.

At least two are returned, or an error.
*/
func Keyframes(r io.ReadSeeker) ([]float64, error) {
	size, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	rd := &reader{r: r, size: size}

	// The EBML header says what kind of file this is.
	id, n, hdrLen, err := rd.element(0, size)
	if err != nil || id != idEBML {
		return nil, ErrNotMatroska
	}
	if n > maxHeader {
		return nil, ErrNotMatroska
	}
	body, err := rd.read(hdrLen, int64(n))
	if err != nil {
		return nil, ErrNotMatroska
	}
	doc := ""
	if err := children(body, func(id uint64, b []byte) error {
		if id == idDocType {
			doc = string(b)
		}
		return nil
	}); err != nil || (doc != "matroska" && doc != "webm") {
		return nil, ErrNotMatroska
	}

	// The segment holds everything else.
	segAt := hdrLen + int64(n)
	id, n, segHdr, err := rd.element(segAt, size)
	if err != nil || id != idSegment {
		return nil, ErrNotMatroska
	}
	segStart := segAt + segHdr
	segEnd := size
	if n != unknownSize && segStart+int64(n) < size {
		segEnd = segStart + int64(n)
	}

	var (
		scale   uint64 = 1000000 // nanoseconds per tick: the default
		video   uint64
		haveTrk bool
		cues    []byte
		seek    = map[uint64]int64{}
	)
	handle := func(id uint64, b []byte) error {
		switch id {
		case idSeekHead:
			return children(b, func(id uint64, b []byte) error {
				if id != idSeek {
					return nil
				}
				var sid uint64
				var pos int64 = -1
				if err := children(b, func(id uint64, b []byte) error {
					switch id {
					case idSeekID:
						sid = beUint(b)
					case idSeekPosition:
						if v := beUint(b); v < uint64(segEnd-segStart) {
							pos = int64(v)
						}
					}
					return nil
				}); err != nil {
					return err
				}
				if pos >= 0 {
					if _, ok := seek[sid]; !ok {
						seek[sid] = pos
					}
				}
				return nil
			})
		case idInfo:
			return children(b, func(id uint64, b []byte) error {
				if id == idTimestampScale {
					if v := beUint(b); v > 0 {
						scale = v
					}
				}
				return nil
			})
		case idTracks:
			return children(b, func(id uint64, b []byte) error {
				if id != idTrackEntry || haveTrk {
					return nil
				}
				var num, typ uint64
				if err := children(b, func(id uint64, b []byte) error {
					switch id {
					case idTrackNumber:
						num = beUint(b)
					case idTrackType:
						typ = beUint(b)
					}
					return nil
				}); err != nil {
					return err
				}
				if typ == 1 && num > 0 {
					video, haveTrk = num, true
				}
				return nil
			})
		case idCues:
			cues = b
		}
		return nil
	}
	limit := func(id uint64) int64 {
		switch id {
		case idSeekHead:
			return maxSeekHead
		case idInfo:
			return maxInfo
		case idTracks:
			return maxTracks
		case idCues:
			return maxCues
		}
		return 0
	}
	// readAt reads and handles the element at off, if it is the one wanted.
	readAt := func(off int64, want uint64) error {
		id, n, h, err := rd.element(off, segEnd)
		if err != nil {
			return err
		}
		if id != want || n == unknownSize || int64(n) > limit(id) {
			return errBad
		}
		b, err := rd.read(off+h, int64(n))
		if err != nil {
			return err
		}
		return handle(id, b)
	}

	/*
	 * Walk the top level up to the first cluster: the seek head, the info and
	 * the tracks are almost always before it. The index is usually after the
	 * last cluster, and the seek head says where.
	 */
	off := segStart
	walked := 0
	for off < segEnd && walked < maxTopLevel {
		walked++
		id, n, h, err := rd.element(off, segEnd)
		if err != nil {
			break
		}
		if id == idCluster && (cues == nil || !haveTrk) {
			// Jump to what the seek head knows before walking clusters.
			for _, want := range []uint64{idInfo, idTracks, idCues} {
				if pos, ok := seek[want]; ok {
					_ = readAt(segStart+pos, want)
					delete(seek, want)
				}
			}
			if cues != nil && haveTrk {
				break
			}
		}
		if n == unknownSize {
			break // an open-ended cluster: nothing after it can be found by walking
		}
		if l := limit(id); l > 0 {
			if int64(n) > l {
				return nil, errBad
			}
			b, err := rd.read(off+h, int64(n))
			if err != nil {
				return nil, err
			}
			if err := handle(id, b); err != nil {
				return nil, err
			}
			if cues != nil && haveTrk {
				break
			}
		}
		next := off + h + int64(n)
		if next <= off {
			break
		}
		off = next
	}
	if !haveTrk || cues == nil {
		return nil, ErrNoIndex
	}

	var times []uint64
	err = children(cues, func(id uint64, b []byte) error {
		if id != idCuePoint {
			return nil
		}
		var t uint64
		var haveT, forVideo bool
		if err := children(b, func(id uint64, b []byte) error {
			switch id {
			case idCueTime:
				t, haveT = beUint(b), true
			case idCueTrackPos:
				return children(b, func(id uint64, b []byte) error {
					if id == idCueTrack && beUint(b) == video {
						forVideo = true
					}
					return nil
				})
			}
			return nil
		}); err != nil {
			return err
		}
		if haveT && forVideo {
			times = append(times, t)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	out := make([]float64, 0, len(times))
	for i, t := range times {
		if i > 0 && t == times[i-1] {
			continue
		}
		out = append(out, float64(t)*float64(scale)/1e9)
	}
	if len(out) < 2 {
		return nil, ErrNoIndex
	}
	return out, nil
}

type reader struct {
	r    io.ReadSeeker
	size int64
}

// element reads the ID and size of the element at off, which must end by end.
// It returns the header's own length.
func (rd *reader) element(off, end int64) (id, size uint64, hdr int64, err error) {
	var buf [12]byte
	n := int64(len(buf))
	if off+n > rd.size {
		n = rd.size - off
	}
	if n < 2 {
		return 0, 0, 0, io.ErrUnexpectedEOF
	}
	b, err := rd.read(off, n)
	if err != nil {
		return 0, 0, 0, err
	}
	id, il, ok := vint(b, true)
	if !ok {
		return 0, 0, 0, errBad
	}
	size, sl, ok := vint(b[il:], false)
	if !ok {
		return 0, 0, 0, errBad
	}
	hdr = int64(il + sl)
	if off+hdr > end || (size != unknownSize && size > uint64(end-off-hdr)) {
		return 0, 0, 0, errBad
	}
	return id, size, hdr, nil
}

func (rd *reader) read(off, n int64) ([]byte, error) {
	if off < 0 || n < 0 || off+n > rd.size {
		return nil, io.ErrUnexpectedEOF
	}
	if _, err := rd.r.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(rd.r, b); err != nil {
		return nil, err
	}
	return b, nil
}

/*
vint decodes a variable-length integer. An ID keeps its length marker, as IDs
are written in the specification; a size drops it, and a size of all ones is
unknownSize.
*/
func vint(b []byte, keepMarker bool) (uint64, int, bool) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0, false
	}
	l := 1
	for mask := byte(0x80); b[0]&mask == 0; mask >>= 1 {
		l++
	}
	if l > 8 || len(b) < l || (keepMarker && l > 4) {
		return 0, 0, false
	}
	v := uint64(b[0])
	if !keepMarker {
		v &= uint64(0xFF >> l)
	}
	allOnes := v == uint64(0xFF>>l)
	for i := 1; i < l; i++ {
		v = v<<8 | uint64(b[i])
		allOnes = allOnes && b[i] == 0xFF
	}
	if !keepMarker && allOnes {
		return unknownSize, l, true
	}
	return v, l, true
}

// children calls fn for each child element in an element's body, in order.
func children(body []byte, fn func(id uint64, b []byte) error) error {
	for len(body) > 0 {
		id, il, ok := vint(body, true)
		if !ok {
			return errBad
		}
		size, sl, ok := vint(body[il:], false)
		if !ok {
			return errBad
		}
		h := il + sl
		if size == unknownSize || size > uint64(len(body)-h) {
			return errBad
		}
		if err := fn(id, body[h:h+int(size)]); err != nil {
			return err
		}
		body = body[h+int(size):]
	}
	return nil
}

// beUint reads a big-endian unsigned integer of up to eight bytes.
func beUint(b []byte) uint64 {
	if len(b) > 8 {
		return 0
	}
	var buf [8]byte
	copy(buf[8-len(b):], b)
	return binary.BigEndian.Uint64(buf[:])
}
