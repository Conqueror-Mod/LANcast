package photo

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"lancast/internal/store"
)

type fakeStore struct {
	mu      sync.Mutex
	pending []store.Item
	checked map[int64]bool
	art     map[int64]string
	meta    map[int64][3]int64
	putErr  error
}

func newFakeStore(items ...store.Item) *fakeStore {
	return &fakeStore{
		pending: items,
		checked: map[int64]bool{},
		art:     map[int64]string{},
		meta:    map[int64][3]int64{},
	}
}

func (f *fakeStore) PendingPhotos(_ context.Context, limit int) ([]store.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []store.Item{}
	for _, it := range f.pending {
		if !f.checked[it.ID] {
			out = append(out, it)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeStore) PendingPhotoCount(_ context.Context) (int, error) {
	items, _ := f.PendingPhotos(context.Background(), 1<<30)
	return len(items), nil
}

func (f *fakeStore) MarkArtworkChecked(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checked[id] = true
	return nil
}

func (f *fakeStore) PutArtwork(_ context.Context, id int64, hash, _, _ string, _, _ int, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putErr != nil {
		return f.putErr
	}
	f.art[id] = hash
	return nil
}

func (f *fakeStore) SetPhotoMeta(_ context.Context, id int64, w, h int, taken int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.meta[id] = [3]int64{int64(w), int64(h), taken}
	return nil
}

type fakeCache struct {
	mu   sync.Mutex
	puts [][]byte
}

func (c *fakeCache) Put(body []byte) (string, int, int, int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.puts = append(c.puts, body)
	cfg, _, err := image.DecodeConfig(newReader(body))
	if err != nil {
		return "", 0, 0, 0, err
	}
	return "hash", cfg.Width, cfg.Height, int64(len(body)), nil
}

func newReader(b []byte) *readerAt { return &readerAt{b: b} }

type readerAt struct {
	b []byte
	i int
}

func (r *readerAt) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func bigPNG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y += 8 {
		for x := 0; x < w; x += 8 {
			img.Set(x, y, color.RGBA{uint8(x % 255), uint8(y % 255), 90, 255})
		}
	}
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWorkerThumbnailsAPhoto(t *testing.T) {
	dir := t.TempDir()
	path := bigPNG(t, dir, "a.png", 60, 40)

	st := newFakeStore(store.Item{ID: 1, Kind: "photo", Path: path})
	cache := &fakeCache{}
	w := NewWorker(st, cache, &Decoder{}, quietLog())

	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if st.art[1] != "hash" {
		t.Error("no artwork recorded for the photo")
	}
	if got := st.meta[1]; got[0] != 60 || got[1] != 40 {
		t.Errorf("dimensions recorded = %dx%d, want 60x40", got[0], got[1])
	}
	if s := w.Stats(); s.Done != 1 || s.Failed != 0 {
		t.Errorf("stats = %+v, want one done and no failures", s)
	}
}

// The cache stores what it is handed and derives smaller variants from it, so a
// full-resolution photo would put a second copy of the library on disk.
func TestWorkerCachesADisplaySizedCopyNotTheOriginal(t *testing.T) {
	dir := t.TempDir()
	path := bigPNG(t, dir, "huge.png", displayMax*2, displayMax)

	st := newFakeStore(store.Item{ID: 1, Kind: "photo", Path: path})
	cache := &fakeCache{}
	w := NewWorker(st, cache, &Decoder{}, quietLog())
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(cache.puts) != 1 {
		t.Fatalf("cache received %d images, want 1", len(cache.puts))
	}
	cfg, _, err := image.DecodeConfig(newReader(cache.puts[0]))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != displayMax {
		t.Errorf("cached width = %d, want %d", cfg.Width, displayMax)
	}
	// The database still records the picture's real size, which is what a
	// viewer needs to know before it loads the file from disk.
	if got := st.meta[1]; got[0] != int64(displayMax*2) {
		t.Errorf("recorded width = %d, want the real %d", got[0], displayMax*2)
	}
}

// Every outcome must stamp. A row that fails without stamping is handed back
// forever — the shape that stalled enrichment for every music library until
// v0.6.1, and the reason the worker has a no-progress guard at all.
func TestEveryOutcomeStampsTheRow(t *testing.T) {
	dir := t.TempDir()
	good := bigPNG(t, dir, "good.png", 20, 20)

	broken := filepath.Join(dir, "broken.png")
	if err := os.WriteFile(broken, []byte("\x89PNG\r\n\x1a\n nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	heic := filepath.Join(dir, "phone.heic")
	if err := os.WriteFile(heic, []byte("not a heic"), 0o644); err != nil {
		t.Fatal(err)
	}

	st := newFakeStore(
		store.Item{ID: 1, Kind: "photo", Path: good},
		store.Item{ID: 2, Kind: "photo", Path: broken},
		store.Item{ID: 3, Kind: "photo", Path: heic},
	)
	w := NewWorker(st, &fakeCache{}, &Decoder{}, quietLog())
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, id := range []int64{1, 2, 3} {
		if !st.checked[id] {
			t.Errorf("item %d was not stamped; the queue would hand it back forever", id)
		}
	}
	s := w.Stats()
	if s.Done != 1 || s.Failed != 1 || s.Unsupported != 1 {
		t.Errorf("stats = %+v, want 1 done, 1 failed, 1 unsupported", s)
	}
	if s.Remaining != 0 {
		t.Errorf("Remaining = %d after draining the queue", s.Remaining)
	}
}

// An unreadable format and a broken file are counted apart, because they send a
// reader to different places: one is fixed by installing ffmpeg, the other by
// looking at the file.
func TestUnsupportedIsNotCountedAsFailed(t *testing.T) {
	dir := t.TempDir()
	heic := filepath.Join(dir, "phone.heic")
	if err := os.WriteFile(heic, []byte("not a heic"), 0o644); err != nil {
		t.Fatal(err)
	}

	st := newFakeStore(store.Item{ID: 1, Kind: "photo", Path: heic})
	w := NewWorker(st, &fakeCache{}, &Decoder{}, quietLog())
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if s := w.Stats(); s.Failed != 0 || s.Unsupported != 1 {
		t.Errorf("stats = %+v, want 0 failed and 1 unsupported", s)
	}
}

// Metadata is recorded even when the thumbnail cannot be stored: it came from
// the same decode pass, and a photo with no thumbnail still sorts by when it
// was taken.
func TestMetadataSurvivesAFailedThumbnail(t *testing.T) {
	dir := t.TempDir()
	path := bigPNG(t, dir, "a.png", 30, 20)

	st := newFakeStore(store.Item{ID: 1, Kind: "photo", Path: path})
	st.putErr = errors.New("disk full")
	w := NewWorker(st, &fakeCache{}, &Decoder{}, quietLog())
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := st.meta[1]; got[0] != 30 || got[1] != 20 {
		t.Errorf("dimensions = %v, want 30x20 recorded despite the failure", got)
	}
	if !st.checked[1] {
		t.Error("the row was not stamped after a failure")
	}
}

func TestFitLeavesASmallImageAlone(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 10, 10))
	if got := Fit(src, 100); got != image.Image(src) {
		t.Error("an image already within bounds must not be re-encoded")
	}
}

func TestFitPreservesAspect(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 400, 100))
	got := Fit(src, 200).Bounds()
	if got.Dx() != 200 || got.Dy() != 50 {
		t.Errorf("bounds = %dx%d, want 200x50", got.Dx(), got.Dy())
	}
}
