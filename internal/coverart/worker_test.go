package coverart

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"lancast/internal/store"
)

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeStore is the worker's persistence, in memory.
type fakeStore struct {
	mu      sync.Mutex
	albums  []store.Item
	tracks  map[int64][]string
	checked map[int64]bool
	art     map[int64]string

	putArtworkErr error
	markErr       error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		tracks:  map[int64][]string{},
		checked: map[int64]bool{},
		art:     map[int64]string{},
	}
}

// PendingCoverArt models the real query: unchecked albums only. This is what
// makes the "no progress" guard meaningful — a worker that never stamps gets
// the same rows back forever.
func (f *fakeStore) PendingCoverArt(ctx context.Context, limit int) ([]store.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Item
	for _, a := range f.albums {
		if !f.checked[a.ID] {
			out = append(out, a)
		}
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeStore) PendingCoverArtCount(ctx context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, a := range f.albums {
		if !f.checked[a.ID] {
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) AlbumTrackPaths(ctx context.Context, albumID int64) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tracks[albumID], nil
}

func (f *fakeStore) MarkCoverArtChecked(ctx context.Context, itemID int64) error {
	if f.markErr != nil {
		return f.markErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checked[itemID] = true
	return nil
}

func (f *fakeStore) PutArtwork(ctx context.Context, itemID int64, hash, kind, sourceURL string, w, h int, size int64) error {
	if f.putArtworkErr != nil {
		return f.putArtworkErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.art[itemID] = hash
	return nil
}

// fakeCache stands in for the content-addressed store.
type fakeCache struct {
	mu   sync.Mutex
	put  int
	err  error
	last []byte
}

func (c *fakeCache) Put(body []byte) (string, int, int, int64, error) {
	if c.err != nil {
		return "", 0, 0, 0, c.err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.put++
	c.last = body
	return "hash-of-cover", 600, 600, int64(len(body)), nil
}

// albumWithSidecar builds an album directory with a real cover file and returns
// the album row and its track paths.
func albumWithSidecar(t *testing.T, id int64, cover []byte) (store.Item, []string) {
	t.Helper()
	dir := t.TempDir()
	track := filepath.Join(dir, "01.flac")
	if err := os.WriteFile(track, []byte("not really audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cover != nil {
		if err := os.WriteFile(filepath.Join(dir, "cover.png"), cover, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return store.Item{ID: id, Kind: "album", Title: "An Album"}, []string{track}
}

// noFFmpeg is a resolver whose extractor cannot run, so tests exercise the
// sidecar path without depending on the test machine having ffmpeg.
func noFFmpeg(t *testing.T) *Resolver {
	t.Helper()
	return NewResolver(&Extractor{Path: filepath.Join(t.TempDir(), "no-such-ffmpeg")})
}

func TestWorkerStoresAFoundCover(t *testing.T) {
	st := newFakeStore()
	album, tracks := albumWithSidecar(t, 1, onePixelPNG)
	st.albums = []store.Item{album}
	st.tracks[1] = tracks

	cache := &fakeCache{}
	w := NewWorker(st, cache, noFFmpeg(t), quietLog())
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if cache.put != 1 {
		t.Errorf("cache.Put called %d times, want 1", cache.put)
	}
	if st.art[1] != "hash-of-cover" {
		t.Errorf("artwork not recorded against the album: %v", st.art)
	}
	if s := w.Stats(); s.Found != 1 {
		t.Errorf("Found = %d, want 1", s.Found)
	}
}

// The failure this whole design turns on. An album with no cover must still be
// stamped, or the pending query returns it in every batch forever and the
// worker spins.
func TestAlbumWithNoArtIsStampedAndCounted(t *testing.T) {
	st := newFakeStore()
	album, tracks := albumWithSidecar(t, 1, nil) // no cover written
	st.albums = []store.Item{album}
	st.tracks[1] = tracks

	w := NewWorker(st, &fakeCache{}, noFFmpeg(t), quietLog())
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !st.checked[1] {
		t.Error("an artless album was not stamped; the queue would never drain")
	}
	s := w.Stats()
	if s.None != 1 {
		t.Errorf("None = %d, want 1 — finding nothing is an outcome, not a failure", s.None)
	}
	if s.Failed != 0 {
		t.Errorf("Failed = %d, want 0 — an album with no cover has not failed", s.Failed)
	}
}

// A cover the cache refuses is a real failure, and still has to stamp: an
// unstorable album retried every pass is the same infinite loop by another name.
func TestUnstorableCoverStillStamps(t *testing.T) {
	st := newFakeStore()
	album, tracks := albumWithSidecar(t, 1, onePixelPNG)
	st.albums = []store.Item{album}
	st.tracks[1] = tracks

	cache := &fakeCache{err: errors.New("disk full")}
	w := NewWorker(st, cache, noFFmpeg(t), quietLog())
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !st.checked[1] {
		t.Error("an album whose cover could not be stored was not stamped")
	}
	if s := w.Stats(); s.Failed != 1 {
		t.Errorf("Failed = %d, want 1", s.Failed)
	}
}

// If stamping itself fails, nothing progresses — and the worker must stop
// rather than re-request the same batch until the process is killed.
func TestWorkerStopsWhenNothingProgresses(t *testing.T) {
	st := newFakeStore()
	album, tracks := albumWithSidecar(t, 1, onePixelPNG)
	st.albums = []store.Item{album}
	st.tracks[1] = tracks
	st.markErr = errors.New("database is locked")

	w := NewWorker(st, &fakeCache{}, noFFmpeg(t), quietLog())
	done := make(chan error, 1)
	go func() { done <- w.Run(context.Background()) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-context.Background().Done():
	}
	// Reaching here at all is the assertion: a worker without the guard never
	// returns.
}

func TestWorkerDrainsEveryAlbum(t *testing.T) {
	st := newFakeStore()
	for id := int64(1); id <= 5; id++ {
		album, tracks := albumWithSidecar(t, id, onePixelPNG)
		st.albums = append(st.albums, album)
		st.tracks[id] = tracks
	}

	w := NewWorker(st, &fakeCache{}, noFFmpeg(t), quietLog())
	w.BatchSize = 2 // force several passes
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for id := int64(1); id <= 5; id++ {
		if !st.checked[id] {
			t.Errorf("album %d was never checked", id)
		}
	}
	if n, _ := st.PendingCoverArtCount(context.Background()); n != 0 {
		t.Errorf("%d albums still pending after a full run", n)
	}
	if s := w.Stats(); s.Running {
		t.Error("Stats still reports running after the queue drained")
	}
}

// An album cover fills the same slot in the grid a film poster does, so it is
// recorded as a poster and the client needs no new artwork kind.
func TestCoverIsRecordedAsAPoster(t *testing.T) {
	st := newFakeStore()
	album, tracks := albumWithSidecar(t, 1, onePixelPNG)
	st.albums = []store.Item{album}
	st.tracks[1] = tracks

	var gotKind, gotURL string
	recorder := &kindRecorder{fakeStore: st, kind: &gotKind, url: &gotURL}

	w := NewWorker(recorder, &fakeCache{}, noFFmpeg(t), quietLog())
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotKind != "poster" {
		t.Errorf("artwork kind = %q, want poster", gotKind)
	}
	// A local file has no URL, and inventing a file:// one would leak a server
	// path into a column the API hands to clients.
	if gotURL != "" {
		t.Errorf("source_url = %q, want empty for a local image", gotURL)
	}
}

type kindRecorder struct {
	*fakeStore
	kind *string
	url  *string
}

func (k *kindRecorder) PutArtwork(ctx context.Context, itemID int64, hash, kind, sourceURL string, w, h int, size int64) error {
	*k.kind = kind
	*k.url = sourceURL
	return k.fakeStore.PutArtwork(ctx, itemID, hash, kind, sourceURL, w, h, size)
}
