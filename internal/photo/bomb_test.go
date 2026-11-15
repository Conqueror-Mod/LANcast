package photo

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/imagecap"
	"lancast/internal/imagecap/imagecaptest"
)

// The picture-library half of R08-1: a .png whose header declares 4 TB used
// to take the photo worker, and the server with it, down with a fatal
// out-of-memory error. It is refused from its header, and not handed to
// ffmpeg, whose output would only be refused the same way.
func TestAHugeDeclaredPictureIsRefusedNotDecoded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bomb.png")
	if err := os.WriteFile(path, imagecaptest.BombPNG(1_000_000, 1_000_000), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &fakeFFmpeg{png: onePixelPNG(t)}
	d := &Decoder{FFmpeg: fake}
	_, _, err := d.Read(context.Background(), path)
	if !errors.Is(err, imagecap.ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	if errors.Is(err, ErrUnsupported) {
		t.Fatal("a refused picture is a failure to report, not an unsupported format")
	}
	if fake.called {
		t.Fatal("a picture refused for its size was offered to ffmpeg")
	}
}

// What ffmpeg hands back is a library file too, at one remove: a format Go
// does not read can still convert to a PNG of any size.
func TestAHugePictureFromFFmpegIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "photo.heic")
	if err := os.WriteFile(path, []byte("not a format Go reads"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &fakeFFmpeg{png: imagecaptest.BombPNG(1_000_000, 1_000_000)}
	d := &Decoder{FFmpeg: fake}
	if _, _, err := d.Read(context.Background(), path); !errors.Is(err, imagecap.ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

// A RAW's embedded preview whose header claims 65535 × 65535 (about 6 GB
// decoded) is not a candidate; the real preview beside it still is.
func TestARAWPreviewPastTheLimitIsSkipped(t *testing.T) {
	small := jpegOf(t, image.NewRGBA(image.Rect(0, 0, 40, 30)))
	huge := bytes.Clone(small)
	// SOF0 (FF C0): length(2) precision(1) height(2) width(2).
	at := bytes.Index(huge, []byte{0xFF, 0xC0})
	if at < 0 {
		t.Fatal("no SOF0 in the test JPEG")
	}
	binary.BigEndian.PutUint16(huge[at+5:], 0xFFFF)
	binary.BigEndian.PutUint16(huge[at+7:], 0xFFFF)

	raw := append(append([]byte("RAWHEADER"), huge...), small...)
	img, err := rawPreview(raw)
	if err != nil {
		t.Fatalf("the small preview should still be found: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 40 || b.Dy() != 30 {
		t.Fatalf("got %v, want the 40 × 30 preview", b)
	}
	if _, err := rawPreview(huge); !errors.Is(err, errNoPreview) {
		t.Fatalf("a RAW holding only the oversized preview: want errNoPreview, got %v", err)
	}
}
