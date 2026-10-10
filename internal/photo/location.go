package photo

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"os"
)

// Where a photograph was taken, read from EXIF GPS (ADR 0078).
//
// ADR 0028 refused this field, and exif.go was written so that refusal was a
// missing parser rather than a skipped call. It was revisited on purpose on
// 2026-10-04, and this file is that decision: the parser exists now, it lives
// apart from readEXIF so the thumbnail pass still never touches it, and the
// only thing that calls it is the location pass.

const (
	tagGPSIFD       = 0x8825
	tagGPSLatRef    = 0x0001
	tagGPSLat       = 0x0002
	tagGPSLonRef    = 0x0003
	tagGPSLon       = 0x0004
	tagGPSStatus    = 0x0009
	typeASCII       = 2
	typeRational    = 5
	rationalTriplet = 24
)

// Location is a photograph's position in decimal degrees, south and west
// negative.
type Location struct {
	Lat float64
	Lon float64
}

// locationReadLimit bounds how much of a file ReadLocation reads.
//
// Larger than exifReadLimit because of HEIC: a JPEG keeps EXIF in an APP1
// segment within the first 64KB, but a HEIF file keeps it as an item whose
// bytes the container may place after its own index.
const locationReadLimit = 512 * 1024

// ReadLocation returns where a photograph was taken, and false when it does not
// say or says something that cannot be a place.
func ReadLocation(path string) (Location, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Location{}, false
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, locationReadLimit))
	if err != nil {
		return Location{}, false
	}
	return readLocation(raw)
}

func readLocation(raw []byte) (Location, bool) {
	tiff, err := findTIFF(raw)
	if err != nil {
		if tiff = findHEIFExif(raw); tiff == nil {
			return Location{}, false
		}
	}
	bo, ifd0, ok := tiffHeader(tiff)
	if !ok {
		return Location{}, false
	}

	var gpsIFD uint32
	readIFD(tiff, bo, ifd0, func(tag, kind uint16, count, valueOff uint32, value []byte) {
		if tag == tagGPSIFD {
			gpsIFD = valueOff
		}
	})
	if gpsIFD == 0 {
		return Location{}, false
	}

	var latRef, lonRef, status string
	var lat, lon float64
	var haveLat, haveLon bool
	readIFD(tiff, bo, gpsIFD, func(tag, kind uint16, count, valueOff uint32, value []byte) {
		switch tag {
		case tagGPSLatRef:
			latRef = readString(tiff, bo, kind, count, valueOff, value)
		case tagGPSLonRef:
			lonRef = readString(tiff, bo, kind, count, valueOff, value)
		case tagGPSStatus:
			status = readString(tiff, bo, kind, count, valueOff, value)
		case tagGPSLat:
			lat, haveLat = readDegrees(tiff, bo, kind, count, valueOff)
		case tagGPSLon:
			lon, haveLon = readDegrees(tiff, bo, kind, count, valueOff)
		}
	})
	if !haveLat || !haveLon {
		return Location{}, false
	}
	// "V" is a receiver reporting that it had no fix: the coordinates beside
	// it are whatever it last held, or nothing.
	if status == "V" {
		return Location{}, false
	}
	switch latRef {
	case "S":
		lat = -lat
	case "N":
	default:
		return Location{}, false
	}
	switch lonRef {
	case "W":
		lon = -lon
	case "E":
	default:
		return Location{}, false
	}
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return Location{}, false
	}
	// Exactly 0,0 is a camera with GPS and no fix writing zeroes, not a
	// photograph taken in the Gulf of Guinea. Grouping it would make the
	// largest "place" in most libraries a point in the sea.
	if lat == 0 && lon == 0 {
		return Location{}, false
	}
	return Location{Lat: lat, Lon: lon}, true
}

// tiffHeader reads a TIFF block's byte order and the offset of its first IFD.
func tiffHeader(tiff []byte) (binary.ByteOrder, uint32, bool) {
	if len(tiff) < 8 {
		return nil, 0, false
	}
	var bo binary.ByteOrder
	switch {
	case tiff[0] == 'I' && tiff[1] == 'I':
		bo = binary.LittleEndian
	case tiff[0] == 'M' && tiff[1] == 'M':
		bo = binary.BigEndian
	default:
		return nil, 0, false
	}
	if bo.Uint16(tiff[2:4]) != 42 {
		return nil, 0, false
	}
	return bo, bo.Uint32(tiff[4:8]), true
}

// readDegrees reads a GPS coordinate: three RATIONALs, degrees, minutes and
// seconds, always stored at an offset because 24 bytes never fit inline.
func readDegrees(tiff []byte, bo binary.ByteOrder, kind uint16, count, valueOff uint32) (float64, bool) {
	if kind != typeRational || count != 3 {
		return 0, false
	}
	end := int(valueOff) + rationalTriplet
	if int(valueOff) < 0 || end > len(tiff) || end < int(valueOff) {
		return 0, false
	}
	b := tiff[valueOff:end]
	var parts [3]float64
	for i := range parts {
		num := bo.Uint32(b[i*8 : i*8+4])
		den := bo.Uint32(b[i*8+4 : i*8+8])
		if den == 0 {
			// 0/0 is how some writers say "unknown"; a real zero is 0/1.
			if num != 0 {
				return 0, false
			}
			if i == 0 {
				return 0, false
			}
			continue
		}
		parts[i] = float64(num) / float64(den)
	}
	if parts[1] >= 60 || parts[2] >= 60 {
		return 0, false
	}
	d := parts[0] + parts[1]/60 + parts[2]/3600
	if math.IsNaN(d) || math.IsInf(d, 0) {
		return 0, false
	}
	return d, true
}

// findHEIFExif finds the TIFF block inside a HEIF/HEIC file's Exif item.
//
// The proper route is the container's own index (iinf names the item, iloc
// says where its bytes are). The item's payload is always the same shape,
// though — a four-byte offset, then "Exif\0\0", then the TIFF header — and that
// is distinctive enough to find directly. It is only used here, for the
// location; orientation is deliberately left to ffmpeg's conversion for HEIC,
// which already applies the container's rotation.
func findHEIFExif(raw []byte) []byte {
	if len(raw) < 12 || string(raw[4:8]) != "ftyp" {
		return nil
	}
	marker := []byte("Exif\x00\x00")
	for from := 0; from < len(raw); {
		i := bytes.Index(raw[from:], marker)
		if i < 0 {
			return nil
		}
		start := from + i + len(marker)
		if start+4 <= len(raw) {
			h := raw[start : start+4]
			if (h[0] == 'I' && h[1] == 'I' && h[2] == 42 && h[3] == 0) ||
				(h[0] == 'M' && h[1] == 'M' && h[2] == 0 && h[3] == 42) {
				return raw[start:]
			}
		}
		from = start
	}
	return nil
}
