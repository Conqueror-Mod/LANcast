package transcode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lancast/internal/probe"
)

/*
 * Does a playlist actually appear while a film is converting?
 *
 * Nothing asked that until now, and the cost of not asking was a release.
 * `-hls_playlist_type vod` makes ffmpeg defer the playlist until the encode
 * ends, so every HLS file request timed out on its thirty-second wait and fell
 * back to the progressive path — the very path segments were introduced to
 * replace. It shipped, and the whole server log held one HLS file session, on
 * the day the feature landed, and none after.
 *
 * Every existing test around this passes under the bug. The argument tests
 * assert the flags the code means to pass, and it passed exactly the flags it
 * meant to. The client tests assert which URL is asked for, and it asked for
 * the right one. Nobody ran ffmpeg and looked.
 *
 * There was even a harness — cmd/hlsharness — built to answer this exact
 * question, which found the same bug for *channels*. It takes a channel URL. It
 * was never pointed at a file.
 *
 * So this runs the shipping code with a real ffmpeg against a real file and
 * watches. It needs no media library: ffmpeg generates its own input.
 */

/*
 * syntheticFilm writes a clip that needs converting, and is long enough that
 * the question can be asked while it is still being converted.
 *
 * Three hundred seconds, chosen by measurement rather than taste. On this
 * machine the playlist appears at ~1.16s under `event`, and under `vod` it had
 * not appeared after six. A shorter clip closes that gap — at 120 seconds the
 * `vod` playlist arrives at 5.6s, which is still wrong and much easier to pass
 * by accident. The margin is the whole value of the test.
 *
 * ac3 audio because that is what forces a conversion in the first place: it is
 * the reason the reported film was being converted at all, and h264 video is
 * copied through, which is the shape that actually fails in the field.
 */
func syntheticFilm(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "film.mkv")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=160x120:rate=10:duration=300",
		"-f", "lavfi", "-i", "anullsrc=r=48000:cl=5.1:d=300",
		// -g 60 is six seconds at this frame rate. Without it x264 places
		// keyframes far apart, ffmpeg can only cut segments on one, and a
		// harness asking about six-second segments silently measures
		// twenty-five-second ones.
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-g", "60",
		"-c:a", "ac3", "-shortest", "-y", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("could not generate a test clip: %v: %s", err, out)
	}
	return path
}

func ffmpegOrSkip(t *testing.T) *Manager {
	t.Helper()
	m := NewManager(t.TempDir(), quiet())
	if !m.Available() {
		t.Skip("ffmpeg not installed")
	}
	t.Cleanup(func() { m.StopAll() })
	return m
}

/*
 * A real ffmpeg, handed the arguments this package builds, writes a playlist a
 * player can use — and writes it as an EVENT playlist, which is what makes it
 * exist before the encode has finished.
 *
 * # What this asserts, and what it deliberately does not
 *
 * The failure was an ordering one: under `vod` the playlist is written only
 * when ffmpeg exits, so it never arrived inside the thirty-second wait and
 * every request fell back to the progressive path.
 *
 * The obvious test is therefore "the playlist exists while the session is still
 * running" — and it is not asserted here, because it cannot be made to mean
 * anything. This clip is five minutes of 160x120 video that is *copied*, and
 * the whole conversion finishes in about 210ms on this machine. Under the bug
 * the playlist still appeared inside the wait, simply because ffmpeg had
 * already exited. An ordering assertion would have passed under the very bug it
 * was written for, which is worse than no assertion: it reads as cover.
 *
 * Making it bite would need a contrived slow input, and how slow is a fact
 * about the machine rather than about the code.
 *
 * So the assertion is on the artifact instead. `#EXT-X-PLAYLIST-TYPE` and the
 * deferred write are the *same flag*: a playlist that says EVENT is one ffmpeg
 * writes as it goes. Testing the cause is exact where testing the symptom is a
 * race, and it does catch the shipped bug — verified by putting it back.
 */
