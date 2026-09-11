package transcode

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func readySession(t *testing.T) *Session {
	t.Helper()
	return &Session{Dir: t.TempDir()}
}

func write(t *testing.T, s *Session, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(s.Dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

/*
 * The failure this exists for: a segment that exists, with bytes in it, that
 * ffmpeg has not finished. A complete playlist has the player asking for exactly
 * that segment, and serving it is a corrupt append.
 */
func TestASegmentWithBytesIsNotReadyUntilFFmpegListsIt(t *testing.T) {
	m, s := &Manager{}, readySession(t)
	write(t, s, "index.m3u8", "#EXTM3U\n#EXTINF:6.006000,\nseg00000.m4s\n")
	write(t, s, "seg00000.m4s", "finished")
	write(t, s, "seg00001.m4s", "half of a segm")

	if _, err := m.WaitForSegment(context.Background(), s, "seg00001.m4s", 300*time.Millisecond); err == nil {
		t.Fatal("served a segment ffmpeg was still writing")
	}
	if _, err := m.WaitForSegment(context.Background(), s, "seg00000.m4s", 300*time.Millisecond); err != nil {
		t.Fatalf("a listed segment was not served: %v", err)
	}
}

func TestASegmentIsServedOnceFFmpegListsIt(t *testing.T) {
	m, s := &Manager{}, readySession(t)
	write(t, s, "index.m3u8", "#EXTM3U\n")
	write(t, s, "seg00000.m4s", "in progress")

	go func() {
		time.Sleep(150 * time.Millisecond)
		write(t, s, "index.m3u8", "#EXTM3U\n#EXTINF:6.006000,\nseg00000.m4s\n")
	}()
	if _, err := m.WaitForSegment(context.Background(), s, "seg00000.m4s", 3*time.Second); err != nil {
		t.Fatalf("a segment listed while waiting was never served: %v", err)
	}
}

// The init segment is what went out at 0 bytes. ffmpeg writes it before the
// first segment, and its playlist after, so the playlist is the signal.
func TestInitIsNotReadyBeforeFFmpegsPlaylistExists(t *testing.T) {
	m, s := &Manager{}, readySession(t)
	write(t, s, "init.mp4", "")

	if _, err := m.WaitForSegment(context.Background(), s, "init.mp4", 300*time.Millisecond); err == nil {
		t.Fatal("served init before ffmpeg had written a playlist")
	}
	write(t, s, "init.mp4", "ftyp moov")
	write(t, s, "index.m3u8", "#EXTM3U\n#EXTINF:6.006000,\nseg00000.m4s\n")
	if _, err := m.WaitForSegment(context.Background(), s, "init.mp4", 300*time.Millisecond); err != nil {
		t.Fatalf("init not served once the playlist existed: %v", err)
	}
}

// A finished ffmpeg has closed everything it will write, and one that never
// wrote the segment says so rather than waiting out the timeout.
func TestAFinishedEncodeAnswersAtOnce(t *testing.T) {
	m, s := &Manager{}, readySession(t)
	s.done = true
	write(t, s, "seg00003.m4s", "last")

	if _, err := m.WaitForSegment(context.Background(), s, "seg00003.m4s", time.Second); err != nil {
		t.Errorf("a segment a finished encode wrote was refused: %v", err)
	}
	start := time.Now()
	if _, err := m.WaitForSegment(context.Background(), s, "seg00004.m4s", 5*time.Second); err == nil {
		t.Error("a segment a finished encode never wrote was reported ready")
	}
	if time.Since(start) > time.Second {
		t.Error("waited out the timeout for a segment that can never come")
	}
}
