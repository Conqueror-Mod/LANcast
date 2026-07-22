package probe

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

func workerHarness(t *testing.T) (*store.Store, *store.Library) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	lib, err := st.CreateLibrary(context.Background(), "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st, lib
}

func addItem(t *testing.T, st *store.Store, lib *store.Library, path string) int64 {
	t.Helper()
	if _, err := st.UpsertItem(context.Background(), store.ScanFile{
		LibraryID: lib.ID, Path: path, Kind: "movie",
		Title: filepath.Base(path), SortTitle: filepath.Base(path),
		Container: "mkv", SizeBytes: 1, MTime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	known, _ := st.KnownFiles(context.Background(), lib.ID)
	return known[path].ID
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// LANcast works without ffmpeg — it just cannot make informed playback
// decisions. That must be a clean no-op, not a failure.
func TestWorkerWithoutFFprobe(t *testing.T) {
	st, lib := workerHarness(t)
	addItem(t, st, lib, `C:\m\a.mkv`)

	w := NewWorker(st, &Prober{Path: "definitely-not-a-real-binary"}, quiet())
	// An explicit path is not verified up front, so this exercises the failure
	// path per item rather than the availability short-circuit.
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Every item failed, but all were stamped so the queue drains.
	if n, _ := st.PendingProbeCount(context.Background()); n != 0 {
		t.Errorf("PendingProbeCount = %d, want 0 — failures must not requeue forever", n)
	}
	if s := w.Stats(); s.Failed == 0 {
		t.Error("failures were not counted")
	}
}

// The queue is a query rather than a cursor, so a batch that stamps nothing
// would return identical rows forever.
func TestWorkerTerminatesOnRepeatedFailure(t *testing.T) {
	st, lib := workerHarness(t)
	for i := 0; i < 5; i++ {
		addItem(t, st, lib, `C:\m\`+string(rune('a'+i))+`.mkv`)
	}

	w := NewWorker(st, &Prober{Path: "definitely-not-a-real-binary"}, quiet())
	w.BatchSize = 2

	done := make(chan error, 1)
	go func() { done <- w.Run(context.Background()) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-context.Background().Done():
	}

	if n, _ := st.PendingProbeCount(context.Background()); n != 0 {
		t.Errorf("PendingProbeCount = %d, want the queue drained", n)
	}
}

func TestWorkerStats(t *testing.T) {
	st, lib := workerHarness(t)
	addItem(t, st, lib, `C:\m\a.mkv`)
	addItem(t, st, lib, `C:\m\b.mkv`)

	w := NewWorker(st, &Prober{Path: "definitely-not-a-real-binary"}, quiet())
	w.Run(context.Background())

	s := w.Stats()
	if s.Running {
		t.Error("Running is true after Run returned")
	}
	if s.Total != 2 {
		t.Errorf("Total = %d, want 2", s.Total)
	}
	if s.Remaining != 0 {
		t.Errorf("Remaining = %d, want 0", s.Remaining)
	}
}

func TestWorkerCancelled(t *testing.T) {
	st, lib := workerHarness(t)
	addItem(t, st, lib, `C:\m\a.mkv`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	w := NewWorker(st, &Prober{Path: "definitely-not-a-real-binary"}, quiet())
	if err := w.Run(ctx); err == nil {
		t.Error("Run ignored a cancelled context")
	}
}

func TestResultFromItem(t *testing.T) {
	probed := int64(1)
	dur := int64(7284512)
	container, vcodec, acodec := "mkv", "h264", "eac3"
	w, h, ch := 1920, 1080, 6

	it := &store.Item{
		ProbedAt: &probed, DurationMS: &dur, Container: &container,
		VideoCodec: &vcodec, Width: &w, Height: &h,
		AudioCodec: &acodec, AudioChannels: &ch,
	}

	r := ResultFromItem(it)
	if r == nil {
		t.Fatal("ResultFromItem returned nil for a probed item")
	}
	// The decision engine compares against ffprobe's vocabulary, so the
	// extension must be mapped to the format name.
	if r.Container != "matroska" {
		t.Errorf("Container = %q, want matroska", r.Container)
	}
	if v := r.Video(); v == nil || v.Codec != "h264" || v.Height != 1080 {
		t.Errorf("Video() = %+v", v)
	}
	if a := r.Audio(); a == nil || a.Codec != "eac3" || a.Channels != 6 {
		t.Errorf("Audio() = %+v", a)
	}

	// An unprobed item yields nil, which Decide treats as direct play.
	if ResultFromItem(&store.Item{}) != nil {
		t.Error("ResultFromItem returned a result for an unprobed item")
	}
	if ResultFromItem(nil) != nil {
		t.Error("ResultFromItem(nil) is not nil")
	}
}

// A stored eac3 track must produce an audio-only transcode, not a full one —
// this is the path a third of a real library takes.
func TestStoredProbeDrivesDecision(t *testing.T) {
	probed := int64(1)
	container, vcodec, acodec := "mkv", "h264", "eac3"
	w, h, ch := 1920, 1080, 6

	it := &store.Item{
		ProbedAt: &probed, Container: &container,
		VideoCodec: &vcodec, Width: &w, Height: &h,
		AudioCodec: &acodec, AudioChannels: &ch,
	}

	d := Decide(ResultFromItem(it), BrowserProfile())
	if d.Method != Transcode {
		t.Fatalf("Method = %q (%s), want transcode", d.Method, d.Reason)
	}
	if d.VideoAction != "copy" {
		t.Errorf("VideoAction = %q, want copy — the video is fine", d.VideoAction)
	}
}

func TestContainerFromExtension(t *testing.T) {
	tests := map[string]string{
		"mkv": "matroska", "mp4": "mov", "m4v": "mov", "mov": "mov",
		"webm": "webm", "avi": "avi", "ts": "mpegts", "weird": "weird",
	}
	for in, want := range tests {
		if got := containerFromExtension(in); got != want {
			t.Errorf("containerFromExtension(%q) = %q, want %q", in, got, want)
		}
	}
}
