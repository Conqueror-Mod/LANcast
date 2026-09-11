package marker

import "testing"

/*
 * A run crosses a short disagreement.
 *
 * Built from hashes directly, so the walk is tested without the fingerprint's
 * own noise deciding the answer. `same` agrees frame for frame; a flipped frame
 * disagrees in every bit, well past any tolerance.
 */

func same(n int, seed uint32) []uint32 {
	out := make([]uint32, n)
	x := seed | 1
	for i := range out {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		out[i] = x & 0xFFFF
	}
	return out
}

func flip(h []uint32, at ...int) []uint32 {
	out := append([]uint32(nil), h...)
	for _, i := range at {
		out[i] ^= 0xFFFF
	}
	return out
}

// The failure: one noisy frame in the middle of a 300-frame intro. Strictly,
// the longest piece is half of it; bridged, it is all of it.
func TestOneNoisyFrameNoLongerSplitsAnIntro(t *testing.T) {
	a := same(300, 7)
	b := flip(a, 150)

	strict := CommonRun(a, b, 3)
	if strict.Frames >= 300 {
		t.Fatalf("strict run crossed a disagreeing frame (%d frames); the test proves nothing", strict.Frames)
	}
	bridged := CommonRunBridging(a, b, 3, 5)
	if bridged.Frames != 300 {
		t.Errorf("bridged run = %d frames, want the whole 300", bridged.Frames)
	}
	if bridged.OffsetA != 0 || bridged.OffsetB != 0 {
		t.Errorf("bridged run starts at %d/%d, want 0/0", bridged.OffsetA, bridged.OffsetB)
	}
}

// A gap longer than the allowance still ends the run: this bridges noise over
// a title sequence, not the gap between two different things that happen to
// share a little.
func TestAGapLongerThanTheAllowanceEndsTheRun(t *testing.T) {
	a := same(300, 9)
	b := flip(a, 100, 101, 102, 103, 104, 105, 106, 107)

	m := CommonRunBridging(a, b, 3, 5)
	if m.Frames >= 300 {
		t.Errorf("bridged an 8-frame gap with an allowance of 5 (%d frames)", m.Frames)
	}
	if m.Frames != 192 {
		t.Errorf("run = %d frames, want the 192 after the gap", m.Frames)
	}
}

// Disagreement after the last agreeing frame is not part of the run, however
// short: bridging must not extend an intro into what follows it.
func TestATrailingGapIsNotCountedAsAgreement(t *testing.T) {
	a := same(300, 13)
	b := flip(a, 297, 298, 299)

	m := CommonRunBridging(a, b, 3, 5)
	if m.Frames != 297 {
		t.Errorf("run = %d frames, want 297 — the three trailing disagreements are not agreement", m.Frames)
	}
}

// Zero allowance is the original walk exactly, so the change cannot alter what
// the strict path answered.
func TestZeroAllowanceIsTheStrictWalk(t *testing.T) {
	a := same(400, 17)
	b := flip(a, 50, 51, 200, 333)
	if s, z := CommonRun(a, b, 3), CommonRunBridging(a, b, 3, 0); s != z {
		t.Errorf("CommonRun %+v != CommonRunBridging(gap 0) %+v", s, z)
	}
}

// Real audio through the real fingerprint: a burst of unrelated sound laid over
// the middle of a shared stretch, the shape of a line of dialogue over titles.
func TestABurstOverTheTitlesIsBridgedInRealAudio(t *testing.T) {
	shared := varied(SampleRate*20, 51)
	over := append([]float64(nil), shared...)
	burst := varied(SampleRate/4, 52) // a quarter second
	mid := SampleRate * 10
	for i, v := range burst {
		over[mid+i] += 3 * v
	}
	a := concat(varied(SampleRate*5, 53), shared, varied(SampleRate*10, 54))
	b := concat(varied(SampleRate*9, 55), over, varied(SampleRate*10, 56))

	strict := BestCommonRun(FingerprintPhases(a), Fingerprint(b), IntroTolerance)
	bridged := BestCommonRunBridging(FingerprintPhases(a), Fingerprint(b), IntroTolerance, IntroGapFrames)
	t.Logf("strict %.1fs, bridged %.1fs", Seconds(strict.Frames), Seconds(bridged.Frames))
	if Seconds(bridged.Frames) < 17 {
		t.Errorf("bridged %.1fs across a quarter-second burst, want most of 20s", Seconds(bridged.Frames))
	}
	if bridged.Frames < strict.Frames {
		t.Errorf("bridging found less than the strict walk (%d < %d)", bridged.Frames, strict.Frames)
	}
}
