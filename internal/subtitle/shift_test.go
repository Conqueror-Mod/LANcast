package subtitle

import (
	"strings"
	"testing"
)

/*
 * Shifting cues for a resumed film.
 *
 * Reported as a film whose subtitles showed nothing. The track was fine — two
 * subrip streams, extractable in three seconds — and so was every other part of
 * the path. What was wrong was here: WebVTT permits `mm:ss.ttt` as well as
 * `hh:mm:ss.ttt`, ffmpeg's own muxer writes the short form for everything under
 * an hour, and this shifter recognised only the long one.
 *
 * So a resumed film had the last third of its subtitles moved and the first two
 * thirds left at absolute times the player never reaches. The fixture below is
 * copied from a real extraction rather than invented.
 */

// What ffmpeg's webvtt muxer actually produced from Normal (2025).mkv, plus a
// cue past the hour so both shapes appear in one file — which is the normal
// case for any film longer than sixty minutes.
const ffmpegShapes = `WEBVTT

02:46.734 --> 02:47.735
Ah.

03:32.113 --> 03:33.213
<i>In special news,</i>

01:05:10.500 --> 01:05:12.000
Late in the film.
`

func TestShiftMovesFFmpegsShortStamps(t *testing.T) {
	out := string(ShiftVTT([]byte(ffmpegShapes), 60))

	if strings.Contains(out, "02:46.734") {
		t.Error("a sub-hour cue was left at its absolute time; ffmpeg writes " +
			"mm:ss.mmm and only hh:mm:ss.mmm was recognised")
	}
	if !strings.Contains(out, "00:01:46.734") {
		t.Errorf("02:46.734 did not become 00:01:46.734 after a 60s shift:\n%s", out)
	}
}

// The long form still works, which is the half that used to and must not break.
func TestShiftStillMovesLongStamps(t *testing.T) {
	out := string(ShiftVTT([]byte(ffmpegShapes), 60))
	if !strings.Contains(out, "01:04:10.500") {
		t.Errorf("01:05:10.500 did not become 01:04:10.500:\n%s", out)
	}
}

// Both shapes in one file shift by the same amount. A film over an hour long
// contains both, so disagreeing about them is disagreeing with itself.
func TestBothShapesShiftEqually(t *testing.T) {
	out := string(ShiftVTT([]byte(ffmpegShapes), 60))
	for _, want := range []string{"00:01:46.734", "00:02:32.113", "01:04:10.500"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
}

/*
 * A cue wholly before the resume point is dropped, text and all.
 *
 * Leaving the text behind would put a caption with no timing into the file,
 * which a browser reads as part of the previous cue — so one dropped line
 * corrupts the one before it.
 */
func TestACuePastAlreadyIsDroppedWithItsText(t *testing.T) {
	in := `WEBVTT

00:10.000 --> 00:12.000
Long gone.

05:00.000 --> 05:02.000
Still to come.
`
	out := string(ShiftVTT([]byte(in), 120))
	if strings.Contains(out, "Long gone.") {
		t.Error("the text of a dropped cue survived it")
	}
	if !strings.Contains(out, "Still to come.") {
		t.Error("a cue after the offset was dropped")
	}
}

// A cue straddling the resume point starts at zero rather than at a negative
// time, which a player ignores entirely.
func TestAStraddlingCueClampsToZero(t *testing.T) {
	in := "WEBVTT\n\n01:58.000 --> 02:04.000\nAcross the join.\n"
	out := string(ShiftVTT([]byte(in), 120))
	if !strings.Contains(out, "00:00:00.000 -->") {
		t.Errorf("a straddling cue was not clamped to zero:\n%s", out)
	}
}

// Cue settings after the timing survive the rewrite: they position the caption,
// and losing them moves subtitles into the middle of the picture.
func TestCueSettingsSurvive(t *testing.T) {
	in := "WEBVTT\n\n02:00.000 --> 02:04.000 line:90% align:center\nBottom.\n"
	out := string(ShiftVTT([]byte(in), 60))
	if !strings.Contains(out, "line:90% align:center") {
		t.Errorf("cue settings were lost:\n%s", out)
	}
}

func TestParseVTTStampReadsBothShapes(t *testing.T) {
	for _, c := range []struct {
		in   string
		want float64
	}{
		{"00:00:01.500", 1.5},
		{"01:02:03.250", 3723.25},
		{"02:46.734", 166.734},
		{"59:59.999", 3599.999},
	} {
		if got := parseVTTStamp(c.in); got < c.want-0.001 || got > c.want+0.001 {
			t.Errorf("parseVTTStamp(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	// Anything that is neither shape reads as zero rather than as a guess.
	if got := parseVTTStamp("nonsense"); got != 0 {
		t.Errorf("parseVTTStamp(nonsense) = %v, want 0", got)
	}
}

// An offset of zero is the direct-play case and must not rewrite anything —
// including reformatting stamps a player was already reading correctly.
func TestNoOffsetLeavesTheFileAlone(t *testing.T) {
	out := string(ShiftVTT([]byte(ffmpegShapes), 0))
	if out != ffmpegShapes {
		t.Errorf("a zero offset rewrote the file:\n%s", out)
	}
}
