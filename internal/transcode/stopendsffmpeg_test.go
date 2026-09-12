package transcode

import (
	"context"
	"testing"
	"time"
)

/*
 * Stopping a session ends its ffmpeg, and says so.
 *
 * Stop cancelled the context and then called cmd.Wait itself — a second Wait,
 * because startHLS already runs one to record why ffmpeg exited. os/exec
 * documents Wait as a single call, and two of them race for the process state:
 * measured on Windows, a stopped session never reported Done within five
 * seconds, so as far as the reaper and the session ceiling were concerned the
 * session was still running.
 *
 * That matters beyond tidiness. MaxSessions is 3, and a session that never
 * reports finished is a slot that never comes back — which is what "the server
 * may be converting everything it can" looked like from the front.
 *
 * The stand-in ffmpeg is this test binary re-executed (see sleepingFFmpeg), so
 * this runs on Windows, which is where it was measured.
 */

func TestStoppingASessionEndsFFmpeg(t *testing.T) {
	m := sleepingFFmpeg(t)

	sess, err := m.EnsureHLS(context.Background(), 7058, "u_test",
		Options{Input: "x.mkv", Decision: remux()})
	if err != nil {
		t.Fatal(err)
	}
	m.Stop(sess.ID)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if done, _ := sess.Done(); done {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("a stopped session never reported ffmpeg as finished; " +
		"its session slot is held for ever")
}

// Shutdown takes the same path, and a server that cannot report its own
// sessions ended is a server that leaves ffmpeg behind it.
func TestStopAllEndsEveryFFmpeg(t *testing.T) {
	m := sleepingFFmpeg(t)

	var sessions []*Session
	for _, item := range []int64{1, 2} {
		s, err := m.EnsureHLS(context.Background(), item, "u_test",
			Options{Input: "x.mkv", Decision: remux()})
		if err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, s)
	}
	m.StopAll()

	deadline := time.Now().Add(5 * time.Second)
	for _, s := range sessions {
		for {
			if done, _ := s.Done(); done {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("session %s never reported ffmpeg as finished after StopAll", s.ID)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}
