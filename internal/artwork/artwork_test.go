package artwork

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// makeJPEG builds a real image so decoding is genuinely exercised.
func makeJPEG(t *testing.T, w, h int, shade uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: shade, G: uint8(x % 256), B: uint8(y % 256), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPutAndOpenOriginal(t *testing.T) {
	c := New(t.TempDir())
	body := makeJPEG(t, 800, 1200, 200)

	hash, w, h, size, err := c.Put(body)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if len(hash) != 64 {
		t.Errorf("hash = %q, want a 64-char sha256", hash)
	}
	if w != 800 || h != 1200 {
		t.Errorf("dimensions = %dx%d, want 800x1200", w, h)
	}
	if size != int64(len(body)) {
		t.Errorf("size = %d, want %d", size, len(body))
	}

	f, err := c.Open(hash, SizeOriginal)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()
	got, _ := os.ReadFile(f.Name())
	if !bytes.Equal(got, body) {
		t.Error("stored original does not match the input bytes")
	}
}

// Content addressing: the same bytes stored twice are one file.
func TestPutIsContentAddressed(t *testing.T) {
	c := New(t.TempDir())
	body := makeJPEG(t, 100, 150, 10)

	h1, _, _, _, err := c.Put(body)
	if err != nil {
		t.Fatal(err)
	}
	h2, _, _, _, err := c.Put(body)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("identical bytes produced different hashes: %s vs %s", h1, h2)
	}

	different, _, _, _, _ := c.Put(makeJPEG(t, 100, 150, 250))
	if different == h1 {
		t.Error("different images produced the same hash")
	}
}

func TestPutRejectsNonImage(t *testing.T) {
	c := New(t.TempDir())
	if _, _, _, _, err := c.Put([]byte("this is not an image")); err == nil {
		t.Error("Put accepted non-image bytes")
	}
}

func TestPutAcceptsPNG(t *testing.T) {
	c := New(t.TempDir())
	if _, w, h, _, err := c.Put(makePNG(t, 120, 80)); err != nil || w != 120 || h != 80 {
		t.Errorf("PNG put = %dx%d, %v", w, h, err)
	}
}

func TestDerivedSizesAreGeneratedOnDemand(t *testing.T) {
	c := New(t.TempDir())
	hash, _, _, _, _ := c.Put(makeJPEG(t, 1000, 1500, 120))

	for _, size := range []Size{SizeThumb, SizePoster, SizePoster2x} {
		f, err := c.Open(hash, size)
		if err != nil {
			t.Fatalf("Open(%s): %v", size, err)
		}
		cfg, _, err := image.DecodeConfig(f)
		f.Close()
		if err != nil {
			t.Fatalf("decode %s: %v", size, err)
		}
		if cfg.Width != widths[size] {
			t.Errorf("%s width = %d, want %d", size, cfg.Width, widths[size])
		}
		// Aspect ratio must survive the resize.
		wantH := 1500 * widths[size] / 1000
		if cfg.Height < wantH-2 || cfg.Height > wantH+2 {
			t.Errorf("%s height = %d, want about %d", size, cfg.Height, wantH)
		}
	}
}

// Upscaling costs bytes and looks worse than the original.
func TestSmallImagesAreNotUpscaled(t *testing.T) {
	c := New(t.TempDir())
	hash, _, _, _, _ := c.Put(makeJPEG(t, 100, 150, 90))

	f, err := c.Open(hash, SizeFanart) // wants 1280 wide
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width > 100 {
		t.Errorf("width = %d, want no upscaling beyond the original 100", cfg.Width)
	}
}

func TestDerivedSizeIsCached(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	hash, _, _, _, _ := c.Put(makeJPEG(t, 600, 900, 60))

	f, _ := c.Open(hash, SizePoster)
	f.Close()

	target := filepath.Join(dir, hash[:2], hash, "poster.jpg")
	first, err := os.Stat(target)
	if err != nil {
		t.Fatalf("derived file was not cached: %v", err)
	}

	f, _ = c.Open(hash, SizePoster)
	f.Close()
	second, _ := os.Stat(target)
	if !first.ModTime().Equal(second.ModTime()) {
		t.Error("the derived size was regenerated instead of served from cache")
	}
}

