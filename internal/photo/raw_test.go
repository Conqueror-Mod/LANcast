package photo

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/tiff"
)

func solid(w, h int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func jpegOf(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

/*
 * fakeRAW lays out what a real RAW holds, every trap included: a TIFF the
 * standard decoder can read (a NEF's 160 × 120 thumbnail), a small EXIF JPEG,
 * a JPEG whose header claims the largest size but whose data is cut short,
 * sensor data stored as lossless JPEG (SOF3), and the real preview.
 */
func fakeRAW(t *testing.T) []byte {
	t.Helper()
	var f bytes.Buffer
	if err := tiff.Encode(&f, solid(160, 120, color.RGBA{255, 0, 0, 255}), nil); err != nil {
		t.Fatal(err)
	}
	f.Write(make([]byte, 333))
	f.Write(jpegOf(t, solid(160, 120, color.RGBA{0, 0, 255, 255}))) // EXIF thumbnail
	f.Write(make([]byte, 517))
	cut := jpegOf(t, solid(3000, 2000, color.RGBA{0, 255, 0, 255}))
	f.Write(cut[:len(cut)/3]) // claims 3000 × 2000, then stops
	f.Write(make([]byte, 101))
	// Lossless JPEG (SOF3): SOI, then a SOF3 header for 6000 × 4000.
	f.Write([]byte{0xFF, 0xD8, 0xFF, 0xC3, 0x00, 0x0B, 0x0C, 0x0F, 0xA0, 0x17, 0x70, 0x01, 0x01, 0x11, 0x00})
	f.Write(make([]byte, 2048))
	f.Write(jpegOf(t, solid(1200, 800, color.RGBA{250, 250, 250, 255}))) // the preview
	f.Write(make([]byte, 999))
	return f.Bytes()
}

func TestARAWShowsItsLargestRealPreview(t *testing.T) {
	img, err := rawPreview(fakeRAW(t))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 1200 || b.Dy() != 800 {
		t.Fatalf("picked a %d × %d picture, want the 1200 × 800 preview", b.Dx(), b.Dy())
	}
}

// Through the decoder the photo worker uses: by extension, never the TIFF
// thumbnail the standard decoder would have returned.
func TestTheDecoderReadsARAWsPreview(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"DSC_0001.NEF", "IMG_0001.cr2", "DSCF0001.raf"} {
		path := filepath.Join(dir, name)
		os.WriteFile(path, fakeRAW(t), 0o644)
		var d Decoder
		img, meta, err := d.Read(context.Background(), path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if meta.Width != 1200 || meta.Height != 800 || img.Bounds().Dx() != 1200 {
			t.Fatalf("%s: %d × %d", name, meta.Width, meta.Height)
		}
		if meta.SHA256 == "" {
			t.Fatalf("%s: no digest", name)
		}
	}
}

// A RAW with no picture in it says so, rather than showing a thumbnail or
// pretending to be broken in some other way.
func TestARAWWithNoPreviewSaysSo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.dng")
	var f bytes.Buffer
	tiff.Encode(&f, solid(64, 48, color.Black), nil)
	os.WriteFile(path, f.Bytes(), 0o644)
	var d Decoder
	if _, _, err := d.Read(context.Background(), path); err == nil {
		t.Fatal("a RAW with only a TIFF thumbnail decoded")
	}
}
