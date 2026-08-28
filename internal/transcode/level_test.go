package transcode

import (
	"testing"

	"lancast/internal/probe"
)

/*
 * The level a hardware encode promises.
 *
 * This is the rule that stopped a 2160x1080 episode from encoding at all:
 * every hardware encode stated `4.1`, NVENC enforced it, and the whole job
 * failed before a frame was produced —
 * `InitializeEncoder failed: invalid param (8): Invalid Level`.
 *
 * One named case per shape, asserting the level *and* why, in the manner of
 * decide_test.go. A level that is silently too small is indistinguishable from
 * a broken file, which is what made this expensive to find.
 */

func TestLevelForOrdinary1080p(t *testing.T) {
	// The case that always worked, and must keep the level it had: 4.1 is what
	// browsers and devices are most reliably happy with.
	if got := H264Level(1920, 1080, 23.976); got != "4.1" {
		t.Errorf("1080p24 = %q, want 4.1", got)
	}
}

func TestLevelForTheEpisodeThatFoundThis(t *testing.T) {
	/*
	 * 2160x1080. The trap in one line: it is *1080 tall*, so every height cap
	 * in the system waves it through, and 2160 wide, which is 9,180 macroblocks
	 * against level 4.1's ceiling of 8,192.
	 */
	got := H264Level(2160, 1080, 23.976)
	if got == "4.1" {
		t.Fatal("2160x1080 was given level 4.1, which NVENC refuses outright")
	}
	if got != "5.0" {
		t.Errorf("2160x1080 = %q, want 5.0 — the lowest level that fits it", got)
	}
}

func TestLevelForFourKAtTwentyFour(t *testing.T) {
	// 3840x2160 is 32,400 macroblocks: past 5.0's 22,080, inside 5.1's 36,864.
	if got := H264Level(3840, 2160, 23.976); got != "5.1" {
		t.Errorf("4K24 = %q, want 5.1", got)
	}
}

func TestLevelForFourKAtSixty(t *testing.T) {
	/*
	 * The same frame at twice the rate needs a higher level, and this is the
	 * only reason the frame rate is consulted at all: 4K60 is 1,944,000
	 * macroblocks per second against 5.1's 983,040.
	 */
	if got := H264Level(3840, 2160, 60); got != "5.2" {
		t.Errorf("4K60 = %q, want 5.2 — 5.1 cannot carry the throughput", got)
	}
}

func TestLevelIgnoresAnUnknownFrameRate(t *testing.T) {
	// Zero means the source did not say. The frame still decides; the rate is
	// simply not consulted, rather than being treated as zero throughput.
	if got := H264Level(3840, 2160, 0); got != "5.1" {
		t.Errorf("4K with no reported rate = %q, want 5.1 from the frame alone", got)
	}
}

func TestLevelNeverGoesBelowWhatItAlwaysWas(t *testing.T) {
	/*
	 * Small frames stay at 4.1 rather than dropping to 3.0.
	 *
	 * Nothing is gained by telling a decoder that a 480p stream is level 3.0,
	 * and this rule is only allowed to move *up* — it is a repair for frames
	 * that do not fit, not a new opinion about frames that always did.
	 */
	for _, wh := range [][2]int{{640, 480}, {1280, 720}, {854, 480}} {
		if got := H264Level(wh[0], wh[1], 25); got != "4.1" {
			t.Errorf("%dx%d = %q, want 4.1", wh[0], wh[1], got)
		}
	}
}

func TestLevelWithNoFrameSizeKeepsTheOldDefault(t *testing.T) {
	// An unprobed source has nothing to compute from, and the previous
	// behaviour is the conservative answer rather than an invented one.
	if got := H264Level(0, 0, 0); got != "4.1" {
		t.Errorf("unknown frame = %q, want 4.1", got)
	}
}

func TestMacroblocksRoundUp(t *testing.T) {
	// A partial macroblock is still a macroblock, and 1080 is not a multiple
	// of 16 — the arithmetic that decides whether 1080p fits at all.
	if got := mbFor(1920, 1080); got != 8160 {
		t.Errorf("1920x1080 = %d macroblocks, want 8160", got)
	}
	if got := mbFor(2160, 1080); got != 9180 {
		t.Errorf("2160x1080 = %d macroblocks, want 9180", got)
	}
}

/*
 * The keyframe interval, which decides how long a scrub takes to show a
 * picture.
 *
 * Nothing set one, so encodes ran at the encoder's default of roughly ten
 * seconds — and because the output fragments on keyframes, that is how long the
 * viewer waited after every seek. Measured at ~2,344ms to first bytes against
 * ~1,413ms at two seconds.
 */

func TestGopIsTwoSecondsOfWhateverTheRateIs(t *testing.T) {
	// The point of deriving it: 48 frames is two seconds at 24fps and less
	// than one at 60, so a fixed number is right for one library and wrong for
	// the next.
	for _, tc := range []struct {
		fps  float64
		want int
	}{
		{23.976, 48},
		{24, 48},
		{25, 50},
		{29.97, 60},
		{30, 60},
		{50, 100},
		{59.94, 120},
		{60, 120},
	} {
		if got := gopFrames(tc.fps); got != tc.want {
			t.Errorf("%.3f fps = %d frames, want %d", tc.fps, got, tc.want)
		}
	}
}

func TestGopFallsBackWhenTheRateIsUnknown(t *testing.T) {
	// An unprobed or unreported rate gets the common case rather than a guess
	// about an unusual one.
	if got := gopFrames(0); got != 48 {
		t.Errorf("unknown rate = %d, want 48", got)
	}
}

func TestGopHasAFloor(t *testing.T) {
	// A pathological rate must not produce a keyframe every frame, which costs
	// far more in bits than it returns in latency.
	if got := gopFrames(1); got != 12 {
		t.Errorf("1 fps = %d, want the floor of 12", got)
	}
}

// And it reaches the command line, which is the half a passing rule does not
// prove.
func TestArgsCarryTheKeyframeInterval(t *testing.T) {
	a := Args(Options{
		Input: "in.mkv", Output: Progressive, AudioIndex: -1,
		Encoder: Encoder{Name: "h264_nvenc", qualityFlag: "-cq"},
		Decision: probe.Decision{
			Method: probe.Transcode, VideoAction: "encode", AudioAction: "copy",
			SourceWidth: 1920, SourceHeight: 1080, SourceFrameRate: 23.976,
		},
	})
	if got := argValue(a, "-g"); got != "48" {
		t.Errorf("keyframe interval = %q, want 48", got)
	}
}

// A copy has no encoder to instruct, and naming a GOP for one would be an
// argument about a stream nothing is re-encoding.
func TestACopyGetsNoKeyframeInterval(t *testing.T) {
	a := Args(Options{
		Input: "in.mkv", Output: Progressive, AudioIndex: -1,
		Decision: probe.Decision{
			Method: probe.Remux, VideoAction: "copy", AudioAction: "copy",
		},
	})
	for _, v := range a {
		if v == "-g" {
			t.Fatal("a copy was given a keyframe interval")
		}
	}
}
