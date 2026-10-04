package marker

import "testing"

/*
 * The gate: does what follows a black run look like credits?
 *
 * Every Shape below was measured from a real film in ADR 0054's first sample,
 * at +15/+30/+45/+60/+90s after the run the old rule chose, and every verdict
 * was judged by eye from frames around it.
 */

// Role Models, 97.0%: white text on black. Every frame is black and sharp.
var roleModels = []Shape{{90, 14.36}, {95, 7.25}, {92, 10.86}, {88, 16.74}, {87, 18.1}}

// Green Street Hooligans, 91.6%: a fade inside the film, then a dim scene.
// Half black at best — a scene, not a credit roll.
var greenStreet = []Shape{{61, 12.85}, {59, 6.52}, {43, 8.2}, {47, 7.68}, {53, 3.17}}

// Fantasia, 97.7%: a fade to a dark, empty concert stage. Black enough to pass
// every black test, and soft — nothing written on it.
var fantasia = []Shape{{100, 0}, {97, 0.27}, {99, 0.13}, {99, 0.05}, {45, 2.07}}

func TestGateAcceptsTextOnBlack(t *testing.T) {
	if !LooksLikeCredits(roleModels) {
		t.Error("Role Models' credit roll was turned away")
	}
}

func TestGateRejectsASceneAfterAFade(t *testing.T) {
	if LooksLikeCredits(greenStreet) {
		t.Error("a dim scene inside the film was accepted as credits")
	}
}

// The edge half of the rule exists for this film. Black alone accepts it.
func TestGateRejectsBlackWithNothingWrittenOnIt(t *testing.T) {
	if LooksLikeCredits(fantasia) {
		t.Error("a dark empty stage was accepted as credits — the edge test did nothing")
	}
}

/*
 * Near the end of a file fewer frames fit, and the bar scales with them.
 *
 * Three frames need two black and one sharp. Without scaling, a run 50s before
 * the end could never pass — four black frames cannot exist in three — and a
 * short final credit card would always abstain.
 */
func TestGateScalesTheBarToTheFramesThatFit(t *testing.T) {
	three := roleModels[:3]
	if !LooksLikeCredits(three) {
		t.Error("three credits frames were turned away for not being five")
	}
	oneBlack := []Shape{{90, 14}, {40, 14}, {30, 14}}
	if LooksLikeCredits(oneBlack) {
		t.Error("one black frame in three passed; the scaled bar is two")
	}
}

// A frame ffmpeg could not read is a zero Shape, and it must count against.
func TestGateCountsAnUnreadableFrameAgainst(t *testing.T) {
	if LooksLikeCredits(nil) {
		t.Error("no frames at all was accepted as credits")
	}
	unread := []Shape{{90, 14}, {}, {}, {}, {}}
	if LooksLikeCredits(unread) {
		t.Error("one readable frame and four failures was accepted as credits")
	}
}

// The gate is asked in the rule's own order, and the first yes is the answer:
// a rejected fade at 91.6% gives way to the credits behind it at 95.2%.
func TestCreditsMoveToTheFirstCandidateTheGateAccepts(t *testing.T) {
	dur := 6000.0
	runs := []Run{
		{Start: 5496.0, End: 5503.0}, // 91.6%, a fade
		{Start: 5712.0, End: 5720.0}, // 95.2%, the credits
	}
	var asked []float64
	gate := func(at float64) bool {
		asked = append(asked, at)
		return at == 5712.0
	}
	got := CreditsFrom(runs, dur, gate)
	if !got.Found || got.StartMS != 5_712_000 {
		t.Fatalf("got %+v, want the 95.2%% run the gate accepted", got)
	}
	if len(asked) != 2 || asked[0] != 5496.0 {
		t.Errorf("gate asked about %v, want the 91.6%% run first and then the 95.2%%", asked)
	}
}

// No candidate passing is no marker. Falling back to the earliest run would
// hand back exactly the answer the gate exists to doubt.
func TestCreditsAbstainWhenTheGateAcceptsNothing(t *testing.T) {
	dur := 6000.0
	runs := []Run{{Start: 5496.0, End: 5503.0}, {Start: 5712.0, End: 5714.0}}
	if got := CreditsFrom(runs, dur, func(float64) bool { return false }); got.Found {
		t.Errorf("got %+v, want no answer", got)
	}
}

// A long run turned away in the first tier is not put to the gate again in
// the second: same frames, same answer, five more decodes.
func TestCreditsAskAboutEachRunOnce(t *testing.T) {
	dur := 6000.0
	runs := []Run{{Start: 5496.0, End: 5503.0}} // 7s: first tier
	n := 0
	CreditsFrom(runs, dur, func(float64) bool { n++; return false })
	if n != 1 {
		t.Errorf("gate asked %d times about one run, want 1", n)
	}
}

// What ffmpeg 8.1 printed for one credits frame through the worker's chain,
// captured from a test-library film 60s before its end. blackframe also writes
// its number as metadata, `pblack=95`; the parser reads the `pblack:` line.
const shapeStderr = `[Parsed_blackframe_2 @ 00000243ed307580] frame:0 pblack:95 pts:18 t:0.018000 type:P last_keyframe:0
[Parsed_metadata_5 @ 00000243ed3069c0] frame:0    pts:18      pts_time:0.018
[Parsed_metadata_5 @ 00000243ed3069c0] lavfi.blackframe.pblack=95
[Parsed_metadata_5 @ 00000243ed3069c0] lavfi.signalstats.YAVG=5.90024
[Parsed_blackframe_2 @ 00000243ed307580] frame:1 pblack:95 pts:60 t:0.060000 type:B last_keyframe:0
[Parsed_metadata_5 @ 00000243ed3069c0] frame:1    pts:60      pts_time:0.06
[Parsed_metadata_5 @ 00000243ed3069c0] lavfi.blackframe.pblack=95
[Parsed_metadata_5 @ 00000243ed3069c0] lavfi.signalstats.YAVG=5.91458
`

func TestParseShapeReadsRealOutput(t *testing.T) {
	got := ParseShape(shapeStderr)
	if got.PBlack != 95 || got.Edge != 5.90024 {
		t.Errorf("got %+v, want {95 5.90024}", got)
	}
}

// The null muxer pushes a second frame through the filters before it stops.
// The frame that was asked for is the first. (Constructed: in the real
// capture above the two frames happen to agree on pblack.)
func TestParseShapeReadsTheFirstFrameNotTheSecond(t *testing.T) {
	two := "frame:0 pblack:92\nlavfi.signalstats.YAVG=10.86\nframe:1 pblack:15\nlavfi.signalstats.YAVG=3.9\n"
	if got := ParseShape(two); got.PBlack != 92 || got.Edge != 10.86 {
		t.Errorf("got %+v, want {92 10.86}", got)
	}
}

func TestParseShapeOfNothingIsZero(t *testing.T) {
	if got := ParseShape("Error opening input file\n"); got != (Shape{}) {
		t.Errorf("got %+v, want a zero Shape", got)
	}
}
