package report

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"
	"testing"
)

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A screenshot stays a PNG, so its text stays sharp, and keeps its size.
func TestAScreenshotStaysAPNG(t *testing.T) {
	img, err := PrepareImage(pngOf(t, 320, 200), 1)
	if err != nil {
		t.Fatal(err)
	}
	if img.Name != "picture-1.png" {
		t.Errorf("name %q", img.Name)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(img.Data))
	if err != nil || format != "png" || cfg.Width != 320 || cfg.Height != 200 {
		t.Errorf("got %s %dx%d, %v", format, cfg.Width, cfg.Height, err)
	}
}

// A picture wider than 2560 pixels is brought down to it, in proportion.
func TestALargePictureIsShrunk(t *testing.T) {
	img, err := PrepareImage(pngOf(t, 5120, 400), 2)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, _ := image.DecodeConfig(bytes.NewReader(img.Data))
	if cfg.Width != 2560 || cfg.Height != 200 {
		t.Errorf("got %dx%d", cfg.Width, cfg.Height)
	}
}

/*
 * The reason every picture is encoded again: a phone photograph says where it
 * was taken, in an EXIF block, and that must not go to the developer with a
 * bug report.
 */
func TestAPhotosMetadataDoesNotGo(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 64, 48)), nil); err != nil {
		t.Fatal(err)
	}
	plain := buf.Bytes()
	marker := []byte("Exif\x00\x00GPSLatitude 51.5074 N")
	app1 := append([]byte{0xFF, 0xE1, 0, 0}, marker...)
	binary.BigEndian.PutUint16(app1[2:], uint16(len(marker)+2))
	photo := append(append(append([]byte{}, plain[:2]...), app1...), plain[2:]...)
	if !bytes.Contains(photo, []byte("GPSLatitude")) {
		t.Fatal("the test photo carries no location")
	}

	img, err := PrepareImage(photo, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(img.Name, ".jpg") {
		t.Errorf("a photograph became %q", img.Name)
	}
	if bytes.Contains(img.Data, []byte("GPS")) || bytes.Contains(img.Data, []byte("Exif")) {
		t.Error("the photo's metadata went with it")
	}
}

func TestAFileThatIsNotAPictureIsRefused(t *testing.T) {
	_, err := PrepareImage([]byte("<html><script>alert(1)</script>"), 1)
	if !errors.Is(err, ErrNotAnImage) {
		t.Errorf("err %v", err)
	}
}

// A few bytes claiming an enormous canvas are refused before anything is
// allocated for it.
func TestAPictureClaimingAHugeCanvasIsRefused(t *testing.T) {
	data := pngOf(t, 1, 1)
	// IHDR follows the 8-byte signature: length, type, width, height, …, CRC.
	binary.BigEndian.PutUint32(data[16:], 100_000)
	binary.BigEndian.PutUint32(data[20:], 100_000)
	binary.BigEndian.PutUint32(data[29:], crc32.ChecksumIEEE(data[12:29]))
	_, err := PrepareImage(data, 1)
	if !errors.Is(err, ErrNotAnImage) || !strings.Contains(err.Error(), "too large") {
		t.Errorf("err %v", err)
	}
}

// Pictures ride as files of their own, and the summary says how many.
func TestPicturesAreAttached(t *testing.T) {
	srv, got := fakeWebhook(t, http.StatusNoContent)
	a, _ := PrepareImage(pngOf(t, 8, 8), 1)
	b, _ := PrepareImage(pngOf(t, 8, 8), 2)
	err := Send(context.Background(), srv.Client(), srv.URL, Report{What: "Black screen", Images: []Image{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	r := (*got)[0]
	if r.files["files[0]:picture-1.png"] != string(a.Data) || r.files["files[1]:picture-2.png"] != string(b.Data) {
		t.Errorf("files: %d", len(r.files))
	}
	if c := r.payload["content"].(string); !strings.Contains(c, "· 2 pictures") {
		t.Errorf("content: %s", c)
	}
}
