package transcode

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"lancast/internal/probe"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// fakeFFmpeg writes a small script that stands in for ffmpeg, so the manager's
// process lifecycle is tested without a real encode. Skips on Windows, where a
// shell script is not directly executable.
func fakeFFmpeg(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake ffmpeg script needs a POSIX shell")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func newManager(t *testing.T, bin string) *Manager {
	t.Helper()
	m := NewManager(t.TempDir(), quiet())
	m.bin = bin
	return m
}

func remux() probe.Decision {
	return probe.Decision{Method: probe.Remux, VideoAction: "copy", AudioAction: "copy"}
}

func TestManagerReportsUnavailable(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	m.bin = ""
	if m.Available() {
		t.Error("Available() is true with no ffmpeg binary")
	}
	if _, err := m.Progressive(context.Background(), 1, "u1", Options{Decision: remux()}); err != ErrNotInstalled {
		t.Errorf("error = %v, want ErrNotInstalled", err)
	}
}

func TestProgressiveStreamsOutput(t *testing.T) {
	bin := fakeFFmpeg(t, `printf 'FAKEMP4DATA'; exit 0`)
	m := newManager(t, bin)

	rc, err := m.Progressive(context.Background(), 1, "u1", Options{Input: "x.mkv", Decision: remux()})
	if err != nil {
		t.Fatalf("Progressive: %v", err)
	}
	defer rc.Close()

	got, _ := io.ReadAll(rc)
	if string(got) != "FAKEMP4DATA" {
		t.Errorf("output = %q, want the fake payload", got)
	}
}

// Closing the reader must stop the session — a closed browser tab should not
// leave ffmpeg running.
func TestClosingReaderEndsSession(t *testing.T) {
	bin := fakeFFmpeg(t, `while true; do printf 'x'; sleep 0.05; done`)
	m := newManager(t, bin)

	rc, err := m.Progressive(context.Background(), 1, "u1", Options{Input: "x.mkv", Decision: remux()})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	rc.Read(buf)

	if len(m.Sessions()) != 1 {
		t.Fatalf("sessions = %d, want 1 while streaming", len(m.Sessions()))
	}
	rc.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.Sessions()) == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("session survived the reader being closed")
}

// Each transcode is a full ffmpeg process; without a ceiling a few clients
// bring a home server to its knees.
func TestConcurrencyLimit(t *testing.T) {
	bin := fakeFFmpeg(t, `while true; do printf 'x'; sleep 0.05; done`)
	m := newManager(t, bin)
	m.MaxSessions = 2

	var readers []io.ReadCloser
	for i := 0; i < 2; i++ {
		rc, err := m.Progressive(context.Background(), int64(i), "u1", Options{Input: "x.mkv", Decision: remux()})
		if err != nil {
			t.Fatalf("session %d: %v", i, err)
		}
		rc.Read(make([]byte, 1))
		readers = append(readers, rc)
	}
	t.Cleanup(func() {
		for _, rc := range readers {
			rc.Close()
		}
	})

	if _, err := m.Progressive(context.Background(), 99, "u1", Options{Input: "x.mkv", Decision: remux()}); err != ErrTooManySessions {
		t.Errorf("error = %v, want ErrTooManySessions past the limit", err)
	}
}

/*
 * Seeking a transcode re-requests the stream, and the replaced one must go.
 *
 * Without this, each seek left its predecessor running until the reaper
 * noticed, so seeking around one film filled MaxSessions with encodes of that
 * same film and the machine decoded it several times over.
 */
func TestProgressiveSupersedesSameOwnersStream(t *testing.T) {
	bin := fakeFFmpeg(t, `while true; do printf 'x'; sleep 0.05; done`)
	m := newManager(t, bin)

	first, err := m.Progressive(context.Background(), 7, "chris", Options{Input: "x.mkv", Decision: remux()})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	first.Read(make([]byte, 1))

	second, err := m.Progressive(context.Background(), 7, "chris", Options{Input: "x.mkv", Decision: remux()})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	second.Read(make([]byte, 1))

	if n := len(m.Sessions()); n != 1 {
		t.Errorf("sessions = %d, want 1 — the seek should replace the stream it re-requested, not join it", n)
	}
}

// Two people watching the same film at once is a thing a media server must do.
// Superseding on the item alone would have them ending each other's playback on
// every seek, which is a worse bug than the one it fixes.
func TestProgressiveLeavesOtherViewersAlone(t *testing.T) {
	bin := fakeFFmpeg(t, `while true; do printf 'x'; sleep 0.05; done`)
	m := newManager(t, bin)

	for _, who := range []string{"chris", "georgia"} {
		rc, err := m.Progressive(context.Background(), 7, who, Options{Input: "x.mkv", Decision: remux()})
		if err != nil {
			t.Fatalf("%s: %v", who, err)
		}
		defer rc.Close()
		rc.Read(make([]byte, 1))
	}

	if n := len(m.Sessions()); n != 2 {
		t.Errorf("sessions = %d, want 2 — one viewer starting must not end another's stream", n)
	}
}

