package media

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRAWIsAPicture(t *testing.T) {
	for name, want := range map[string]bool{
		"a.cr2": true, "a.CR3": true, "a.nef": true, "a.arw": true, "a.dng": true,
		"a.raf": true, "a.orf": true, "a.rw2": true, "a.jpg": false, "a.mkv": false,
	} {
		if IsRAW(name) != want {
			t.Errorf("IsRAW(%s) = %v", name, !want)
		}
		if want && !IsScannable(name, LibraryPicture) {
			t.Errorf("%s is not scanned in a picture library", name)
		}
	}
}

// A RAW with a JPEG or HEIC of the same name beside it is one photograph shot
// twice; a RAW alone is a photograph.
func TestARAWWithAJPEGBesideItIsATwin(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"pair.CR2", "pair.JPG", "phone.dng", "phone.heic", "alone.nef", "notraw.jpg"} {
		os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644)
	}
	for name, want := range map[string]bool{
		"pair.CR2": true, "phone.dng": true, "alone.nef": false, "notraw.jpg": false,
	} {
		if got := HasDevelopedTwin(filepath.Join(dir, name)); got != want {
			t.Errorf("%s: twin = %v, want %v", name, got, want)
		}
	}
}
