package marker

import (
	"math"
)

/*
 * Audio fingerprinting, for finding what several episodes have in common.
 *
 * An intro is findable for one reason: every episode of a season carries the
 * same one. That is also why none of this says anything about a film — there
 * is nothing to compare a film with, which is why credits needed a different
 * engine entirely (ADR 0054).
 *
 * The fingerprint is deliberately coarse and deliberately not the audio. Each
 * frame becomes a handful of bits describing the *shape* of its spectrum —
 * which bands hold more energy than their neighbours — so two encodes of the
 * same intro at different volumes, bitrates or codecs still agree. Comparing
 * samples would not survive any of that.
 *
 * Everything here is pure. The ffmpeg call that produces the PCM lives with
 * the worker, so these rules are testable against synthesised audio with no
 * media on disk, the same split the credits detector makes.
 */

const (
	// SampleRate the PCM is expected in. 8 kHz keeps a Nyquist of 4 kHz, which
	// is well above the band range below and cheap to decode a whole episode
	// of. Music and speech are separable long before 4 kHz.
	SampleRate = 8000
	// FrameSize is the analysis window: 4096 samples, 512ms. Long enough that
	// a single drum hit does not dominate a frame, short enough to place a
	// boundary within about half a second.
	FrameSize = 4096
	// HopSize gives 10 frames a second, which is the resolution of every
	// answer this file produces.
	HopSize = 800
	// Bands is how many log-spaced bands the spectrum is reduced to. One bit
	// per adjacent pair, so a frame is Bands-1 bits wide.
	Bands = 17
)

// bandEdges are the FFT bin boundaries of the log-spaced bands, covering
// roughly 200 Hz to 3.5 kHz — the range that carries a theme tune and survives
// any codec anyone would ship television in.
func bandEdges() []int {
	const lo, hi = 200.0, 3500.0
	edges := make([]int, Bands+1)
	for i := 0; i <= Bands; i++ {
		f := lo * math.Pow(hi/lo, float64(i)/float64(Bands))
		edges[i] = int(f * FrameSize / SampleRate)
	}
	return edges
}

/*
 * Fingerprint turns mono PCM into one hash per frame.
 *
 * The bits are comparisons between neighbouring bands rather than the band
 * energies themselves. That is what makes it survive a volume change: scaling
 * every band by the same amount changes no comparison between them.
 */
func Fingerprint(samples []float64) []uint32 {
	if len(samples) < FrameSize {
		return nil
	}
	edges := bandEdges()
	win := hann(FrameSize)

	frames := (len(samples)-FrameSize)/HopSize + 1
	out := make([]uint32, 0, frames)
	re := make([]float64, FrameSize)
	im := make([]float64, FrameSize)

	energy := make([]float64, Bands)
	prevEnergy := make([]float64, Bands)
	havePrev := false

	for start := 0; start+FrameSize <= len(samples); start += HopSize {
		for i := 0; i < FrameSize; i++ {
			re[i] = samples[start+i] * win[i]
			im[i] = 0
		}
		fft(re, im)

		for b := 0; b < Bands; b++ {
			sum := 0.0
			for k := edges[b]; k < edges[b+1] && k < FrameSize/2; k++ {
				sum += re[k]*re[k] + im[k]*im[k]
			}
			// Log, so a quiet passage compares on the same terms as a loud one.
			energy[b] = math.Log1p(sum)
		}

		if havePrev {
			var h uint32
			for b := 0; b < Bands-1; b++ {
				/*
				 * The double difference (Haitsma-Kalker): how this band
				 * compares to its neighbour, against how it compared a frame
				 * ago.
				 *
				 * Differencing across frequency *and* time is what makes the
				 * bit survive re-encoding. The first version differenced only
				 * across frequency, and it was measured on two real episodes:
				 * 8.1% of frames found an exact match and the best ten-second
				 * window still differed by 4.19 bits of 16, against 7.94 for
				 * random alignment. A signal, but far too weak to align on.
				 */
				d := (energy[b] - energy[b+1]) - (prevEnergy[b] - prevEnergy[b+1])
				if d > 0 {
					h |= 1 << uint(b)
				}
			}
			out = append(out, h)
		}
		copy(prevEnergy, energy)
		havePrev = true
	}
	return out
}

/*
 * Phases is how many sub-hop alignments FingerprintPhases produces.
 *
 * A frame hop is 100ms, and two episodes have no reason to begin their intro
 * on the same 100ms grid. Measured against a file matched to itself: shifting
 * the decode by a whole hop costs 0.03 bits of 16, but shifting it by *half* a
 * hop costs 3.08 — on identical audio. That noise floor is what buried the
 * first real-episode results at 4.2 bits, close enough to the 7.9 of random
 * alignment to be useless.
 *
 * So one side is fingerprinted at several phases and the best is taken. Four
 * quarters put the worst-case misalignment at an eighth of a hop rather than a
 * half, and it costs four decodes of one episode rather than a finer hop
 * costing four times the frames on every comparison for ever.
 */