func TestARealFFmpegWritesAUsableEventPlaylist(t *testing.T) {
	m := ffmpegOrSkip(t)
	film := syntheticFilm(t)
	ctx := context.Background()

	sess, err := m.EnsureHLS(ctx, 1, "u_test", Options{
		Input:    film,
		Decision: audioEncodeDecision(),
	})
	if err != nil {
		t.Fatalf("EnsureHLS: %v", err)
	}
	defer sess.Stop()

	path, err := m.WaitForFile(ctx, sess, "index.m3u8", 10*time.Second)
	if err != nil {
		t.Fatalf("no playlist at all: %v\n\nffmpeg said: %s", err, sess.Stderr())
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the playlist: %v", err)
	}
	text := string(body)

	/*
	 * EVENT, which is the whole fix.
	 *
	 * The argument test next door asserts the flag this package passes. This
	 * asserts what a real ffmpeg wrote after being passed it — the half nobody
	 * checked, and the half where "the flag means what we assume" turned out to
	 * be false for a fortnight.
	 */
	if !strings.Contains(text, "#EXT-X-PLAYLIST-TYPE:EVENT") {
		t.Errorf("not an EVENT playlist, so ffmpeg writes it only when the encode "+
			"ends — which is the failure that shipped:\n%s", text)
	}

	// A playlist naming no segments is a file, not a playlist: an element given
	// one has nothing to fetch and stalls exactly as if it were missing.
	if !strings.Contains(text, ".m4s") {
		t.Errorf("the playlist lists no segments:\n%s", text)
	}
	if !strings.Contains(text, "#EXT-X-MAP:URI=") {
		t.Errorf("no init segment in an fMP4 playlist:\n%s", text)
	}

	/*
	 * Segments are about the length asked for.
	 *
	 * Not pedantry: this is what caught the synthetic clip being unrepresentative.
	 * Without frequent keyframes in the source, ffmpeg cuts on the keyframes it
	 * has and produces twenty-five-second segments while `-hls_time 6` sits in
	 * the command line looking obeyed. A harness measuring the wrong thing
	 * quietly is the failure mode this whole file exists about.
	 */
	if !strings.Contains(text, "#EXTINF:6.") && !strings.Contains(text, "#EXTINF:5.") {
		t.Errorf("segments are not the ~6s the arguments ask for, so this clip is "+
			"not exercising the shape a real film takes:\n%s", text)
	}
}

/*
 * The whole path a client takes, once: ask for HLS, get a playlist, fetch a
 * segment it names, and find real media in it.
 *
 * Everything above is about the playlist. This is the one assertion that the
 * bytes behind it exist and are what they claim — an fMP4 segment begins with
 * a styp box, the same cheapest-true-statement the live tests make about their
 * output.
 */
func TestASegmentThePlaylistNamesIsRealMedia(t *testing.T) {
	m := ffmpegOrSkip(t)
	film := syntheticFilm(t)
	ctx := context.Background()

	sess, err := m.EnsureHLS(ctx, 3, "u_test", Options{
		Input:    film,
		Decision: audioEncodeDecision(),
	})
	if err != nil {
		t.Fatalf("EnsureHLS: %v", err)
	}
	defer sess.Stop()

	if _, err := m.WaitForFile(ctx, sess, "index.m3u8", 3*time.Second); err != nil {
		t.Fatalf("no playlist: %v", err)
	}
	initPath, err := m.WaitForFile(ctx, sess, "init.mp4", 3*time.Second)
	if err != nil {
		t.Fatalf("no init segment: %v", err)
	}
	head := make([]byte, 12)
	f, err := os.Open(initPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Read(head); err != nil {
		t.Fatalf("reading the init segment: %v", err)
	}
	if got := string(head[4:8]); got != "ftyp" {
		t.Errorf("init segment does not begin with an ftyp box, got %q: % x", got, head)
	}
}

// A guard on the decision the tests above lean on: they are only exercising the
// real path if the file genuinely needs converting.
func TestTheSyntheticFilmNeedsConverting(t *testing.T) {
	if d := audioEncodeDecision(); d.Method != probe.Transcode || d.AudioAction != "encode" {
		t.Fatalf("decision = %+v, want a transcode that re-encodes audio", d)
	}
}
