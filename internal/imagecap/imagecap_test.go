package imagecap_test

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"testing"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"

	"lancast/internal/imagecap"
	"lancast/internal/imagecap/imagecaptest"
)

func smallPNG(t testing.TB, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// The file from the finding: a header that declares 4 TB. Without the check
// this test does not fail, it kills the test binary.
func TestAHugeDeclaredSizeIsRefusedBeforeDecoding(t *testing.T) {
	bomb := imagecaptest.BombPNG(1_000_000, 1_000_000)
	if _, _, err := imagecap.Decode(bomb); !errors.Is(err, imagecap.ErrTooLarge) {
		t.Fatalf("Decode: want ErrTooLarge, got %v", err)
	}
	if _, _, err := imagecap.DecodeFrom(bytes.NewReader(bomb)); !errors.Is(err, imagecap.ErrTooLarge) {
		t.Fatalf("DecodeFrom: want ErrTooLarge, got %v", err)
	}
}

// The limit is a product, taken wide: a sliver that is enormous in one
// direction is as large as a square, and two sides that overflow int32 when
// multiplied must not wrap under the limit.
func TestTheLimitIsOnPixelsNotSides(t *testing.T) {
	for _, c := range []struct {
		w, h int
		ok   bool
	}{
		{16320, 12240, true}, // a 200 MP phone sensor
		{imagecap.MaxPixels, 1, true},
		{imagecap.MaxPixels + 1, 1, false},
		{1, imagecap.MaxPixels + 1, false},
		{65536, 65536, false},
		{1<<31 - 1, 1<<31 - 1, false},
		{-1, 10, false},
	} {
		err := imagecap.Check(image.Config{Width: c.w, Height: c.h})
		if (err == nil) != c.ok {
			t.Errorf("%d × %d: ok=%v, got %v", c.w, c.h, c.ok, err)
		}
	}
}

func TestAnOrdinaryPictureDecodes(t *testing.T) {
	body := smallPNG(t, 30, 20)
	img, format, err := imagecap.Decode(body)
	if err != nil || format != "png" || img.Bounds().Dx() != 30 || img.Bounds().Dy() != 20 {
		t.Fatalf("Decode: %v %q %v", img.Bounds(), format, err)
	}
	// From part-way into a reader: the rewind goes back to where it began,
	// not to the start of the stream.
	r := bytes.NewReader(append([]byte("junk"), body...))
	r.Seek(4, 0)
	img, _, err = imagecap.DecodeFrom(r)
	if err != nil || img.Bounds().Dx() != 30 {
		t.Fatalf("DecodeFrom: %v %v", img, err)
	}
}

// Bytes that are no picture keep image.ErrFormat, which internal/photo reads
// to tell "no decoder for this" from "a broken file".
func TestNotAPictureIsStillErrFormat(t *testing.T) {
	if _, _, err := imagecap.Decode([]byte("plain text")); !errors.Is(err, image.ErrFormat) {
		t.Fatalf("want image.ErrFormat, got %v", err)
	}
}

// Whatever the bytes, Decode either refuses or returns a picture no larger
// than the limit, and never panics.
func FuzzDecode(f *testing.F) {
	f.Add(imagecaptest.BombPNG(1_000_000, 1_000_000))
	f.Add(imagecaptest.BombPNG(20_000, 20_000))
	f.Add(smallPNG(f, 3, 2))
	f.Add([]byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"))
	f.Add([]byte("BM"))
	f.Fuzz(func(t *testing.T, data []byte) {
		img, _, err := imagecap.Decode(data)
		if err != nil {
			return
		}
		b := img.Bounds()
		if int64(b.Dx())*int64(b.Dy()) > imagecap.MaxPixels {
			t.Fatalf("decoded %v past the limit", b)
		}
	})
}
