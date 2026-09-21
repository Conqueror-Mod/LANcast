package transcode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
 * A fallback does not destroy a remux that has already finished.
 *
 * The rule it replaces was that falling back from segments to a progressive
 * stream means "the way it abandoned will never be asked for again", so the
 * segments are a slot spent on nobody. True when a fallback is permanent.
 * False when it is impatience — the next attempt goes straight back to
 * segments, and EnsureHLS reuses a session at the same offset before it
 * supersedes anything.
 *
 * Measured on Jay and Silent Bob Reboot, where it cost the most it could: a
 * copy-plus-audio-encode ran 111 seconds of a 118-second job, the viewer gave
 * up, and starting the progressive stream destroyed the session six seconds
 * from done. The retry found the playlist gone and began again.
 */

// finishedSession is a session whose ffmpeg has written a closed playlist.
func finishedSession(t *testing.T, m *Manager, id string, item int64, owner string) *Session {
	t.Helper()
	s := viewerSession(id, item, owner, HLS, 0, 0)
	s.Dir = filepath.Join(t.TempDir(), id)
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	playlist := "#EXTM3U\n#EXT-X-TARGETDURATION:6\nseg0.ts\n#EXT-X-ENDLIST\n"
	if err := os.WriteFile(filepath.Join(s.Dir, "index.m3u8"), []byte(playlist), 0o644); err != nil {
		t.Fatal(err)
	}
	m.sessions[id] = s
	return s
}

// growingSession is one ffmpeg is still writing: a playlist with no ENDLIST.
func growingSession(t *testing.T, m *Manager, id string, item int64, owner string) *Session {
	t.Helper()
	s := viewerSession(id, item, owner, HLS, 0, 0)
	s.Dir = filepath.Join(t.TempDir(), id)
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	playlist := "#EXTM3U\n#EXT-X-TARGETDURATION:6\nseg0.ts\n"
	if err := os.WriteFile(filepath.Join(s.Dir, "index.m3u8"), []byte(playlist), 0o644); err != nil {
		t.Fatal(err)
	}
	m.sessions[id] = s
	return s
}

// The case the change is for.
func TestAFallbackSparesAFinishedRemux(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	finishedSession(t, m, "segments", 7022, "u1")

	// The viewer gave up on segments and the player asked for a progressive
	// stream instead.
	m.supersedeOutput("u1", 7022, HLS, spareFinished)

	if _, ok := m.sessions["segments"]; !ok {
		t.Error("a finished remux was destroyed by the fallback; the next " +
			"request for this offset would have reused it and played at once")
	}
}

/*
 * And the case that must not change with it. A session still being written is
 * exactly what the original rule was for: it is burning CPU for a player that
 * has walked away, and the ceiling is small.
 */
func TestAFallbackStillReclaimsAnUnfinishedRemux(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	growingSession(t, m, "segments", 7022, "u1")

	m.supersedeOutput("u1", 7022, HLS, spareFinished)

	if _, ok := m.sessions["segments"]; ok {
		t.Error("an unfinished remux survived the fallback and is holding a " +
			"slot while encoding for nobody")
	}
}

/*
 * A seek spares nothing, finished or not.
 *
 * Its sessions are offsets this viewer has left, and EnsureHLS only ever
 * reuses one at the *same* offset — so a finished session somewhere else in
 * the film is a slot nothing can claim back.
 */
func TestASeekDoesNotSpareAFinishedRemux(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	finishedSession(t, m, "elsewhere", 7022, "u1")

	m.supersedeOutput("u1", 7022, HLS, spareNothing)

	if _, ok := m.sessions["elsewhere"]; ok {
		t.Error("a seek kept a finished session at an offset nothing will ask for")
	}
}

// Sparing is per viewer, like everything else here: one person's finished
// remux must not be spared into somebody else's supersede, nor destroyed by it.
func TestSparingLeavesAnotherViewerAlone(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	finishedSession(t, m, "mine", 7022, "u1")
	finishedSession(t, m, "theirs", 7022, "u2")

	m.supersedeOutput("u1", 7022, HLS, spareFinished)

	if _, ok := m.sessions["theirs"]; !ok {
		t.Error("superseding one viewer touched another's session")
	}
}

/*
 * An unreadable session counts as unfinished.
 *
 * The only thing a caller does with "finished" is keep the session, so a
 * failed read must not buy a slot on a guess.
 */
func TestAnUnreadableSessionIsNotSpared(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	s := viewerSession("segments", 7022, "u1", HLS, 0, 0)
	s.Dir = filepath.Join(t.TempDir(), "gone") // never created
	m.sessions["segments"] = s

	m.supersedeOutput("u1", 7022, HLS, spareFinished)

	if _, ok := m.sessions["segments"]; ok {
		t.Error("a session whose playlist could not be read was spared anyway")
	}
}

// The predicate itself, so a failure points at the right half.
func TestRemuxFinished(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	done := finishedSession(t, m, "done", 1, "u1")
	going := growingSession(t, m, "going", 2, "u1")

	if !done.remuxFinished() {
		t.Error("a closed playlist did not read as finished")
	}
	if going.remuxFinished() {
		t.Error("a growing playlist read as finished")
	}
}

/*
 * The wiring, which is the half that was actually broken.
 *
 * Every test above calls supersedeOutput directly, so they prove the rule and
 * say nothing about whether the fallback uses it. Checked by restoring the old
 * argument at the call site: all of them still passed, which makes them worth
 * exactly nothing against the fault they were written for.
 *
 * Progressive cannot be driven from a test without ffmpeg and a real item, so
 * this reads the call site instead. A source-level assertion is a poor tool and
 * the right one here: the alternative is a suite that is green while the bug is
 * present, which is how this got shipped in the first place.
 */
func TestTheFallbackAsksToSpareFinishedSessions(t *testing.T) {
	src, err := os.ReadFile("manager.go")
	if err != nil {
		t.Fatalf("read manager.go: %v", err)
	}
	body := functionBody(string(src), "func (m *Manager) Progressive(")
	if body == "" {
		t.Fatal("Progressive is gone; the fallback supersedes some other way now")
	}
	if !strings.Contains(body, "supersedeOutput(owner, itemID, HLS, spareFinished)") {
		t.Error("the progressive fallback no longer spares a finished remux.\n\n" +
			"Falling back is often impatience rather than a permanent choice, and " +
			"the next attempt comes straight back to segments. Destroying a " +
			"finished remux there threw away 111 seconds of a 118-second job on " +
			"Jay and Silent Bob Reboot.")
	}
}

// functionBody returns the text from a function's signature to the line that
// closes it at column zero.
func functionBody(src, signature string) string {
	i := strings.Index(src, signature)
	if i < 0 {
		return ""
	}
	rest := src[i:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}
