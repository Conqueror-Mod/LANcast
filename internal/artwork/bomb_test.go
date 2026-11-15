package artwork

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/imagecap"
	"lancast/internal/imagecap/imagecaptest"
)

// An original that claims more pixels than imagecap allows is never stored,
// because every size is made from it by decoding it (R08-1).
func TestPutRefusesAHugeDeclaredSize(t *testing.T) {
	c := New(t.TempDir())
	_, _, _, _, err := c.Put(imagecaptest.BombPNG(1_000_000, 1_000_000))
	if !errors.Is(err, imagecap.ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

// An original cached before the limit existed is still only decoded through
// it: deriving a size refuses, Open falls back to the original as it does for
// any failed resize, and Tone, which decodes whatever Open returned, refuses
// too. Without the check each of these is a fatal out-of-memory error.
func TestAHugeOriginalAlreadyCachedIsNeverDecoded(t *testing.T) {
	c := New(t.TempDir())
	const hash = "0000000000000000000000000000000000000000000000000000000000000000"
	orig := c.pathFor(hash, SizeOriginal)
	if err := os.MkdirAll(filepath.Dir(orig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orig, imagecaptest.BombPNG(1_000_000, 1_000_000), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.derive(hash, SizeThumb); !errors.Is(err, imagecap.ErrTooLarge) {
		t.Fatalf("derive: want ErrTooLarge, got %v", err)
	}
	if _, err := c.Tone(hash); !errors.Is(err, imagecap.ErrTooLarge) {
		t.Fatalf("Tone: want ErrTooLarge, got %v", err)
	}
}
