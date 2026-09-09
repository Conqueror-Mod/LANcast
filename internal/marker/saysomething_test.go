package marker

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/store"
)

/*
 * A pass that runs for hours says so.
 *
 * Every line this package could emit was a failure. Meanwhile a pass occupies a
 * core with ffmpeg for as long as it takes to decode the tail of every
 * unexamined film — on a real library, 337 items at roughly forty-five seconds
 * each, so about four hours — and said nothing at all while doing it.
 *
 * From outside that is indistinguishable from a leaked process, and two people
 * reached exactly that conclusion about it in one evening: an ffmpeg parented by
 * the server, no session anywhere in the log, and a new one every few minutes.
 * Settling it took a code read and a database query, for a question the log
 * should have answered.
 */

// countingStore reports a fixed backlog so the finished line has a number to
// carry.
type countingStore struct {
	stubStore
	remaining int
}

func (c *countingStore) PendingMarkersCount(context.Context) (int, error) {
	return c.remaining, nil
}

func loggedWorker(t *testing.T, st Store) (*Worker, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	w := NewWorker(st, slog.New(slog.NewTextHandler(&buf, nil)))
	// A binary that cannot exist: every scan fails fast, because what is under
	// test is what the pass says about itself, not what it finds.
	w.FFmpegPath = filepath.Join(t.TempDir(), "no-such-ffmpeg.exe")
	return w, &buf
}

func TestAPassSaysItStartedAndFinished(t *testing.T) {
	dur := int64(6_000_000)
	st := &countingStore{remaining: 336}
	st.pending = []store.Item{
		{ID: 1, Path: filepath.Join(t.TempDir(), "a.mkv"), DurationMS: &dur},
	}
	w, buf := loggedWorker(t, st)

	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "credits detection started") {
		t.Errorf("a pass began and said nothing:\n%s", out)
	}
	if !strings.Contains(out, "credits detection finished") {
		t.Errorf("a pass ended and said nothing:\n%s", out)
	}
	/*
	 * The number is the point. "ffmpeg is busy again" and "there are 336 films
	 * left" are a mystery and an estimate, and the difference is one field.
	 */
	if !strings.Contains(out, "remaining=336") {
		t.Errorf("the finished line does not say how much is left:\n%s", out)
	}
	if !strings.Contains(out, "level=INFO") {
		t.Errorf("logged below INFO, so an ordinary server records nothing:\n%s", out)
	}
}

/*
 * An empty pass says nothing.
 *
 * Run is called on a timer. A line for every sweep would bury the ones that
 * mean something, which is the same reason the transcode reaper logs what it
 * took rather than every time it looked.
 */
func TestAnEmptyPassIsSilent(t *testing.T) {
	w, buf := loggedWorker(t, &countingStore{})

	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out := buf.String(); out != "" {
		t.Errorf("a pass with nothing to do logged:\n%s", out)
	}
}

/*
 * A count that could not be read is visibly missing.
 *
 * Nothing left to do and could not find out are opposite states, and reporting
 * the second as zero would say the backlog is clear at the exact moment the
 * server has lost track of it.
 */
func TestAnUnreadableCountIsNotReportedAsNone(t *testing.T) {
	dur := int64(6_000_000)
	st := &failingCountStore{}
	st.pending = []store.Item{
		{ID: 1, Path: filepath.Join(t.TempDir(), "a.mkv"), DurationMS: &dur},
	}
	w, buf := loggedWorker(t, st)

	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "remaining=0") {
		t.Errorf("an unreadable count was reported as an empty backlog:\n%s", out)
	}
	if !strings.Contains(out, "remaining=-1") {
		t.Errorf("an unreadable count is not marked as unknown:\n%s", out)
	}
}

type failingCountStore struct{ stubStore }

func (f *failingCountStore) PendingMarkersCount(context.Context) (int, error) {
	return 0, context.DeadlineExceeded
}