// The cache must be fully rebuildable: delete it and it heals.
func TestCacheIsRebuildable(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	body := makeJPEG(t, 400, 600, 30)
	hash, _, _, _, _ := c.Put(body)

	// Close before wiping: Windows refuses to unlink an open file, so a leaked
	// handle here fails the RemoveAll rather than the behavior under test.
	f, err := c.Open(hash, SizePoster)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Open(hash, SizePoster); !os.IsNotExist(err) {
		t.Errorf("Open after cache wipe = %v, want ErrNotExist", err)
	}
	if c.Stored(hash) {
		t.Error("Stored reports true after the cache was wiped")
	}

	// Re-storing the same bytes restores the same identity.
	again, _, _, _, err := c.Put(body)
	if err != nil {
		t.Fatal(err)
	}
	if again != hash {
		t.Errorf("rebuilt hash = %s, want the original %s", again, hash)
	}
	healed, err := c.Open(hash, SizePoster)
	if err != nil {
		t.Errorf("cache did not heal: %v", err)
		return
	}
	healed.Close()
}

func TestOpenUnknownHash(t *testing.T) {
	c := New(t.TempDir())
	valid := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if _, err := c.Open(valid, SizePoster); !os.IsNotExist(err) {
		t.Errorf("unknown hash = %v, want ErrNotExist", err)
	}
}

// A hash comes from the URL path, so it must be validated before it becomes a
// filesystem path.
func TestOpenRejectsMalformedHash(t *testing.T) {
	c := New(t.TempDir())
	for _, bad := range []string{
		"", "short", "../../../etc/passwd",
		"ZZZZ56789abcdef0123456789abcdef0123456789abcdef0123456789abcdef01",
		"../0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab",
	} {
		if _, err := c.Open(bad, SizePoster); !os.IsNotExist(err) {
			t.Errorf("Open(%q) = %v, want ErrNotExist", bad, err)
		}
	}
}

func TestInvalidSizeFallsBackToPoster(t *testing.T) {
	c := New(t.TempDir())
	hash, _, _, _, _ := c.Put(makeJPEG(t, 600, 900, 40))

	f, err := c.Open(hash, Size("enormous"))
	if err != nil {
		t.Fatalf("Open with unknown size: %v", err)
	}
	defer f.Close()
	cfg, _, _ := image.DecodeConfig(f)
	if cfg.Width != widths[SizePoster] {
		t.Errorf("width = %d, want the poster fallback %d", cfg.Width, widths[SizePoster])
	}
}

func TestValidSize(t *testing.T) {
	for _, s := range []Size{SizeThumb, SizePoster, SizePoster2x, SizeFanart, SizeOriginal} {
		if !ValidSize(s) {
			t.Errorf("ValidSize(%s) = false", s)
		}
	}
	if ValidSize("nope") {
		t.Error("ValidSize accepted an unknown size")
	}
}

func TestDownload(t *testing.T) {
	body := makeJPEG(t, 500, 750, 77)
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write(body)
	}))
	defer srv.Close()

	c := New(t.TempDir())
	c.http = srv.Client()

	hash, w, h, size, err := c.Download(context.Background(), srv.URL+"/poster.jpg")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if w != 500 || h != 750 {
		t.Errorf("dimensions = %dx%d, want 500x750", w, h)
	}
	if size != int64(len(body)) {
		t.Errorf("size = %d, want %d", size, len(body))
	}
	if !c.Stored(hash) {
		t.Error("Stored = false after a successful download")
	}
}

func TestDownloadHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(t.TempDir())
	c.http = srv.Client()
	if _, _, _, _, err := c.Download(context.Background(), srv.URL+"/missing.jpg"); err == nil {
		t.Error("Download accepted a 404")
	}
}

// Concurrent requests for the same missing size must not both decode and
// encode it, and must not corrupt the cached file.
func TestConcurrentDeriveIsSafe(t *testing.T) {
	c := New(t.TempDir())
	hash, _, _, _, _ := c.Put(makeJPEG(t, 900, 1350, 55))

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, err := c.Open(hash, SizePoster)
			if err != nil {
				errs <- err
				return
			}
			defer f.Close()
			if _, _, err := image.DecodeConfig(f); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Open: %v", err)
	}
}

func TestNoTempFilesLeftBehind(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	hash, _, _, _, _ := c.Put(makeJPEG(t, 700, 1050, 15))
	f, _ := c.Open(hash, SizePoster)
	f.Close()

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && len(info.Name()) > 5 && info.Name()[:5] == ".tmp-" {
			t.Errorf("temp file left behind: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
