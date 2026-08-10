package photo

import (
	"encoding/binary"
	"errors"
	"time"
)

// A deliberately small EXIF reader: two tags, no dependency.
//
// The alternative was a library, and the cost/benefit did not survive contact
// with the requirement. LANcast wants orientation and capture time; a general
// EXIF package brings a tag dictionary, maker notes, GPS parsing and a decade of
// camera quirks, and the GPS part is data ADR 0028 says explicitly must never be
// loaded. Not having a parser for it is a stronger guarantee than choosing not
// to call it.
//
// This is the same reasoning as the vendored WebView2 binding and the refusal to
// ship hls.js: a dependency is a permanent liability, and a hundred lines that
// do exactly what is needed are cheaper than the surface of a package that does
// everything.

var errNoEXIF = errors.New("no exif")

const (
	tagOrientation       = 0x0112
	tagDateTimeOriginal  = 0x9003
	tagDateTimeDigitized = 0x9004
	tagDateTime          = 0x0132
	tagExifIFD           = 0x8769
)

type exifData struct {
	takenAt     int64
	orientation int
}

// readEXIF finds the TIFF header in a JPEG APP1 segment (or at the start of a
// TIFF file) and reads the two tags that matter.
func readEXIF(raw []byte) (exifData, error) {
	tiff, err := findTIFF(raw)
	if err != nil {
		return exifData{}, err
	}
	if len(tiff) < 8 {
		return exifData{}, errNoEXIF
	}

	var bo binary.ByteOrder
	switch {
	case tiff[0] == 'I' && tiff[1] == 'I':
		bo = binary.LittleEndian
	case tiff[0] == 'M' && tiff[1] == 'M':
		bo = binary.BigEndian
	default:
		return exifData{}, errNoEXIF
	}
	if bo.Uint16(tiff[2:4]) != 42 {
		return exifData{}, errNoEXIF
	}

	out := exifData{}
	// IFD0 carries orientation and a fallback timestamp; the Exif sub-IFD it
	// points at carries the capture time cameras actually set.
	offset := bo.Uint32(tiff[4:8])
	var exifIFD uint32
	readIFD(tiff, bo, offset, func(tag uint16, kind uint16, count uint32, valueOff uint32, value []byte) {
		switch tag {
		case tagOrientation:
			if kind == 3 {
				out.orientation = int(bo.Uint16(value[:2]))
			}
		case tagExifIFD:
			exifIFD = valueOff
		case tagDateTime:
			if out.takenAt == 0 {
				out.takenAt = parseEXIFTime(readString(tiff, bo, kind, count, valueOff, value))
			}
		}
	})
	if exifIFD != 0 {
		readIFD(tiff, bo, exifIFD, func(tag uint16, kind uint16, count uint32, valueOff uint32, value []byte) {
			switch tag {
			case tagDateTimeOriginal, tagDateTimeDigitized:
				// Original wins over digitized, and both win over IFD0's
				// DateTime — which is when the file was last written, not when
				// the shutter fired. An edited photo has a DateTime of the edit.
				if tag == tagDateTimeOriginal || out.takenAt == 0 {
					if t := parseEXIFTime(readString(tiff, bo, kind, count, valueOff, value)); t != 0 {
						out.takenAt = t
					}
				}
			}
		})
	}
	if out.orientation == 0 && out.takenAt == 0 {
		return exifData{}, errNoEXIF
	}
	return out, nil
}

// findTIFF locates the TIFF block: inside a JPEG's APP1 marker, or at byte zero
// of a TIFF file.
func findTIFF(raw []byte) ([]byte, error) {
	if len(raw) >= 4 && (string(raw[:2]) == "II" || string(raw[:2]) == "MM") {
		return raw, nil
	}
	if len(raw) < 4 || raw[0] != 0xFF || raw[1] != 0xD8 {
		return nil, errNoEXIF
	}
	// Walk the JPEG segment chain. Bounded by the buffer, and every step moves
	// forward by a length the file itself declares, so a truncated or hostile
	// file runs out rather than looping.
	i := 2
	for i+4 <= len(raw) {
		if raw[i] != 0xFF {
			return nil, errNoEXIF
		}
		marker := raw[i+1]
		if marker == 0xD8 || (marker >= 0xD0 && marker <= 0xD9) {
			i += 2
			continue
		}
		if i+4 > len(raw) {
			return nil, errNoEXIF
		}
		length := int(binary.BigEndian.Uint16(raw[i+2 : i+4]))
		if length < 2 || i+2+length > len(raw) {
			return nil, errNoEXIF
		}
		if marker == 0xE1 {
			seg := raw[i+4 : i+2+length]
			if len(seg) > 6 && string(seg[:4]) == "Exif" {
				return seg[6:], nil
			}
		}
		// SOS: image data follows and there are no more headers worth walking.
		if marker == 0xDA {
			return nil, errNoEXIF
		}
		i += 2 + length
	}
	return nil, errNoEXIF
}

// readIFD walks one image file directory, calling fn per entry. Every offset is
// bounds-checked against the block: EXIF is attacker-controlled data in a file
// the server was pointed at, and a malformed offset must end the walk rather
// than panic the worker.
func readIFD(tiff []byte, bo binary.ByteOrder, offset uint32, fn func(tag, kind uint16, count, valueOff uint32, value []byte)) {
	if offset == 0 || int(offset)+2 > len(tiff) {
		return
	}
	n := int(bo.Uint16(tiff[offset : offset+2]))
	pos := int(offset) + 2
	for i := 0; i < n; i++ {
		if pos+12 > len(tiff) {
			return
		}
		e := tiff[pos : pos+12]
		tag := bo.Uint16(e[0:2])
		kind := bo.Uint16(e[2:4])
		count := bo.Uint32(e[4:8])
		valueOff := bo.Uint32(e[8:12])
		fn(tag, kind, count, valueOff, e[8:12])
		pos += 12
	}
}

// readString resolves an ASCII tag, which lives inline when short and at an
// offset when not.
func readString(tiff []byte, bo binary.ByteOrder, kind uint16, count, valueOff uint32, inline []byte) string {
	if kind != 2 || count == 0 {
		return ""
	}
	if count <= 4 {
		return trimNul(string(inline[:count]))
	}
	if int(valueOff)+int(count) > len(tiff) {
		return ""
	}
	return trimNul(string(tiff[valueOff : valueOff+count]))
}

func trimNul(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return s[:i]
		}
	}
	return s
}

// parseEXIFTime reads EXIF's "2019:07:14 18:03:22".
//
// Interpreted as local time, deliberately. EXIF's basic timestamp carries no
// zone — the camera wrote wall-clock time where it stood — so any zone chosen
// here is a guess. Local is the guess that matches what the photographer saw on
// the camera, and it is the same reasoning the global rule about UTC dates
// applies in reverse: a date built from UTC would shift a summer evening's
// photos into the next day.
func parseEXIFTime(s string) int64 {
	if s == "" {
		return 0
	}
	t, err := time.ParseInLocation("2006:01:02 15:04:05", s, time.Local)
	if err != nil || t.Year() < 1900 {
		return 0
	}
	return t.Unix()
}
