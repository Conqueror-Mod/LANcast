package picture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

func solid(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func pngBytes(t testing.TB, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func jpegBytes(t testing.TB, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// decodeJPEG checks an output is a JPEG of the given size and returns it.
func decodeJPEG(t *testing.T, b []byte, size int) image.Image {
	t.Helper()
	img, format, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("output does not decode: %v", err)
	}
	if format != "jpeg" {
		t.Errorf("output is %s, want jpeg", format)
	}
	if r := img.Bounds(); r.Dx() != size || r.Dy() != size {
		t.Errorf("output is %d × %d, want %d × %d", r.Dx(), r.Dy(), size, size)
	}
	return img
}

func TestEachAcceptedFormatGivesTwoJPEGs(t *testing.T) {
	webp, err := os.ReadFile("testdata/small.webp")
	if err != nil {
		t.Fatal(err)
	}
	for name, in := range map[string][]byte{
		"png":  pngBytes(t, solid(40, 30, color.RGBA{200, 10, 10, 255})),
		"jpeg": jpegBytes(t, solid(40, 30, color.RGBA{200, 10, 10, 255})),
		"webp": webp,
	} {
		t.Run(name, func(t *testing.T) {
			res, err := Make(bytes.NewReader(in))
			if err != nil {
				t.Fatal(err)
			}
			decodeJPEG(t, res.Large, Large)
			decodeJPEG(t, res.Small, Small)
		})
	}
}

// The reason the server re-draws: whatever rode in the file does not come out.
func TestEXIFDoesNotSurvive(t *testing.T) {
	plain := jpegBytes(t, solid(64, 64, color.RGBA{10, 200, 10, 255}))
	payload := append([]byte("Exif\x00\x00"), []byte("GPS-LATITUDE-51.4778N")...)
	app1 := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(app1[2:], uint16(len(payload)+2))
	withExif := append(append(append([]byte{}, plain[:2]...), app1...), payload...)
	withExif = append(withExif, plain[2:]...)

	res, err := Make(bytes.NewReader(withExif))
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range [][]byte{res.Large, res.Small} {
		if bytes.Contains(out, []byte("GPS-LATITUDE")) || bytes.Contains(out, []byte("Exif")) {
			t.Error("the uploaded EXIF block is in the stored picture")
		}
	}
}

// Wide pictures keep their middle.
func TestCropsTheCentreSquare(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 300, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 300; x++ {
			c := color.RGBA{0, 220, 0, 255} // middle third: green
			if x < 100 {
				c = color.RGBA{220, 0, 0, 255}
			} else if x >= 200 {
				c = color.RGBA{0, 0, 220, 255}
			}
			img.Set(x, y, c)
		}
	}
	res, err := Make(bytes.NewReader(pngBytes(t, img)))
	if err != nil {
		t.Fatal(err)
	}
	out := decodeJPEG(t, res.Large, Large)
	for _, p := range []image.Point{{5, 128}, {128, 128}, {250, 128}} {
		r, g, b, _ := out.At(p.X, p.Y).RGBA()
		if g>>8 < 180 || r>>8 > 60 || b>>8 > 60 {
			t.Errorf("pixel %v = (%d,%d,%d), want the green middle", p, r>>8, g>>8, b>>8)
		}
	}
}

// Transparent areas take the page colour, not black.
func TestTransparencyIsFlattenedOntoThePage(t *testing.T) {
	res, err := Make(bytes.NewReader(pngBytes(t, solid(32, 32, color.RGBA{}))))
	if err != nil {
		t.Fatal(err)
	}
	out := decodeJPEG(t, res.Small, Small)
	r, g, b, _ := out.At(32, 32).RGBA()
	if d := int(b>>8) - int(background.B); d < -6 || d > 6 || r>>8 > 16 || g>>8 > 24 {
		t.Errorf("transparent pixel drew (%d,%d,%d), want about the page colour %v", r>>8, g>>8, b>>8, background)
	}
}

func refused(t *testing.T, in []byte) {
	t.Helper()
	if _, err := Make(bytes.NewReader(in)); !errors.Is(err, ErrRefused) {
		t.Errorf("err = %v, want ErrRefused", err)
	}
}

func TestGIFIsRefusedThoughItDecodes(t *testing.T) {
	var b bytes.Buffer
	pal := image.NewPaletted(image.Rect(0, 0, 32, 32), color.Palette{color.Black, color.White})
	if err := gif.Encode(&b, pal, nil); err != nil {
		t.Fatal(err)
	}
	refused(t, b.Bytes())
}

func TestNotAnImageIsRefused(t *testing.T) {
	refused(t, []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"))
	refused(t, nil)
}

func TestTooSmallIsRefused(t *testing.T) {
	refused(t, pngBytes(t, solid(8, 40, color.White)))
}

func TestTooLargeAFileIsRefused(t *testing.T) {
	big := append(pngBytes(t, solid(32, 32, color.White)), make([]byte, MaxBytes)...)
	refused(t, big)
}

/*
 * A header that claims a huge image is refused from the header alone.
 *
 * The IDAT is missing entirely, so if Make got as far as decoding it would
 * fail anyway. What this pins is the order: the check reads dimensions first,
 * and the message says it was the size.
 */
func TestAHugeHeaderIsRefusedBeforeDecoding(t *testing.T) {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 100_000)
	binary.BigEndian.PutUint32(ihdr[4:], 100_000)
	ihdr[8], ihdr[9] = 8, 6 // 8-bit RGBA
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))

	_, err := Make(bytes.NewReader(b.Bytes()))
	if !errors.Is(err, ErrRefused) || !bytes.Contains([]byte(err.Error()), []byte("too large")) {
		t.Errorf("err = %v, want refused as too large", err)
	}
}

// Any bytes in: a refusal, or two valid JPEGs of the right size. Never a panic.
func FuzzMake(f *testing.F) {
	t := &testing.T{}
	f.Add(pngBytes(t, solid(40, 30, color.RGBA{1, 2, 3, 128})))
	f.Add(jpegBytes(t, solid(20, 50, color.RGBA{9, 8, 7, 255})))
	if webp, err := os.ReadFile("testdata/small.webp"); err == nil {
		f.Add(webp)
	}
	f.Add([]byte("not an image"))
	f.Fuzz(func(t *testing.T, in []byte) {
		res, err := Make(bytes.NewReader(in))
		if err != nil {
			return
		}
		decodeJPEG(t, res.Large, Large)
		decodeJPEG(t, res.Small, Small)
	})
}
