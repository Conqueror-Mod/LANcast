package transcode

import (
	"context"
	"os"
	"testing"
	"time"
)

/*
 * An HLS session outlives the request that started it.
 *
 * The playlist handler passes its request context to EnsureHLS, and Go cancels
 * that context the moment the handler returns — which is the moment the
 * playlist has been sent. startHLS tied ffmpeg to it, so every segmented
 * session was killed as its playlist went out, having written one segment.
 * The wait goroutine recorded the kill as an ordinary stop, so nothing logged
 * an error; the player fetched segment 0 and then waited for segments that
 * could never be written.
 *
 * Seen as the service on v0.9.13, The Fifth Element: `transcode started …
 * playlist=complete`, then 2.5 seconds later `transcode finished without
 * producing seg00001.m4s:` with an empty reason, and a fall back to the
 * progressive stream. A progressive session is different on purpose: it *is*
 * its response, and ending with the request is correct there.
 *
 * The stand-in ffmpeg is this test binary re-executed, so these run on Windows
 * too — fakeFFmpeg's shell script cannot, and Windows is where this shipped.
 */

const sleeperEnv = "LANCAST_TEST_FFMPEG_SLEEPS"

func init() {
	if os.Getenv(sleeperEnv) == "1" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
}

func sleepingFFmpeg(t *testing.T) *Manager {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Skipf("cannot find the test binary: %v", err)
	}
	t.Setenv(sleeperEnv, "1")
	m := newManager(t, self)
	t.Cleanup(m.StopAll)
	return m
}

func TestAnHLSSessionOutlivesTheRequestThatStartedIt(t *testing.T) {
	m := sleepingFFmpeg(t)

	ctx, cancel := context.WithCancel(context.Background())
	sess, err := m.EnsureHLS(ctx, 7058, "u_test", Options{Input: "x.mkv", Decision: remux()})
	if err != nil {
		t.Fatal(err)
	}

	cancel() // the playlist response has been written
	time.Sleep(500 * time.Millisecond)

	if done, _ := sess.Done(); done {
		t.Fatal("ffmpeg was killed when the playlist request ended; " +
			"every later segment request waits on a process that is gone")
	}
}
