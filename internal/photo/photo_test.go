package photo

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writePNG makes a w×h image with a distinct pixel at the top-left, so a
// rotation can be told apart from a resize.
func writePNG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{10, 10, 10, 255})
		}
	}
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})

	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadRecordsDimensions(t *testing.T) {
	dir := t.TempDir()
	path := writePNG(t, dir, "wide.png", 40, 10)

	d := &Decoder{}
	img, meta, err := d.Read(context.Background(), path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if img == nil {
		t.Fatal("no image")
	}
	if meta.Width != 40 || meta.Height != 10 {
		t.Errorf("dimensions = %dx%d, want 40x10", meta.Width, meta.Height)
	}
	if meta.TakenAt != 0 {
		t.Errorf("TakenAt = %d, want 0 — a bare PNG has no EXIF", meta.TakenAt)
	}
}

// The format is decided by content, not extension. Every "save as" dialog has
// produced a PNG named .jpg at some point, and trusting the name would fail a
// file that decodes perfectly well.
func TestFormatComesFromContentNotExtension(t *testing.T) {
	dir := t.TempDir()
	raw, err := os.ReadFile(writePNG(t, dir, "actually.png", 8, 8))
	if err != nil {
		t.Fatal(err)
	}
	lying := filepath.Join(dir, "liar.jpg")
	if err := os.WriteFile(lying, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	d := &Decoder{}
	_, meta, err := d.Read(context.Background(), lying)
	if err != nil {
		t.Fatalf("a PNG named .jpg must still decode: %v", err)
	}
	if meta.Width != 8 {
		t.Errorf("width = %d, want 8", meta.Width)
	}
}

// A HEIC with no ffmpeg is unsupported, not corrupt. The distinction is what
// lets the worker report "this build cannot read that format" rather than
// blaming the file.
func TestHEICWithoutFFmpegIsUnsupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "phone.heic")
	if err := os.WriteFile(path, []byte("not really a heic"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &Decoder{}
	if _, _, err := d.Read(context.Background(), path); err != ErrUnsupported {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestCorruptSupportedFormatIsAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.png")
	if err := os.WriteFile(path, []byte("\x89PNG\r\n\x1a\n garbage"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &Decoder{}
	_, _, err := d.Read(context.Background(), path)
	if err == nil {
		t.Fatal("a corrupt PNG must be an error")
	}
	if err == ErrUnsupported {
		t.Error("a corrupt file of a supported format must not read as unsupported")
	}
}

// Orientation 6 is a quarter turn, which phones set constantly. The reported
// dimensions must describe the picture as it will be seen: a portrait photo
// reporting landscape dimensions is wrong in the database and wrong in any
// layout reading them before the image loads.
func TestQuarterTurnSwapsReportedDimensions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "portrait.jpg")
	if err := os.WriteFile(path, jpegWithOrientation(t, 20, 10, 6), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &Decoder{}
	_, meta, err := d.Read(context.Background(), path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if meta.Orientation != 6 {
		t.Fatalf("Orientation = %d, want 6", meta.Orientation)
	}
	if meta.Width != 10 || meta.Height != 20 {
		t.Errorf("dimensions = %dx%d, want 10x20 (stored 20x10, turned a quarter)",
			meta.Width, meta.Height)
	}
}

func TestOrientApplied(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 2))
	src.Set(0, 0, color.RGBA{255, 0, 0, 255})

	// 6 is a 90° clockwise turn: the top-left pixel lands top-right.
	got := Orient(src, 6)
	b := got.Bounds()
	if b.Dx() != 2 || b.Dy() != 4 {
		t.Fatalf("bounds = %dx%d, want 2x4", b.Dx(), b.Dy())
	}
	if r, _, _, _ := got.At(1, 0).RGBA(); r>>8 != 255 {
		t.Error("the marked pixel is not top-right after a quarter turn")
	}
}

func TestOrientLeavesAnUnknownValueAlone(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 2))
	if got := Orient(src, 99); got != image.Image(src) {
		t.Error("an orientation outside 1..8 is a malformed file, not an instruction")
	}
}

func TestEXIFTimeIsReadAsLocal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dated.jpg")
	if err := os.WriteFile(path, jpegWithDate(t, "2019:07:14 18:03:22"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &Decoder{}
	_, meta, err := d.Read(context.Background(), path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := time.Date(2019, 7, 14, 18, 3, 22, 0, time.Local).Unix()
	if meta.TakenAt != want {
		t.Errorf("TakenAt = %d, want %d (local, as the camera wrote it)", meta.TakenAt, want)
	}
}

// EXIF is attacker-controlled data in a file the server was merely pointed at.
// A malformed offset must end the walk rather than panic the worker.
func TestMalformedEXIFDoesNotPanic(t *testing.T) {
	cases := [][]byte{
		{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x08, 'E', 'x', 'i', 'f', 0, 0},
		{0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF, 'E', 'x', 'i', 'f'},
		append([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x20, 'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0},
			0xFF, 0xFF, 0xFF, 0x7F),
		{'I', 'I', 42, 0, 0xFF, 0xFF, 0xFF, 0x7F},
		{'M', 'M', 0, 42, 0x7F, 0xFF, 0xFF, 0xFF},
	}
	for i, raw := range cases {
		// The assertion is that this returns at all.
		if _, err := readEXIF(raw); err == nil {
			t.Logf("case %d returned no error, which is fine as long as it returned", i)
		}
	}
}

// jpegWithOrientation builds a real JPEG carrying one EXIF orientation tag.
func jpegWithOrientation(t *testing.T, w, h, orientation int) []byte {
	t.Helper()
	// A SHORT lives in the high half of the 4-byte value field on little-endian.
	return jpegWithEXIF(t, w, h, exifIFD(t, []ifdEntry{
		{tag: tagOrientation, kind: 3, count: 1, value: uint32(orientation)},
	}))
}

func jpegWithDate(t *testing.T, when string) []byte {
	t.Helper()
	// The string lives past the entry: 8 for the TIFF header, 2 for the entry
	// count, 12 for the entry, 4 for the next-IFD pointer.
	off := uint32(8 + 2 + 12 + 4)
	return jpegWithEXIF(t, 4, 4, exifIFD(t, []ifdEntry{
		{tag: tagDateTime, kind: 2, count: uint32(len(when) + 1), value: off, tail: append([]byte(when), 0)},
	}))
}

type ifdEntry struct {
	tag   uint16
	kind  uint16
	count uint32
	value uint32
	tail  []byte
}

// exifIFD builds a little-endian TIFF block holding one IFD.
func exifIFD(t *testing.T, entries []ifdEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("II")
	_ = binary.Write(&buf, binary.LittleEndian, uint16(42))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(8))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(len(entries)))

	var tails []byte
	for _, e := range entries {
		_ = binary.Write(&buf, binary.LittleEndian, e.tag)
		_ = binary.Write(&buf, binary.LittleEndian, e.kind)
		_ = binary.Write(&buf, binary.LittleEndian, e.count)
		_ = binary.Write(&buf, binary.LittleEndian, e.value)
		tails = append(tails, e.tail...)
	}
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0)) // no next IFD
	buf.Write(tails)
	return buf.Bytes()
}

// jpegWithEXIF wraps a real encoded JPEG in an APP1 segment carrying tiff.
func jpegWithEXIF(t *testing.T, w, h int, tiff []byte) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var body bytes.Buffer
	if err := jpeg.Encode(&body, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	raw := body.Bytes()

	seg := append([]byte("Exif\x00\x00"), tiff...)
	var out bytes.Buffer
	out.Write(raw[:2]) // SOI
	out.Write([]byte{0xFF, 0xE1})
	_ = binary.Write(&out, binary.BigEndian, uint16(len(seg)+2))
	out.Write(seg)
	out.Write(raw[2:])
	return out.Bytes()
}
