package photo

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

// Orientation reads the tag without decoding the picture, and says zero for
// anything it cannot answer rather than guessing.
func TestOrientationReadsTheTag(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, body []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	for _, o := range []int{1, 3, 6, 8} {
		p := write("o.jpg", jpegWithOrientation(t, 20, 10, o))
		if got := Orientation(p); got != o {
			t.Errorf("Orientation = %d, want %d", got, o)
		}
	}
	var plain bytes.Buffer
	if err := jpeg.Encode(&plain, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	if got := Orientation(write("plain.jpg", plain.Bytes())); got != 0 {
		t.Errorf("no EXIF: Orientation = %d, want 0", got)
	}
	if got := Orientation(write("junk.heic", []byte("not a picture"))); got != 0 {
		t.Errorf("unreadable: Orientation = %d, want 0", got)
	}
	if got := Orientation(filepath.Join(dir, "missing.jpg")); got != 0 {
		t.Errorf("missing: Orientation = %d, want 0", got)
	}
}
