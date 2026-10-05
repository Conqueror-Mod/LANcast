package faces

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"lancast/internal/store"
)

/*
 * The pass ends, a failure counts once, and HEIC is read from its thumbnail.
 *
 * On a real library the last 26 photographs (19 HEIC, 7 BMP) could not be read
 * by the worker's decoder. They kept no vector, the pending query handed them
 * back, and the pass sent them round for ever: the worker restarted and
 * reloaded its model each time, and the failed count passed 5,000 for a
 * library of 3,079.
 */

// stuckStore behaves like the real pending query: whatever has no vector is
// pending, every time it is asked.
type stuckStore struct {
	mu    sync.Mutex
	items []store.Item
	saved map[int64]bool
}

func (s *stuckStore) PhotosPendingEmbedding(context.Context, int64, string, int) ([]store.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Item
	for _, it := range s.items {
		if !s.saved[it.ID] {
			out = append(out, it)
		}
	}
	return out, nil
}
func (s *stuckStore) SavePhotoEmbedding(_ context.Context, id int64, _ string, _ []float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saved[id] = true
	return nil
}
func (s *stuckStore) PhotosPendingEmbeddingCount(ctx context.Context, lib int64, m string) (int, error) {
	p, _ := s.PhotosPendingEmbedding(ctx, lib, m, 0)
	return len(p), nil
}

// A worker that reads JPEGs and nothing else, like the real one with HEIC.
func jpegOnly(calls *int) func(context.Context, []string) ([]embedLine, error) {
	return func(_ context.Context, paths []string) ([]embedLine, error) {
		*calls++
		var out []embedLine
		for _, p := range paths {
			if strings.HasSuffix(p, ".jpg") {
				out = append(out, embedLine{Path: p, Vector: []float32{1, 0}})
			} else {
				out = append(out, embedLine{Path: p, Error: "unsupported image format"})
			}
		}
		return out, nil
	}
}

func readyIndexer(st EmbedStore, log *slog.Logger) *Indexer {
	ix := NewIndexer(st, &Tool{}, log)
	return ix
}

func runPass(t *testing.T, ix *Indexer) {
	t.Helper()
	// Bounded: the fault this guards against is a pass that never ends.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ix.runModel(ctx, 1, "m"); err != nil {
		t.Fatalf("the pass did not finish: %v", err)
	}
}

func TestAPhotographThatNeverEmbedsIsTriedOncePerPass(t *testing.T) {
	st := &stuckStore{saved: map[int64]bool{}, items: []store.Item{
		{ID: 1, Path: "a.jpg"}, {ID: 2, Path: "b.heic"}, {ID: 3, Path: "c.bmp"},
	}}
	calls := 0
	ix := readyIndexer(st, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	ix.embedFn = jpegOnly(&calls)
	runPass(t, ix)

	if calls != 1 {
		t.Errorf("worker started %d times, want once: the two failures were sent round again", calls)
	}
	if s := ix.Stats(); s.Failed != 2 || s.Embedded != 1 || s.Remaining != 2 {
		t.Errorf("stats = %+v, want 1 embedded, 2 failed (once each), 2 still pending", s)
	}
}

func TestAPhotographItsDecoderCannotReadIsEmbeddedFromItsThumbnail(t *testing.T) {
	st := &stuckStore{saved: map[int64]bool{}, items: []store.Item{{ID: 2, Path: "IMG_0001.heic"}}}
	calls := 0
	ix := readyIndexer(st, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	ix.embedFn = jpegOnly(&calls)
	ix.Thumbnail = func(_ context.Context, it store.Item) (string, bool) {
		return "cache/ab/abcd/original.jpg", true
	}
	runPass(t, ix)

	if !st.saved[2] {
		t.Fatal("the HEIC photograph has no vector; its thumbnail was not tried")
	}
	if s := ix.Stats(); s.Failed != 0 || s.Embedded != 1 {
		t.Errorf("stats = %+v, want 1 embedded and nothing failed", s)
	}
}

// Two photographs that are the same file share one cached thumbnail; both get
// the vector it produces.
func TestCopiesSharingAThumbnailBothGetAVector(t *testing.T) {
	st := &stuckStore{saved: map[int64]bool{}, items: []store.Item{
		{ID: 4, Path: `Mack\IMG.heic`}, {ID: 5, Path: `Misc Pics\IMG.heic`},
	}}
	calls := 0
	ix := readyIndexer(st, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	ix.embedFn = jpegOnly(&calls)
	ix.Thumbnail = func(context.Context, store.Item) (string, bool) { return "same/original.jpg", true }
	runPass(t, ix)

	if !st.saved[4] || !st.saved[5] {
		t.Errorf("saved = %v, want both copies", st.saved)
	}
}

// What could not be indexed is said once, with examples — not a line per
// photograph, and not only at debug, where it was invisible.
func TestFailuresAreReportedOnceAtTheEnd(t *testing.T) {
	var buf bytes.Buffer
	st := &stuckStore{saved: map[int64]bool{}, items: []store.Item{
		{ID: 2, Path: `C:\pics\b.heic`}, {ID: 3, Path: `C:\pics\c.bmp`},
	}}
	calls := 0
	ix := readyIndexer(st, slog.New(slog.NewTextHandler(&buf, nil)))
	ix.embedFn = jpegOnly(&calls)
	runPass(t, ix)

	out := buf.String()
	if n := strings.Count(out, "could not be indexed"); n != 1 {
		t.Fatalf("reported %d times, want once:\n%s", n, out)
	}
	if !strings.Contains(out, "count=2") || !strings.Contains(out, "unsupported image format") {
		t.Errorf("the report does not say how many or why:\n%s", out)
	}
}
