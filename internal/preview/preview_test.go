package preview

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

type fakeStore struct {
	items []store.Item
	keys  map[int64]string
	exist map[int64]bool
}

func (f *fakeStore) PendingPreviews(_ context.Context, rev, _ int) ([]store.Item, error) {
	var out []store.Item
	for _, it := range f.items {
		if f.keys[it.ID] != store.PreviewKey(it, rev) {
			out = append(out, it)
		}
	}
	return out, nil
}

func (f *fakeStore) SetPreviewKey(_ context.Context, id int64, key string) error {
	f.keys[id] = key
	return nil
}

func (f *fakeStore) ExistingItems(_ context.Context, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	for _, id := range ids {
		out[id] = f.exist[id]
	}
	return out, nil
}

func item(id int64, path string) store.Item {
	size, mtime := int64(100), int64(200)
	return store.Item{ID: id, Path: path, SizeBytes: &size, MTime: &mtime}
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// The real thing, on a generated film: a sheet per 1,000 seconds, frames
// 240 wide, a manifest keyed to the file. Skipped without ffmpeg.
func TestPreviewsOfARealFilm(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	film := filepath.Join(dir, "film.mkv")
	// 1,050 seconds at a small size, a keyframe every second: two sheets,
	// the second holding the last five frames.
	cmd := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc=size=320x180:rate=2:duration=1050",
		"-c:v", "mpeg4", "-g", "2", "-y", film)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make fixture: %v %s", err, out)
	}
	st := &fakeStore{items: []store.Item{item(7, film)}, keys: map[int64]string{}, exist: map[int64]bool{7: true}}
	root := filepath.Join(dir, "previews")
	w := NewWorker(st, root, quiet())
	w.FFmpegPath = ffmpeg
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	m, err := Load(root, 7)
	if err != nil {
		t.Fatal(err)
	}
	if m.Sheets != 2 || m.Width != Width || m.Height != 134 && m.Height != 136 || m.Columns != 10 || m.Rows != 10 {
		t.Fatalf("manifest %+v", m)
	}
	if m.Key != store.PreviewKey(st.items[0], Rev) || st.keys[7] != m.Key {
		t.Fatalf("key %q, recorded %q", m.Key, st.keys[7])
	}
	if _, err := os.Stat(SheetPath(root, 7, 1)); err != nil {
		t.Fatalf("second sheet: %v", err)
	}
	// Nothing left behind by the pass.
	if _, err := os.Stat(filepath.Join(root, ".tmp-7")); !os.IsNotExist(err) {
		t.Fatalf("a temporary folder survived: %v", err)
	}
}

// A file ffmpeg cannot read gets no previews and is not tried again until it
// changes: its key is recorded anyway.
func TestAFailureIsRecordedNotRetried(t *testing.T) {
	root := t.TempDir()
	st := &fakeStore{items: []store.Item{item(3, "nowhere.mkv")}, keys: map[int64]string{}, exist: map[int64]bool{3: true}}
	w := NewWorker(st, root, quiet())
	calls := 0
	w.makeFn = func(context.Context, store.Item, string) (*Manifest, error) {
		calls++
		return nil, errors.New("ffmpeg could not read it")
	}
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || st.keys[3] == "" {
		t.Fatalf("calls %d, key %q", calls, st.keys[3])
	}
	if _, err := Load(root, 3); err == nil {
		t.Fatal("a failed item has a manifest")
	}
}

// Off means off before the next file, not at the end of a batch.
func TestTurningItOffStopsBeforeTheNextFile(t *testing.T) {
	st := &fakeStore{items: []store.Item{item(1, "a"), item(2, "b"), item(3, "c")}, keys: map[int64]string{}, exist: map[int64]bool{}}
	w := NewWorker(st, t.TempDir(), quiet())
	on := true
	w.Enabled = func() bool { return on }
	made := 0
	w.makeFn = func(_ context.Context, _ store.Item, dir string) (*Manifest, error) {
		made++
		on = false // switched off while the first file was being made
		os.MkdirAll(dir, 0o755)
		return &Manifest{Sheets: 1}, nil
	}
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if made != 1 {
		t.Fatalf("made %d after being switched off", made)
	}
}

// Previews of an item that no longer exists, and a pass's leftovers, go.
func TestPruningRemovesWhatHasNoItem(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"5", "6", ".tmp-9"} {
		os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	st := &fakeStore{keys: map[int64]string{}, exist: map[int64]bool{5: true}}
	w := NewWorker(st, root, quiet())
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for d, want := range map[string]bool{"5": true, "6": false, ".tmp-9": false} {
		_, err := os.Stat(filepath.Join(root, d))
		if (err == nil) != want {
			t.Errorf("%s: exists=%v, want %v", d, err == nil, want)
		}
	}
}

// "ffmpeg exited cleanly" is not trusted: no sheets is a failure.
func TestNoSheetsIsAFailure(t *testing.T) {
	if _, err := Describe(t.TempDir()); err == nil {
		t.Fatal("an empty folder described as previews")
	}
}
