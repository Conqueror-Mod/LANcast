package photo

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// gpsTIFF builds a little-endian TIFF block whose IFD0 points at a GPS IFD
// holding the given references and degree/minute/second triplets. A nil
// triplet leaves that tag out.
func gpsTIFF(latRef string, lat []uint32, lonRef string, lon []uint32, status string) []byte {
	type entry struct {
		tag, kind uint16
		count     uint32
		value     []byte // inline (<=4 bytes) or out-of-line payload
	}
	var gps []entry
	ascii := func(s string) []byte { return append([]byte(s), 0) }
	rational := func(v []uint32) []byte {
		var b bytes.Buffer
		for _, n := range v {
			_ = binary.Write(&b, binary.LittleEndian, n)
		}
		return b.Bytes()
	}
	if latRef != "" {
		gps = append(gps, entry{tagGPSLatRef, typeASCII, 2, ascii(latRef)})
	}
	if lat != nil {
		gps = append(gps, entry{tagGPSLat, typeRational, 3, rational(lat)})
	}
	if lonRef != "" {
		gps = append(gps, entry{tagGPSLonRef, typeASCII, 2, ascii(lonRef)})
	}
	if lon != nil {
		gps = append(gps, entry{tagGPSLon, typeRational, 3, rational(lon)})
	}
	if status != "" {
		gps = append(gps, entry{tagGPSStatus, typeASCII, 2, ascii(status)})
	}

	// Layout: header (8) | IFD0: count, one entry, next (2+12+4) | GPS IFD |
	// out-of-line values.
	const ifd0 = 8
	gpsIFD := uint32(ifd0 + 2 + 12 + 4)
	valuesAt := gpsIFD + 2 + uint32(len(gps))*12 + 4

	var out bytes.Buffer
	le := binary.LittleEndian
	out.WriteString("II")
	_ = binary.Write(&out, le, uint16(42))
	_ = binary.Write(&out, le, uint32(ifd0))
	_ = binary.Write(&out, le, uint16(1))
	_ = binary.Write(&out, le, uint16(tagGPSIFD))
	_ = binary.Write(&out, le, uint16(4))
	_ = binary.Write(&out, le, uint32(1))
	_ = binary.Write(&out, le, gpsIFD)
	_ = binary.Write(&out, le, uint32(0))

	var tail bytes.Buffer
	_ = binary.Write(&out, le, uint16(len(gps)))
	for _, e := range gps {
		_ = binary.Write(&out, le, e.tag)
		_ = binary.Write(&out, le, e.kind)
		_ = binary.Write(&out, le, e.count)
		if len(e.value) <= 4 {
			v := make([]byte, 4)
			copy(v, e.value)
			out.Write(v)
		} else {
			_ = binary.Write(&out, le, valuesAt+uint32(tail.Len()))
			tail.Write(e.value)
		}
	}
	_ = binary.Write(&out, le, uint32(0))
	out.Write(tail.Bytes())
	return out.Bytes()
}

// 33° 35' 3.6" N, 101° 52' 41.88" W — Texas Tech, as a phone writes it.
var (
	techLat = []uint32{33, 1, 35, 1, 36, 10}
	techLon = []uint32{101, 1, 52, 1, 4188, 100}
)

func TestALocationIsReadFromAJPEG(t *testing.T) {
	raw := jpegWithEXIF(t, 4, 4, gpsTIFF("N", techLat, "W", techLon, ""))
	path := filepath.Join(t.TempDir(), "tech.jpg")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	loc, ok := ReadLocation(path)
	if !ok {
		t.Fatal("no location")
	}
	if math.Abs(loc.Lat-33.5843) > 1e-4 || math.Abs(loc.Lon-(-101.8783)) > 1e-4 {
		t.Errorf("got %.5f, %.5f, want 33.5843, -101.8783 (west is negative)", loc.Lat, loc.Lon)
	}
}

