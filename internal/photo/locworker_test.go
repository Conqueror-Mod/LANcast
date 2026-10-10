package photo

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"lancast/internal/geo"
	"lancast/internal/store"
)

type fakeLocStore struct {
	mu      sync.Mutex
	pending []store.Item
	written map[int64]*store.PhotoPlace
	at      map[int64]*store.LatLon
	forgot  int
	// onRecord runs inside RecordPhotoLocation, for tests that change the
	// world mid-pass.
	onRecord func()
}

func (f *fakeLocStore) PendingLocations(ctx context.Context, limit int) ([]store.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Item
	for _, it := range f.pending {
		if _, done := f.written[it.ID]; !done && len(out) < limit {
			out = append(out, it)
		}
	}
	return out, nil
}

func (f *fakeLocStore) PendingLocationCount(ctx context.Context) (int, error) {
	p, _ := f.PendingLocations(ctx, 1<<30)
	return len(p), nil
}

func (f *fakeLocStore) RecordPhotoLocation(ctx context.Context, id int64, at *store.LatLon, p *store.PhotoPlace) error {
	f.mu.Lock()
	f.written[id] = p
	f.at[id] = at
	f.mu.Unlock()
	if f.onRecord != nil {
		f.onRecord()
	}
	return nil
}

func (f *fakeLocStore) ForgetPhotoLocations(ctx context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.written)
	f.written = map[int64]*store.PhotoPlace{}
	f.at = map[int64]*store.LatLon{}
	f.forgot++
	return int64(n), nil
}

func newFakeLocStore(paths ...string) *fakeLocStore {
	f := &fakeLocStore{written: map[int64]*store.PhotoPlace{}, at: map[int64]*store.LatLon{}}
	for i, p := range paths {
		f.pending = append(f.pending, store.Item{ID: int64(i + 1), Path: p})
	}
	return f
}

// A gazetteer of one town at the origin's neighbour, so tests never
// decompress the real tables.
func oneTown() (*geo.Gazetteer, error) { return geo.Synthetic("Testville", 10, 10, 5000), nil }

func testWorker(st *fakeLocStore, enabled *atomic.Bool, read func(string) (Location, bool)) *LocationWorker {
	w := NewLocationWorker(st, enabled.Load, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.load = oneTown
	w.read = read
	return w
}

func readTable(t map[string]Location) func(string) (Location, bool) {
	return func(p string) (Location, bool) {
		l, ok := t[p]
		return l, ok
	}
}

func TestOffReadsNothing(t *testing.T) {
	st := newFakeLocStore("a.jpg")
	var on atomic.Bool
	reads := 0
	w := testWorker(st, &on, func(string) (Location, bool) { reads++; return Location{}, false })
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reads != 0 || len(st.written) != 0 {
		t.Errorf("with the setting off: %d reads, %d rows", reads, len(st.written))
	}
}

func TestEveryPhotoIsStampedAndPlaced(t *testing.T) {
	st := newFakeLocStore("town.jpg", "sea.jpg", "none.jpg")
	var on atomic.Bool
	on.Store(true)
	w := testWorker(st, &on, readTable(map[string]Location{
		"town.jpg": {Lat: 10.01, Lon: 10.01},
		"sea.jpg":  {Lat: -40, Lon: -140},
	}))
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(st.written) != 3 {
		t.Fatalf("stamped %d of 3; a photo that says nothing is still read", len(st.written))
	}
	if p := st.written[1]; p == nil || p.Name != "Testville" {
		t.Errorf("town.jpg filed under %v, want Testville", p)
	}
	if st.at[2] == nil || st.written[2] != nil {
		t.Errorf("sea.jpg: position %v place %v, want a position and no place", st.at[2], st.written[2])
	}
	if st.at[3] != nil || st.written[3] != nil {
		t.Error("none.jpg recorded a position it does not carry")
	}
	if s := w.Stats(); s.Done != 3 || s.Located != 2 || s.Running || s.FinishedAt == 0 {
		t.Errorf("stats = %+v", s)
	}
}

// Turning the setting off stops the pass at the next photograph, not at the
// end of the library.
func TestTurningItOffStopsThePass(t *testing.T) {
	st := newFakeLocStore("1.jpg", "2.jpg", "3.jpg", "4.jpg")
	var on atomic.Bool
	on.Store(true)
	w := testWorker(st, &on, readTable(nil))
	st.onRecord = func() { on.Store(false) }
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(st.written) != 1 {
		t.Errorf("read %d photos after being turned off at the first", len(st.written))
	}
}

// Forget waits for a pass in flight, so a deletion is never followed by the
// pass writing the next row straight back.
func TestForgetWaitsForThePassInFlight(t *testing.T) {
	st := newFakeLocStore("1.jpg", "2.jpg", "3.jpg")
	var on atomic.Bool
	on.Store(true)
	w := testWorker(st, &on, readTable(nil))

	recorded := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	st.onRecord = func() {
		once.Do(func() {
			close(recorded)
			<-release
		})
	}
	done := make(chan struct{})
	go func() { _ = w.Run(context.Background()); close(done) }()
	<-recorded

	// The order the settings handler uses: save the setting, then forget.
	on.Store(false)
	forgot := make(chan struct{})
	go func() { _, _ = w.Forget(context.Background()); close(forgot) }()
	close(release)
	<-done
	<-forgot

	if len(st.written) != 0 || st.forgot != 1 {
		t.Errorf("after forgetting: %d rows remain, forgot %d times", len(st.written), st.forgot)
	}
}
