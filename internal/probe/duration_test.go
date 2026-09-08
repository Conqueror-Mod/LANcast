package probe

import (
	"fmt"
	"testing"
)

/*
 * How long is a file, when the file cannot agree with itself?
 *
 * The container's duration is the end of its *longest* track, so a single
 * mistimed audio stream makes the whole file claim a length nothing in it has.
 * These cases are the ones that decide whether the picture or the container
 * wins, and the numbers in the important one are real — see
 * TestDurationPrefersThePictureOverAMistimedTrack.
 *
 * Written as one named case per shape, asserting the duration *and* saying why
 * that is the right answer, in the manner of decide_test.go.
 */

// probeJSON builds an ffprobe document with a container duration and a set of
// streams, so each case below is a few numbers rather than a wall of JSON.
type testStream struct {
	kind     string
	codec    string
	duration string
}

func probeJSON(containerDuration string, streams ...testStream) []byte {
	out := `{"format":{"format_name":"mov,mp4,m4a","duration":"` + containerDuration + `","size":"1","bit_rate":"1"},"streams":[`
	for i, s := range streams {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprintf(`{"index":%d,"codec_type":%q,"codec_name":%q,"duration":%q}`,
			i, s.kind, s.codec, s.duration)
	}
	return []byte(out + `]}`)
}

func durationOf(t *testing.T, raw []byte) int64 {
	t.Helper()
	r, err := ParseJSON(raw)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	return r.DurationMS
}

/*
 * The case this rule exists for, with the numbers off a real library.
 *
 * Public Enemies (2009).mp4: a video stream of 8,383s — the film, 2h20 — an
 * audio stream of 20,073s, and a container duration of 20,073s because the
 * container reports its longest track.
 *
 * Believing the container showed the title as 5h35, put it at the head of a
 * sort by longest, and measured progress against a length nobody reaches:
 * watching the entire film gets to 42%, so it would never mark itself watched.
 */
func TestDurationPrefersThePictureOverAMistimedTrack(t *testing.T) {
	raw := probeJSON("20072.938667",
		testStream{"video", "h264", "8383.375000"},
		testStream{"audio", "aac", "20072.938667"},
	)
	got := durationOf(t, raw)
	if want := int64(8383375); got != want {
		t.Errorf("duration = %d ms (%.2fh), want %d ms (2h20) — the container is timing the broken audio track, not the film",
			got, float64(got)/3600000, want)
	}
}

// The ordinary case, which must not move: a track running a moment past the
// last frame is normal, and the container is the right total.
func TestDurationKeepsTheContainerForOrdinaryStreamSkew(t *testing.T) {
	raw := probeJSON("7204.100000",
		testStream{"video", "h264", "7200.000000"},
		testStream{"audio", "aac", "7204.100000"},
	)
	if got, want := durationOf(t, raw), int64(7204100); got != want {
		t.Errorf("duration = %d, want the container's %d — four seconds of skew is not a broken file", got, want)
	}
}

// The boundary is not crossed at exactly half again as long. A rule that fires
// *at* its threshold is a rule whose threshold nobody can state.
func TestDurationHoldsTheContainerExactlyAtTheThreshold(t *testing.T) {
	raw := probeJSON("3000.000000",
		testStream{"video", "h264", "2000.000000"},
	)
	if got, want := durationOf(t, raw), int64(3000000); got != want {
		t.Errorf("duration = %d, want the container's %d at exactly 3/2", got, want)
	}
}

func TestDurationTakesThePictureJustPastTheThreshold(t *testing.T) {
	raw := probeJSON("3000.001000",
		testStream{"video", "h264", "2000.000000"},
	)
	if got, want := durationOf(t, raw), int64(2000000); got != want {
		t.Errorf("duration = %d, want the picture's %d just past 3/2", got, want)
	}
}

// The rule scales: a 22-minute cartoon with a broken track is corrected on the
// same terms as a three-hour film, which is why the test is a ratio and not a
// number of seconds.
func TestDurationCorrectsShortContentToo(t *testing.T) {
	raw := probeJSON("5400.000000",
		testStream{"video", "h264", "1320.000000"},
		testStream{"audio", "aac", "5400.000000"},
	)
	if got, want := durationOf(t, raw), int64(1320000); got != want {
		t.Errorf("duration = %d, want %d — a 22-minute cartoon claiming 90 minutes", got, want)
	}
}

/*
 * Cover art must never be mistaken for the picture.
 *
 * An album's embedded JPEG is stored as a video stream. If one ever carried a
 * nominal duration and this rule did not exclude it, every song on the server
 * would be resized to the length of its own artwork — which is a far worse
 * failure than the one the rule was written to fix.
 */
func TestDurationIgnoresCoverArtOnAnAudioFile(t *testing.T) {
	raw := probeJSON("245.000000",
		testStream{"video", "mjpeg", "0.040000"},
		testStream{"audio", "mp3", "245.000000"},
	)
	if got, want := durationOf(t, raw), int64(245000); got != want {
		t.Errorf("duration = %d, want the song's %d — the cover art was treated as the picture", got, want)
	}
}

// Matroska usually states no per-stream duration. With nothing to compare
// against, the container stands.
func TestDurationKeepsTheContainerWhenThePictureStatesNothing(t *testing.T) {
	raw := probeJSON("7200.000000",
		testStream{"video", "h264", ""},
		testStream{"audio", "aac", ""},
	)
	if got, want := durationOf(t, raw), int64(7200000); got != want {
		t.Errorf("duration = %d, want the container's %d", got, want)
	}
}

// A file with no video at all — a song — is timed by its container.
func TestDurationOfAnAudioOnlyFile(t *testing.T) {
	raw := probeJSON("245.000000", testStream{"audio", "flac", "245.000000"})
	if got, want := durationOf(t, raw), int64(245000); got != want {
		t.Errorf("duration = %d, want %d", got, want)
	}
}

// MPEG-TS states nothing at the container level, and the duration lives on a
// stream. This has always been the behaviour and must survive the change.
func TestDurationFallsBackToAStreamWhenTheContainerIsSilent(t *testing.T) {
	raw := probeJSON("",
		testStream{"video", "h264", "1800.000000"},
		testStream{"audio", "aac", "1800.000000"},
	)
	if got, want := durationOf(t, raw), int64(1800000); got != want {
		t.Errorf("duration = %d, want the stream's %d", got, want)
	}
}

// Nothing anywhere knows: zero, rather than a guess.
func TestDurationIsZeroWhenNothingStatesOne(t *testing.T) {
	raw := probeJSON("", testStream{"video", "h264", ""})
	if got := durationOf(t, raw); got != 0 {
		t.Errorf("duration = %d, want 0", got)
	}
}