// The unconfigured loopback state has no accounts, so every request is
// anonymous. Collapsing those together would let a second viewer end the
// first one's film.
func TestProgressiveDoesNotCollapseAnonymousStreams(t *testing.T) {
	bin := fakeFFmpeg(t, `while true; do printf 'x'; sleep 0.05; done`)
	m := newManager(t, bin)

	for i := 0; i < 2; i++ {
		rc, err := m.Progressive(context.Background(), 7, "", Options{Input: "x.mkv", Decision: remux()})
		if err != nil {
			t.Fatalf("session %d: %v", i, err)
		}
		defer rc.Close()
		rc.Read(make([]byte, 1))
	}

	if n := len(m.Sessions()); n != 2 {
		t.Errorf("sessions = %d, want 2 — anonymous callers are not one player", n)
	}
}

func TestEnsureHLSReusesSession(t *testing.T) {
	// Produce a playlist and one segment, then idle so the session stays alive.
	bin := fakeFFmpeg(t, `
d=$(echo "$@" | tr ' ' '\n' | grep index.m3u8 | xargs dirname)
printf '#EXTM3U\n' > "$d/index.m3u8"
printf 'seg' > "$d/seg00000.m4s"
sleep 5`)
	m := newManager(t, bin)

	a, err := m.EnsureHLS(context.Background(), 7, Options{Input: "x.mkv", Decision: remux()})
	if err != nil {
		t.Fatalf("EnsureHLS: %v", err)
	}
	b, err := m.EnsureHLS(context.Background(), 7, Options{Input: "x.mkv", Decision: remux()})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Error("a second request for the same item and offset started a new session")
	}
	if n := len(m.Sessions()); n != 1 {
		t.Errorf("sessions = %d, want 1 reused", n)
	}
	m.StopAll()
}

func TestWaitForFileTimesOut(t *testing.T) {
	bin := fakeFFmpeg(t, `sleep 5`) // never writes the file
	m := newManager(t, bin)

	sess, err := m.EnsureHLS(context.Background(), 1, Options{Input: "x.mkv", Decision: remux()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.StopAll)

	if _, err := m.WaitForFile(context.Background(), sess, "index.m3u8", 200*time.Millisecond); err == nil {
		t.Error("WaitForFile did not time out on a file that never appears")
	}
}

// ffmpeg exiting nonzero must surface as an error, not an infinite wait.
func TestWaitForFileFailsWhenFFmpegDies(t *testing.T) {
	bin := fakeFFmpeg(t, `echo "boom" >&2; exit 1`)
	m := newManager(t, bin)

	sess, err := m.EnsureHLS(context.Background(), 1, Options{Input: "x.mkv", Decision: remux()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.StopAll)

	if _, err := m.WaitForFile(context.Background(), sess, "index.m3u8", 2*time.Second); err == nil {
		t.Error("WaitForFile succeeded despite ffmpeg failing")
	}
}

func TestIdleSessionsAreReaped(t *testing.T) {
	bin := fakeFFmpeg(t, `
d=$(echo "$@" | tr ' ' '\n' | grep index.m3u8 | xargs dirname)
printf '#EXTM3U\n' > "$d/index.m3u8"
sleep 10`)
	m := newManager(t, bin)
	m.IdleTimeout = 50 * time.Millisecond

	sess, err := m.EnsureHLS(context.Background(), 1, Options{Input: "x.mkv", Decision: remux()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.StopAll)

	dir := sess.Dir
	time.Sleep(80 * time.Millisecond)
	m.reap()

	if len(m.Sessions()) != 0 {
		t.Error("an idle session was not reaped")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("a reaped session left its scratch directory behind")
	}
}

func TestStopAllClearsScratch(t *testing.T) {
	bin := fakeFFmpeg(t, `
d=$(echo "$@" | tr ' ' '\n' | grep index.m3u8 | xargs dirname)
printf '#EXTM3U\n' > "$d/index.m3u8"
sleep 10`)
	m := newManager(t, bin)

	m.EnsureHLS(context.Background(), 1, Options{Input: "x.mkv", Decision: remux()})
	root := m.root
	m.StopAll()

	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Error("StopAll left scratch behind")
	}
}

func TestSameOffset(t *testing.T) {
	if !sameOffset(100, 102) {
		t.Error("offsets within a segment should be treated as the same session")
	}
	if sameOffset(100, 130) {
		t.Error("offsets a segment apart should be distinct sessions")
	}
}
