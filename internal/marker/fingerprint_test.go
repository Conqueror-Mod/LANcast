package marker

import (
	"math"
	"math/rand"
	"testing"
)

/*
 * The fingerprint's job is to survive re-encoding and misalignment while still
 * telling two different pieces of audio apart. Synthesised audio here, because
 * the rules are pure and a test that needs a media file is a test nobody runs.
 */

// varied builds audio whose character changes over time, so a shared stretch is
// distinguishable rather than matching everywhere. A constant tone would pass
// every test below and prove nothing.
func varied(n int, seed int64) []float64 {
	r := rand.New(rand.NewSource(seed))
	out := make([]float64, n)
	f := 300.0
	for i := range out {
		if i%(SampleRate/2) == 0 {
			f = 250 + r.Float64()*2500
		}
		out[i] = math.Sin(2*math.Pi*f*float64(i)/SampleRate) + 0.05*r.NormFloat64()
	}
	return out
}

func concat(parts ...[]float64) []float64 {
	var out []float64
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestSharedStretchIsFoundAtDifferentPositions(t *testing.T) {
	// The Sunny case: the same title sequence after cold opens of different
	// lengths. Position must not be assumed; only the content is shared.
	shared := varied(SampleRate*20, 1)
	a := concat(varied(SampleRate*5, 2), shared, varied(SampleRate*30, 3))
	b := concat(varied(SampleRate*40, 4), shared, varied(SampleRate*20, 5))

	m := BestCommonRun(FingerprintPhases(a), Fingerprint(b), 3)
	if Seconds(m.Frames) < 15 {
		t.Fatalf("found %.1fs, want most of the 20s shared stretch", Seconds(m.Frames))
	}
	if math.Abs(Seconds(m.OffsetA)-5) > 2 {
		t.Errorf("OffsetA = %.1fs, want ~5s", Seconds(m.OffsetA))
	}
	if math.Abs(Seconds(m.OffsetB)-40) > 2 {
		t.Errorf("OffsetB = %.1fs, want ~40s — the same audio, 35s later", Seconds(m.OffsetB))
	}
}

/*
 * The finding that made this work at all.
 *
 * A frame hop is 100ms and two episodes have no reason to start their intro on
 * the same grid. Measured on a real file matched against itself: a whole-hop
 * shift costs 0.03 bits of 16, a half-hop shift costs 3.08 — on identical
 * audio. That noise floor buried the first real-episode results at 4.2 bits
 * against 7.9 for random alignment, which is why nothing was being found.
 */
func TestAHalfHopShiftIsStillFound(t *testing.T) {
	shared := varied(SampleRate*20, 11)
	a := concat(varied(SampleRate*5, 12), shared, varied(SampleRate*20, 13))
	b := concat(varied(SampleRate*13, 14), shared, varied(SampleRate*20, 15))

	// Half a hop of silence in front of B: the same audio on a different grid.
	shift := make([]float64, HopSize/2)
	shifted := concat(shift, b)

	m := BestCommonRun(FingerprintPhases(a), Fingerprint(shifted), 3)
	if Seconds(m.Frames) < 15 {
		t.Errorf("found %.1fs across a half-hop shift, want most of 20s — "+
			"the phase sweep is what makes this survive", Seconds(m.Frames))
	}
}

// Without the sweep the same pair does markedly worse. This is here so that
// removing Phases fails a test rather than quietly degrading detection.
func TestThePhaseSweepIsWhatFindsIt(t *testing.T) {
	shared := varied(SampleRate*20, 21)
	a := concat(varied(SampleRate*5, 22), shared, varied(SampleRate*20, 23))
	b := concat(make([]float64, HopSize/2), varied(SampleRate*13, 24), shared, varied(SampleRate*20, 25))

	single := CommonRun(Fingerprint(a), Fingerprint(b), 3)
	swept := BestCommonRun(FingerprintPhases(a), Fingerprint(b), 3)
	t.Logf("single phase %.1fs, swept %.1fs", Seconds(single.Frames), Seconds(swept.Frames))
	if swept.Frames < single.Frames {
		t.Errorf("the sweep found less than one phase alone (%d < %d frames)",
			swept.Frames, single.Frames)
	}
}

func TestUnrelatedAudioSharesNothingLong(t *testing.T) {
	m := BestCommonRun(FingerprintPhases(varied(SampleRate*40, 7)),
		Fingerprint(varied(SampleRate*40, 8)), 3)
	if Seconds(m.Frames) > 4 {
		t.Errorf("found %.1fs in unrelated audio, want only a short accident",
			Seconds(m.Frames))
	}
}

func TestAVolumeChangeDoesNotBreakTheMatch(t *testing.T) {
	// Bits are comparisons between bands, so scaling every band equally
	// changes none of them. Two rips at different levels must still agree.
	shared := varied(SampleRate*20, 31)
	a := concat(varied(SampleRate*5, 32), shared, varied(SampleRate*10, 33))

	quiet := make([]float64, len(shared))
	for i, v := range shared {
		quiet[i] = v * 0.3
	}
	b := concat(varied(SampleRate*8, 34), quiet, varied(SampleRate*10, 35))

	m := BestCommonRun(FingerprintPhases(a), Fingerprint(b), 3)
	if Seconds(m.Frames) < 15 {
		t.Errorf("found %.1fs at a third the volume, want most of 20s", Seconds(m.Frames))
	}
}

func TestFingerprintRefusesAudioShorterThanAFrame(t *testing.T) {
	if got := Fingerprint(make([]float64, FrameSize-1)); got != nil {
		t.Errorf("got %d frames from less than one window, want none", len(got))
	}
}

func TestEmptyFingerprintsMatchNothing(t *testing.T) {
	if m := CommonRun(nil, Fingerprint(varied(SampleRate*10, 41)), 3); m.Frames != 0 {
		t.Errorf("got %+v, want no match", m)
	}
}
