package transcode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Complete reports whether this session's playlist is listed whole up front.
func (s *Session) Complete() bool {
	return s.MediaSeconds > 0
}

/*
 * WaitForSegment blocks until ffmpeg has *finished* a segment, not merely begun
 * it.
 *
 * WaitForFile's test — the file exists with bytes in it — was enough while the
 * player only ever asked for segments ffmpeg's growing playlist had already
 * listed. A complete playlist lists segments the encode has not reached, and the
 * player asks for them about a segment ahead of the picture, which is exactly
 * when a file can exist and still be mid-write: ffmpeg writes segments in place.
 * Served that way in the experiment behind CompletePlaylist, init.mp4 went out
 * at 0 bytes and the element failed with APPEND_FAILED.
 *
 * So a segment is ready once ffmpeg's own playlist names it, which ffmpeg does
 * only after closing it; and the init segment once that playlist exists at all,
 * since ffmpeg writes init before the first segment. A finished ffmpeg has
 * closed everything it will ever write, so then existing is enough.
 *
 * Used for growing playlists too, where it changes nothing a player can see: a
 * player reading one only asks for what it lists.
 */
func (m *Manager) WaitForSegment(ctx context.Context, s *Session, name string, timeout time.Duration) (string, error) {
	path := filepath.Join(s.Dir, name)
	playlist := filepath.Join(s.Dir, "index.m3u8")
	deadline := time.Now().Add(timeout)

	for {
		if body, err := os.ReadFile(playlist); err == nil && len(body) > 0 {
			if name == "init.mp4" || Listed(string(body), name) {
				if _, err := os.Stat(path); err == nil {
					return path, nil
				}
			}
		}
		if done, ffErr := s.Done(); done {
			if ffErr != nil {
				return "", ffErr
			}
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
			return "", fmt.Errorf("transcode finished without producing %s: %s", name, s.Stderr())
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for %s", name)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

/*
 * Patience bounds a wait for a remux to close its own playlist.
 *
 * Two bounds rather than one deadline, because "how long has this taken" is the
 * wrong question to ask a remux. What matters is whether it is still getting
 * anywhere: one that is writing fifty segments a second will finish, however
 * large the film, and one that has written nothing for seconds is not about to.
 */
type Patience struct {
	// Stall gives up when the playlist has not grown for this long.
	Stall time.Duration
	// Cap is the longest to wait however well it is going, so a pathological
	// source cannot hold a response open indefinitely.
	Cap time.Duration
}

/*
 * WaitForEndlist reports whether ffmpeg's own playlist is finished — closed with
 * #EXT-X-ENDLIST — waiting while the remux is still making progress.
 *
 * It exists for sessions that copy the video and are listed as they grow. The
 * desktop client's engine reads a growing playlist once, plays what it listed,
 * and fires `ended`; the client then reads that as a cut stream and restarts
 * from where it stopped. Seen on v0.9.14 with It's Always Sunny S16E01
 * (video=copy audio=copy): a new session every 36–45 seconds, each delivering
 * the same ~52 MB — what the first playlist fetch had listed. The remux itself
 * wrote the remaining twenty minutes, 205 segments and ENDLIST, in three
 * seconds. A finished playlist is one that engine plays.
 *
 * # Why progress rather than a deadline
 *
 * The fixed twenty seconds this replaced lost a race by about a second, and the
 * way it lost was ugly. Reported as *Scream (2022) starts near the end, then the
 * credits roll*: a 6.1GB MKV, remuxed rather than encoded because the container
 * is unsupported and both codecs are fine. Measured on that file — **1,095 of
 * its 1,124 segments were written in the first twenty seconds**, about 1h49m of
 * a 1h54m film, and the whole remux finished in roughly twenty-one. The wait
 * gave up one segment-worth short of the answer.
 *
 * What the viewer got was not a slow start. A growing playlist is one the engine
 * treats as **live**, so it joined at the live edge — 1:49:30, past the credits
 * marker at 1:48:00 — watched the last minutes, ran out of listed media and
 * stopped. Waiting one more second would have played the film from the
 * beginning.
 *
 * So the wait now ends when the playlist stops growing, not when a clock says
 * so. A remux runs hundreds of times faster than realtime; if segments are still
 * appearing, finishing is worth more than the second it costs.
 *
 * The second return says why the wait ended, for the log: empty on success,
 * otherwise "stalled", "capped", "failed" or "cancelled". False means the caller
 * serves the playlist growing, exactly as before.
 */
func (m *Manager) WaitForEndlist(ctx context.Context, s *Session, p Patience) (bool, string) {
	playlist := filepath.Join(s.Dir, "index.m3u8")
	started := time.Now()
	lastGrowth := started
	grown := -1

	for {
		/*
		 * Somebody is waiting on this session, so it is not abandoned.
		 *
		 * The reaper kills an HLS session that has handed over no bytes after
		 * UnreadIdleTimeout — thirty seconds — and a session being waited on
		 * has by definition handed over nothing yet. The old twenty-second wait
		 * fitted inside that window; waiting until the remux is actually done
		 * does not, and the first run of this against the installed service
		 * proved it: the wait ran 38.9s and ended `why=failed` because the
		 * reaper had destroyed the session underneath it at 39s, served_bytes=0.
		 *
		 * The playback still recovered, by falling back to the progressive
		 * stream, which is why it looked like a success from the front. It was
		 * not one.
		 */
		s.Touch()

		if body, err := os.ReadFile(playlist); err == nil {
			if Finished(string(body)) {
				return true, ""
			}
			// Length is the progress signal: the file is read anyway, and it
			// grows by a line for every segment ffmpeg lists.
			if len(body) > grown {
				grown = len(body)
				lastGrowth = time.Now()
			}
		}
		if done, ffErr := s.Done(); done {
			if ffErr != nil {
				return false, "failed"
			}
			// ffmpeg writes its final playlist before exiting; read it once more
			// in case it landed between the read above and the exit.
			body, err := os.ReadFile(playlist)
			if err == nil && Finished(string(body)) {
				return true, ""
			}
			return false, "failed"
		}
		if p.Cap > 0 && time.Since(started) > p.Cap {
			return false, "capped"
		}
		if p.Stall > 0 && time.Since(lastGrowth) > p.Stall {
			return false, "stalled"
		}
		select {
		case <-ctx.Done():
			return false, "cancelled"
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Finished reports whether a playlist is closed with #EXT-X-ENDLIST.
func Finished(playlist string) bool {
	for _, line := range strings.Split(playlist, "\n") {
		if strings.TrimSpace(line) == "#EXT-X-ENDLIST" {
			return true
		}
	}
	return false
}
