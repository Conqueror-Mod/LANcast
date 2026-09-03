package marker

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"lancast/internal/store"
)

/*
 * A background pass may not take the machine.
 *
 * Reported while it was happening: a film frame-lagging during playback, with
 * no transcode session logged for it at all. The ffmpeg burning **15 of 24
 * cores** was this detector, decoding somebody else's library in the
 * background while they tried to watch something.
 *
 * The worker was already limited to one file at a time, and that was not the
 * limit that mattered: one ffmpeg with a filter attached takes every core it
 * can find. Concurrency bounds how many files; only -threads bounds the cost
 * of one.
 */

func TestThreadsLeaveTheMachineUsable(t *testing.T) {
	w := NewWorker(nil, nil)
	got := w.threads()
	if got < 1 {
		t.Fatalf("threads() = %d, want at least one", got)
	}
	if cpus := runtime.NumCPU(); cpus >= 4 && got > cpus/2 {
		t.Errorf("threads() = %d of %d cores — a pass nothing waits on must leave room",
			got, cpus)
	}
}

func TestThreadsCanBeSetExplicitly(t *testing.T) {
	w := NewWorker(nil, nil)
	w.Threads = 2
	if got := w.threads(); got != 2 {
		t.Errorf("threads() = %d, want the configured 2", got)
	}
}

/*
 * Turning it off stops it now, not at the end of a batch.
 *
 * A pass is twenty-five films. The setting used to be read once, when the pass
 * started, so switching it off left the machine loaded for the rest of the
 * batch — which from outside is indistinguishable from a switch that does
 * nothing, and was reported that way.
 */
func TestAPassStopsWhenTheSettingGoesOff(t *testing.T) {
	w := NewWorker(nil, nil)
	if !w.stillWanted() {
		t.Error("with no Enabled hook the pass must run")
	}

	on := true
	w.Enabled = func() bool { return on }
	if !w.stillWanted() {
		t.Error("stillWanted() = false while the setting is on")
	}
	on = false
	if w.stillWanted() {
		t.Error("stillWanted() = true after the setting went off — " +
			"the check must be live, not captured when the pass began")
	}
}

/*
 * A file that cannot be read must stop being asked about.
 *
 * Two damaged films in a real library — one with an unreadable EBML header,
 * one with no moov atom — were re-decoded on every pass for ever, logging the
 * same warning each time. Nothing was stamped on failure, so that an unmounted
 * drive could not retire a library permanently: right about the drive, wrong
 * about the file.
 */

type stubStore struct {
	saved   map[int64][]store.Marker
	pending []store.Item
}

func (s *stubStore) PendingMarkers(context.Context, int) ([]store.Item, error) {
	return s.pending, nil
}
func (s *stubStore) PendingMarkersCount(context.Context) (int, error) { return 0, nil }
func (s *stubStore) SaveMarkers(_ context.Context, id int64, _ []string, m []store.Marker) error {
	if s.saved == nil {
		s.saved = map[int64][]store.Marker{}
	}
	s.saved[id] = m
	return nil
}

func failingWorker(t *testing.T, path string) (*Worker, *stubStore) {
	t.Helper()
	st := &stubStore{}
	w := NewWorker(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// A binary that cannot exist, so every scan fails the way a damaged file
	// makes ffmpeg fail: the point under test is what happens next.
	w.FFmpegPath = filepath.Join(t.TempDir(), "no-such-ffmpeg.exe")
	return w, st
}

func TestAPresentButUnreadableFileIsStampedOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "damaged.mkv")
	if err := os.WriteFile(path, []byte("not really a matroska file"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, st := failingWorker(t, path)

	dur := int64(6_000_000)
	w.examine(context.Background(), store.Item{ID: 42, Path: path, DurationMS: &dur})

	got, ok := st.saved[42]
	if !ok {
		t.Fatal("nothing was stamped — the file is present and will never be readable")
	}
	if len(got) != 0 {
		t.Errorf("markers = %+v, want none: it was stamped as looked at, not as having credits", got)
	}
}

func TestAnUnreachableFileIsNotStamped(t *testing.T) {
	// No file at that path at all: an unmounted drive, not a damaged file.
	w, st := failingWorker(t, "")
	dur := int64(6_000_000)
	w.examine(context.Background(),
		store.Item{ID: 43, Path: filepath.Join(t.TempDir(), "gone", "film.mkv"), DurationMS: &dur})

	if _, ok := st.saved[43]; ok {
		t.Error("an unreachable file was stamped — a drive that is away must be asked again")
	}
}
