package transcode

import (
	"context"
	"testing"

	"lancast/internal/probe"
)

/*
 * A viewer's session is reused only for the same work (see workKey).
 *
 * No ffmpeg runs, as in TestHLSReuseDoesNotCrossOwners: the binary does not
 * exist, so a request that is not reused fails to start, and that is how these
 * tell reuse from a new session — on every platform, with no shell needed.
 */

func remuxAudioEncode() probe.Decision {
	return probe.Decision{Method: probe.Transcode, VideoAction: "copy", AudioAction: "encode",
		Reason: "audio codec ac3 is not supported"}
}

func encode720() probe.Decision {
	return probe.Decision{Method: probe.Transcode, VideoAction: "encode", AudioAction: "encode",
		TargetHeight: 720, Reason: "video is 1036p, above the 720p limit; audio codec ac3 is not supported"}
}

// running is a session of owner's for item 7 at 208 s, made for o's work.
func running(t *testing.T, m *Manager, o Options) *Session {
	t.Helper()
	s, err := m.AdoptForTest(7, "u_test")
	if err != nil {
		t.Fatal(err)
	}
	s.StartAt = o.StartAt
	s.work = workFor(o)
	return s
}

func noFFmpeg(t *testing.T) *Manager {
	m := NewManager(t.TempDir(), quiet())
	m.bin = "ffmpeg-not-run"
	return m
}

/*
 * Lowering Quality before the film has moved asks again at the same offset,
 * and must start the encode that was decided rather than be handed the remux
 * already running. On the installed service it was handed the remux, and
 * waited 1m42s for that to finish.
 */
func TestHLSReuseFollowsAQualityChange(t *testing.T) {
	m := noFFmpeg(t)
	remuxed := running(t, m, Options{StartAt: 208, Decision: remuxAudioEncode()})

	got, err := m.EnsureHLS(context.Background(), 7, "u_test",
		Options{StartAt: 208, Decision: encode720()})
	if got == remuxed {
		t.Fatal("a 720p encode was handed the copied-video session already running")
	}
	if err == nil {
		t.Fatal("fixture: a session started with no ffmpeg")
	}
	if m.Session(remuxed.ID) != nil {
		t.Error("the superseded remux was left running beside the encode")
	}
}

// Choosing another audio track before the film has moved is the same case.
func TestHLSReuseFollowsAnAudioTrackChange(t *testing.T) {
	m := noFFmpeg(t)
	first := running(t, m, Options{StartAt: 208, Decision: remuxAudioEncode(), AudioIndex: 1})

	got, _ := m.EnsureHLS(context.Background(), 7, "u_test",
		Options{StartAt: 208, Decision: remuxAudioEncode(), AudioIndex: 2})
	if got == first {
		t.Fatal("audio track 2 was handed the session playing track 1")
	}
}

/*
 * The same work, asked again, is still reused — even worded differently. The
 * player asks for the playlist more than once, and an ffmpeg per request is
 * what reuse exists to prevent.
 */
func TestHLSReuseKeepsTheSameWork(t *testing.T) {
	m := noFFmpeg(t)
	d := encode720()
	s := running(t, m, Options{StartAt: 208, Decision: d})

	d.Reason = "worded another way"
	got, err := m.EnsureHLS(context.Background(), 7, "u_test", Options{StartAt: 208, Decision: d})
	if err != nil || got != s {
		t.Fatalf("the same work asked again got %v, %v — want the running session back", got, err)
	}
}
