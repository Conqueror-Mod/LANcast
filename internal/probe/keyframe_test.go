package probe

import (
	"testing"
	"time"
)

/*
 * Picking the keyframe a copied resume will actually start on (ADR 0072).
 *
 * Tested against ffprobe's real output shape rather than a tidied one, because
 * the shape is where this goes wrong: the csv writer leaves a trailing comma
 * on a single field, so every line arrives as "399.649000," and a parser that
 * does not expect it fails on every line while returning a perfectly
 * respectable "no keyframe found".
 *
 * The fixtures are the film this was measured on. Keyframes near the 400s
 * resume point sit at 394.603, 399.649 and 404.571, and the gap between the
 * last two is what made the picture run 0.33s behind the sound.
 */

// As ffprobe actually prints it, trailing separators and all.
const realOutput = `389.389000,
390.724000,
392.684000,
394.603000,
399.649000,
404.571000,
406.114000,
`

func TestPicksTheKeyframeBeforeTheResume(t *testing.T) {
	at := 400 * time.Second
	got, ok := ParseKeyframeBefore(realOutput, at)
	if !ok {
		t.Fatal("no keyframe found in output that plainly contains several")
	}
	want := time.Duration(399.649 * float64(time.Second))
	if abs(got-want) > time.Millisecond {
		t.Errorf("got %v, want %v", got, want)
	}
}

/*
 * The one after the resume must never be chosen. Seeking audio forward of the
 * video is the same fault as today's, pointed the other way, and it would be
 * larger: the next keyframe here is 4.9 seconds on.
 */
func TestNeverPicksAKeyframeAfterTheResume(t *testing.T) {
	at := 400 * time.Second
	got, _ := ParseKeyframeBefore(realOutput, at)
	if got > at {
		t.Errorf("picked %v, which is after the resume at %v", got, at)
	}
}

// A resume that lands exactly on a keyframe takes that keyframe, not the one
// before it.
func TestAResumeOnAKeyframeTakesIt(t *testing.T) {
	at := time.Duration(399.649 * float64(time.Second))
	got, ok := ParseKeyframeBefore(realOutput, at)
	if !ok {
		t.Fatal("no keyframe found")
	}
	if abs(got-at) > time.Millisecond {
		t.Errorf("got %v, want the keyframe itself at %v", got, at)
	}
}

/*
 * Nothing in the window is an ordinary answer, not a failure. The caller
 * converts the way it always did rather than refusing to play the film.
 */
func TestNoKeyframeInTheWindowIsNotFound(t *testing.T) {
	if _, ok := ParseKeyframeBefore("500.000000,\n501.250000,\n", 400*time.Second); ok {
		t.Error("found a keyframe when every candidate is after the resume")
	}
	if _, ok := ParseKeyframeBefore("", 400*time.Second); ok {
		t.Error("found a keyframe in empty output")
	}
}

/*
 * The trailing comma is the whole reason this function exists separately.
 * Without handling it every line fails to parse and the result is a confident
 * "none found" on a file full of keyframes — which would disable the fix
 * silently and for ever.
 */
func TestTheTrailingSeparatorDoesNotDefeatIt(t *testing.T) {
	if _, ok := ParseKeyframeBefore("399.649000,\n", 400*time.Second); !ok {
		t.Error("a line with ffprobe's trailing comma was not parsed")
	}
	// And the same value without one, in case the writer ever stops adding it.
	if _, ok := ParseKeyframeBefore("399.649000\n", 400*time.Second); !ok {
		t.Error("a line without a trailing comma was not parsed")
	}
}

// Junk lines are skipped rather than refused: the same output carries blanks
// and the occasional frame with no timestamp.
func TestJunkLinesAreSkipped(t *testing.T) {
	out := "\nN/A,\n394.603000,\ngarbage\n399.649000,\n\n"
	got, ok := ParseKeyframeBefore(out, 400*time.Second)
	if !ok {
		t.Fatal("junk defeated an output containing two good keyframes")
	}
	want := time.Duration(399.649 * float64(time.Second))
	if abs(got-want) > time.Millisecond {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Order is not guaranteed by anything, so the latest is chosen rather than the
// last one printed.
func TestOutOfOrderOutputStillPicksTheLatest(t *testing.T) {
	out := "399.649000,\n394.603000,\n389.389000,\n"
	got, ok := ParseKeyframeBefore(out, 400*time.Second)
	if !ok {
		t.Fatal("no keyframe found")
	}
	want := time.Duration(399.649 * float64(time.Second))
	if abs(got-want) > time.Millisecond {
		t.Errorf("got %v, want the latest at %v", got, want)
	}
}

// A resume at the start has nothing to align against.
func TestAResumeAtZeroIsNotAsked(t *testing.T) {
	if _, ok := ParseKeyframeBefore(realOutput, 0); ok {
		t.Error("a resume at zero found a keyframe to align to")
	}
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
