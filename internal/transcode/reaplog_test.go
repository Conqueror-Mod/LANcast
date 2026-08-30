package transcode

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

/*
 * What a reaped session leaves in the log.
 *
 * This is the third birth-at-Info, death-at-Debug pair found in this file, and
 * they keep costing the same thing: debug logging is off on a normal server, so
 * a run of sessions on one item records every start and no ending.
 *
 * Reached while trying to explain a queue that did not advance after a long
 * pause. A thirteen-minute pause against a ten-minute timeout produced no reap
 * line and no new session — which either means the reaper did not take it, or
 * something kept it alive. Those are different faults and the only line that
 * tells them apart is this one.
 *
 * Built without ffmpeg on purpose: the neighbouring reaper test drives a fake
 * binary through a shell script and skips on Windows, so the behaviour it
 * covers is unverified on the platform this ships to.
 */
func TestAReapedSessionSaysWhyAtInfo(t *testing.T) {
	var buf bytes.Buffer
	m := &Manager{
		log: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
			// Info, so a line written at Debug fails this rather than passing
			// quietly — which is the whole point.
			Level: slog.LevelInfo,
		})),
		sessions:    map[string]*Session{},
		IdleTimeout: time.Millisecond,
	}

	// A session with no process behind it: reap only reads its clock, and Stop
	// tolerates a nil command.
	s := &Session{ID: "abc123", ItemID: 42}
	s.lastTouch = time.Now().Add(-time.Hour)
	s.NoteServed(4096)
	m.sessions[s.ID] = s

	m.reap()

	if len(m.Sessions()) != 0 {
		t.Fatal("the idle session was not reaped")
	}

	line := buf.String()
	if !strings.Contains(line, "reaping idle transcode") {
		t.Fatalf("nothing was logged at Info about the reap: %q", line)
	}
	if !strings.Contains(line, "item=42") {
		t.Errorf("the line does not say which item: %s", line)
	}
	/*
	 * The numbers, not just the notice.
	 *
	 * A session reaped at 601 seconds and one reaped at 4,000 are different
	 * stories about what the client was doing, and the timeout is configurable
	 * so the threshold alone does not imply the age.
	 */
	if !strings.Contains(line, "idle_seconds=") {
		t.Errorf("the line does not say how long it had been idle: %s", line)
	}
	if !strings.Contains(line, "served_bytes=4096") {
		t.Errorf("the line does not say what it delivered: %s", line)
	}
}