func TestSouthIsNegative(t *testing.T) {
	loc, ok := readLocation(gpsTIFF("S", techLat, "E", techLon, ""))
	if !ok || loc.Lat >= 0 || loc.Lon <= 0 {
		t.Errorf("got %v ok=%v, want south negative and east positive", loc, ok)
	}
}

// A camera with a GPS module and no fix writes zeroes. Grouping those would
// make a point in the Gulf of Guinea the biggest place in most libraries.
func TestZeroZeroIsNoLocation(t *testing.T) {
	zero := []uint32{0, 1, 0, 1, 0, 1}
	if loc, ok := readLocation(gpsTIFF("N", zero, "E", zero, "")); ok {
		t.Errorf("0,0 read as %v", loc)
	}
}

// "V" is the receiver saying the measurement is void.
func TestAVoidFixIsNoLocation(t *testing.T) {
	if loc, ok := readLocation(gpsTIFF("N", techLat, "W", techLon, "V")); ok {
		t.Errorf("void fix read as %v", loc)
	}
}

// Without a reference the sign is unknown, and guessing north-east would put
// the western hemisphere in Asia.
func TestAMissingReferenceIsNoLocation(t *testing.T) {
	if loc, ok := readLocation(gpsTIFF("", techLat, "W", techLon, "")); ok {
		t.Errorf("no latitude reference, read as %v", loc)
	}
}

func TestOutOfRangeIsNoLocation(t *testing.T) {
	cases := map[string][]uint32{
		"degrees past 90":    {91, 1, 0, 1, 0, 1},
		"sixty minutes":      {33, 1, 60, 1, 0, 1},
		"zero denominator":   {33, 0, 0, 1, 0, 1},
		"unknown as 0/0 lat": {0, 0, 0, 0, 0, 0},
	}
	for name, lat := range cases {
		if loc, ok := readLocation(gpsTIFF("N", lat, "W", techLon, "")); ok {
			t.Errorf("%s: read as %v", name, loc)
		}
	}
}

// The thumbnail pass reads two tags and must go on reading only two: a photo
// carrying GPS and nothing else is, as far as readEXIF is concerned, a photo
// with no EXIF.
func TestTheThumbnailPassStillNeverSeesGPS(t *testing.T) {
	if _, err := readEXIF(gpsTIFF("N", techLat, "W", techLon, "")); err != errNoEXIF {
		t.Errorf("readEXIF on a GPS-only block: err = %v, want errNoEXIF", err)
	}
}

// A HEIF file keeps EXIF as an item: a four-byte offset, "Exif\0\0", then the
// TIFF block. The item type in the container's index is also spelled "Exif",
// followed by zeroes, so the first match is not the payload.
func TestALocationIsReadFromAHEIF(t *testing.T) {
	var raw bytes.Buffer
	raw.Write([]byte{0, 0, 0, 24})
	raw.WriteString("ftypheic")
	raw.Write(make([]byte, 12))
	raw.WriteString("infe....Exif\x00\x00\x00\x00") // the index's mention
	raw.Write(make([]byte, 64))
	raw.Write([]byte{0, 0, 0, 6})
	raw.WriteString("Exif\x00\x00")
	raw.Write(gpsTIFF("N", techLat, "W", techLon, ""))

	loc, ok := readLocation(raw.Bytes())
	if !ok || math.Abs(loc.Lat-33.5843) > 1e-4 {
		t.Errorf("got %v ok=%v, want Texas Tech", loc, ok)
	}
}

// EXIF is attacker-controlled. Offsets that point past the block must end the
// walk, not panic the worker.
func TestMalformedGPSDoesNotPanic(t *testing.T) {
	good := gpsTIFF("N", techLat, "W", techLon, "")
	for cut := 0; cut < len(good); cut++ {
		readLocation(good[:cut])
	}
	bad := append([]byte(nil), good...)
	// Point the GPS IFD far past the end.
	binary.LittleEndian.PutUint32(bad[8+2+8:], 0x7FFFFFF0)
	if _, ok := readLocation(bad); ok {
		t.Error("an IFD offset past the end produced a location")
	}
}
