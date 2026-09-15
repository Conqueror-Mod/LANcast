package transcode

import (
	"context"
	"errors"
	"fmt"
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
	if ok, _ := m.WaitForEndlist(context.Background(), s, Patience{Stall: time.Second, Cap: time.Second}); !ok {
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
	if ok, _ := m.WaitForEndlist(context.Background(), s, Patience{Stall: 3 * time.Second, Cap: 3 * time.Second}); !ok {
		t.Error("the remux finished during the wait and the playlist was still served growing")
	}
}

// A remux that has stopped producing is not waited on: a playlist that is not
// growing is not about to finish, and the caller serves it growing as before.
func TestAPlaylistThatStopsGrowingIsServedGrowing(t *testing.T) {
	m, s := &Manager{}, endlistSession(t)
	writePlaylistFile(t, s, growingBody)
	start := time.Now()
	ok, why := m.WaitForEndlist(context.Background(), s,
		Patience{Stall: 300 * time.Millisecond, Cap: 10 * time.Second})
	if ok {
		t.Fatal("a playlist without ENDLIST was reported finished")
	}
	if why != "stalled" {
		t.Errorf("why = %q, want stalled", why)
	}
	// It must give up on the stall rather than sit out the cap.
	if time.Since(start) > 3*time.Second {
		t.Error("waited out the cap for a playlist that had stopped growing")
	}
}

// A failed ffmpeg will never finish the playlist, so there is nothing to wait for.
func TestAFailedEncodeAnswersAtOnce(t *testing.T) {
	m, s := &Manager{}, endlistSession(t)
	writePlaylistFile(t, s, growingBody)
	s.done, s.err = true, errors.New("ffmpeg: exit status 1")
	start := time.Now()
	if ok, why := m.WaitForEndlist(context.Background(), s,
		Patience{Stall: 5 * time.Second, Cap: 5 * time.Second}); ok {
		t.Error("a failed encode was reported finished")
	} else if why != "failed" {
		t.Errorf("why = %q, want failed", why)
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

/*
 * The fault this was rewritten for.
 *
 * Reported as *Scream (2022) starts near the end, then the credits roll*. A
 * 6.1GB MKV, remuxed rather than encoded; measured on that file, **1,095 of its
 * 1,124 segments were written inside the twenty seconds the old fixed wait
 * allowed**, and the whole remux finished in about twenty-one. Giving up one
 * second early did not make the film start slowly — a growing playlist is one
 * the engine treats as live, so it joined at the edge, 1:49:30 of a 1:54:10
 * film, past the credits marker at 1:48:00.
 *
 * So a playlist that is still growing when a fixed deadline would have expired
 * must still be waited on.
 */
func TestAPlaylistStillGrowingIsWaitedOutRatherThanAbandoned(t *testing.T) {
	m, s := &Manager{}, endlistSession(t)
	writePlaylistFile(t, s, growingBody)

	// Segments keep appearing past the point a short fixed deadline would have
	// given up, and only then does it finish.
	go func() {
		body := growingBody
		for i := 1; i <= 8; i++ {
			time.Sleep(60 * time.Millisecond)
			body += fmt.Sprintf("#EXTINF:6.006000,\nseg%05d.m4s\n", i)
			writePlaylistFile(t, s, body)
		}
		writePlaylistFile(t, s, body+"#EXT-X-ENDLIST\n")
	}()

	start := time.Now()
	ok, why := m.WaitForEndlist(context.Background(), s,
		Patience{Stall: 400 * time.Millisecond, Cap: 10 * time.Second})
	if !ok {
		t.Fatalf("a remux that kept producing was abandoned (%s)", why)
	}
	// It has to have waited past where a fixed short deadline would have ended.
	if time.Since(start) < 400*time.Millisecond {
		t.Error("returned before the remux could have finished")
	}
}

// The cap is the backstop: something that produces for ever is still not
// allowed to hold a response open for ever.
func TestAPlaylistThatNeverFinishesIsCapped(t *testing.T) {
	m, s := &Manager{}, endlistSession(t)
	writePlaylistFile(t, s, growingBody)

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		body := growingBody
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case <-time.After(30 * time.Millisecond):
			}
			body += "#EXTINF:6.006000,\nseg.m4s\n"
			writePlaylistFile(t, s, body)
		}
	}()

	ok, why := m.WaitForEndlist(context.Background(), s,
		Patience{Stall: 5 * time.Second, Cap: 500 * time.Millisecond})
	if ok {
		t.Fatal("a playlist that never finished was reported finished")
	}
	if why != "capped" {
		t.Errorf("why = %q, want capped", why)
	}
}
