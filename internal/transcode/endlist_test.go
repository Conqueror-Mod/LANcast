package transcode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

/*
 * A remux's playlist is served finished when ffmpeg finishes it in time.
 *
 * The desktop client's engine plays a growing playlist only as far as its first
 * fetch listed. It's Always Sunny S16E01, copied as-is, restarted every ~40
 * seconds on v0.9.14 while ffmpeg wrote the whole remaining episode in three.
 */

func endlistSession(t *testing.T) *Session {
	t.Helper()
	return &Session{Dir: t.TempDir()}
}

func writePlaylistFile(t *testing.T, s *Session, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(s.Dir, "index.m3u8"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const growingBody = "#EXTM3U\n#EXT-X-PLAYLIST-TYPE:EVENT\n#EXTINF:6.006000,\nseg00000.m4s\n"

func TestAFinishedPlaylistIsReportedFinished(t *testing.T) {
	m, s := &Manager{}, endlistSession(t)
	writePlaylistFile(t, s, growingBody+"#EXT-X-ENDLIST\n")
	if !m.WaitForEndlist(context.Background(), s, time.Second) {
		t.Error("a playlist closed with ENDLIST was not reported finished")
	}
}

func TestAPlaylistThatFinishesWhileWaitingIsServedFinished(t *testing.T) {
	m, s := &Manager{}, endlistSession(t)
	writePlaylistFile(t, s, growingBody)
	go func() {
		time.Sleep(200 * time.Millisecond)
		writePlaylistFile(t, s, growingBody+"#EXTINF:3.9,\nseg00001.m4s\n#EXT-X-ENDLIST\n")
	}()
	if !m.WaitForEndlist(context.Background(), s, 3*time.Second) {
		t.Error("the remux finished during the wait and the playlist was still served growing")
	}
}

// A long remux is not held up for ever: past the timeout it is served growing,
// as it was before this existed.
func TestAPlaylistStillGrowingAtTheTimeoutIsServedGrowing(t *testing.T) {
	m, s := &Manager{}, endlistSession(t)
	writePlaylistFile(t, s, growingBody)
	start := time.Now()
	if m.WaitForEndlist(context.Background(), s, 300*time.Millisecond) {
		t.Fatal("a playlist without ENDLIST was reported finished")
	}
	if time.Since(start) > 2*time.Second {
		t.Error("waited well past the timeout")
	}
}

// A failed ffmpeg will never finish the playlist, so there is nothing to wait for.
func TestAFailedEncodeAnswersAtOnce(t *testing.T) {
	m, s := &Manager{}, endlistSession(t)
	writePlaylistFile(t, s, growingBody)
	s.done, s.err = true, errors.New("ffmpeg: exit status 1")
	start := time.Now()
	if m.WaitForEndlist(context.Background(), s, 5*time.Second) {
		t.Error("a failed encode was reported finished")
	}
	if time.Since(start) > time.Second {
		t.Error("waited out the timeout for an encode that had already failed")
	}
}

func TestFinishedMatchesTheTagOnItsOwnLine(t *testing.T) {
	if Finished(growingBody) {
		t.Error("a growing playlist read as finished")
	}
	if !Finished(growingBody + "#EXT-X-ENDLIST\r\n") {
		t.Error("ENDLIST with a CRLF line ending was missed")
	}
}