const Phases = 4

// FingerprintPhases returns Phases fingerprints of the same audio, each
// starting a fraction of a hop later than the last.
func FingerprintPhases(samples []float64) [][]uint32 {
	out := make([][]uint32, 0, Phases)
	for p := 0; p < Phases; p++ {
		off := p * HopSize / Phases
		if off >= len(samples) {
			break
		}
		out = append(out, Fingerprint(samples[off:]))
	}
	return out
}

/*
 * BestCommonRun tries every phase of a against b and keeps the best.
 *
 * Only one side needs the sweep: what matters is that some pairing of frame
 * grids lines up, not that both are enumerated.
 */
func BestCommonRun(aPhases [][]uint32, b []uint32, maxTol int) Match {
	best := Match{}
	for _, a := range aPhases {
		if m := CommonRun(a, b, maxTol); m.Frames > best.Frames {
			best = m
		}
	}
	return best
}

func hann(n int) []float64 {
	w := make([]float64, n)
	for i := range w {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
	}
	return w
}

// fft is an in-place radix-2 Cooley-Tukey transform. len(re) must be a power
// of two, which FrameSize is.
func fft(re, im []float64) {
	n := len(re)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		ang := -2 * math.Pi / float64(length)
		wr, wi := math.Cos(ang), math.Sin(ang)
		for i := 0; i < n; i += length {
			cr, ci := 1.0, 0.0
			for j := 0; j < length/2; j++ {
				ur, ui := re[i+j], im[i+j]
				vr := re[i+j+length/2]*cr - im[i+j+length/2]*ci
				vi := re[i+j+length/2]*ci + im[i+j+length/2]*cr
				re[i+j], im[i+j] = ur+vr, ui+vi
				re[i+j+length/2], im[i+j+length/2] = ur-vr, ui-vi
				cr, ci = cr*wr-ci*wi, cr*wi+ci*wr
			}
		}
	}
}

// popcount of the difference: how many bits two frames disagree on.
func hamming(a, b uint32) int {
	x := a ^ b
	n := 0
	for x != 0 {
		x &= x - 1
		n++
	}
	return n
}

// Match is a stretch that two episodes share.
type Match struct {
	// OffsetA and OffsetB are where the shared stretch begins in each, in
	// frames. They differ whenever one episode carries a cold open and the
	// other does not, which is exactly why an intro cannot be assumed to sit
	// at the same timestamp in every episode.
	OffsetA, OffsetB int
	// Frames is how long the agreement lasts.
	Frames int
	// Score is the mean bit disagreement across the run: lower is better.
	Score float64
}

// Seconds converts a frame count to seconds at the fingerprint's resolution.
func Seconds(frames int) float64 {
	return float64(frames) * HopSize / SampleRate
}

/*
 * CommonRun finds the longest stretch two fingerprints share.
 *
 * Alignment first, by voting: every frame of B that matches a frame of A casts
 * a vote for the offset between them, and a genuinely shared passage puts
 * hundreds of votes on one offset while coincidental matches scatter. Only
 * then is the run measured, by walking outwards from the best-supported
 * alignment while the frames keep agreeing.
 *
 * maxTol is how many of the fifteen bits may disagree and still count as the
 * same frame. Zero would demand bit-identical encodes; too high and silence
 * matches silence everywhere.
 */
func CommonRun(a, b []uint32, maxTol int) Match {
	if len(a) == 0 || len(b) == 0 {
		return Match{}
	}
	// Index A by exact hash. Exact here is deliberate — the tolerance is spent
	// on measuring the run, not on finding the alignment, or every offset
	// collects votes and the winner means nothing.
	index := make(map[uint32][]int, len(a))
	for i, h := range a {
		index[h] = append(index[h], i)
	}

	votes := make(map[int]int)
	for j, h := range b {
		for _, i := range index[h] {
			votes[i-j]++
		}
	}
	if len(votes) == 0 {
		return Match{}
	}

	bestOffset, bestVotes := 0, 0
	for off, n := range votes {
		if n > bestVotes {
			bestOffset, bestVotes = off, n
		}
	}

	// Walk the aligned pair and take the longest agreeing stretch.
	best := Match{}
	runStart, runBits, runLen := -1, 0, 0
	flush := func(end int) {
		if runStart >= 0 && runLen > best.Frames {
			best = Match{
				OffsetA: runStart + bestOffset,
				OffsetB: runStart,
				Frames:  runLen,
				Score:   float64(runBits) / float64(runLen),
			}
		}
		_ = end
		runStart, runBits, runLen = -1, 0, 0
	}
	for j := 0; j < len(b); j++ {
		i := j + bestOffset
		if i < 0 || i >= len(a) {
			flush(j)
			continue
		}
		if d := hamming(a[i], b[j]); d <= maxTol {
			if runStart < 0 {
				runStart = j
			}
			runBits += d
			runLen++
		} else {
			flush(j)
		}
	}
	flush(len(b))
	return best
}
