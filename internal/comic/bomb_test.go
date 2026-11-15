package comic_test

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/comic"
	"lancast/internal/imagecap"
	"lancast/internal/imagecap/imagecaptest"
	"lancast/internal/store"
)

// writeBombCBZ is the file from R08-1: a .cbz of about 200 bytes whose one
// page is a PNG declaring 1,000,000 × 1,000,000 RGBA pixels. Stored, not
// deflated, so the archive is as small as the finding's.
func writeBombCBZ(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "1.png", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(imagecaptest.BombPNG(1_000_000, 1_000_000)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// Decoding that page asked the runtime for 4 TB, which is a fatal error, not a
// panic: the server died, the defer that stamps the row never ran, and the
// comic was pending again at the next start. Now its header is refused, and
// the row is stamped like any other cover that cannot be made.
func TestAComicWhosePageDeclaresTerabytesIsRefusedAndStamped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bomb.cbz")
	writeBombCBZ(t, path)
	if fi, err := os.Stat(path); err != nil || fi.Size() > 300 {
		t.Fatalf("the archive should be a couple of hundred bytes: %v %v", fi.Size(), err)
	}

	if _, err := comic.Cover(path); !errors.Is(err, imagecap.ErrTooLarge) {
		t.Fatalf("Cover: want ErrTooLarge, got %v", err)
	}

	st := &fakeStore{
		pending: []store.Item{{ID: 7, Path: path}},
		posters: map[int64]string{}, checked: map[int64]bool{},
	}
	cache := &fakeCache{bodies: map[string][]byte{}}
	w := comic.NewCoverWorker(st, cache, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !st.checked[7] {
		t.Fatal("the refused comic must be stamped, or it is retried at every start")
	}
	if _, ok := st.posters[7]; ok {
		t.Fatal("a refused comic got a poster")
	}
}
