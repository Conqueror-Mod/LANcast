// Package marker finds where a film or an episode stops being itself:
// the point the credits begin, and — from stage 2 — where an intro ends.
//
// Process execution is split from the decision, the same split probe makes and
// for the same reason. Everything in this file is pure: it turns captured
// ffmpeg output into a boundary, so every rule below is tested against fixtures
// with no ffmpeg installed and no media on disk. A change that made the
// decision require a live process would make dozens of cases untestable in
// milliseconds.
package marker

import (
	"math"
	"regexp"
	"strconv"
)

// Run is one stretch of the file where something was continuously true —
// black, or silent. Times are absolute seconds from the start of the file.
type Run struct {
	Start float64
	End   float64
}

// Len is how long the run lasted.
func (r Run) Len() float64 { return r.End - r.Start }

/*
 * The rule, and the numbers it was tuned and then tested on (ADR 0054).
 *
 * Below WindowLo a black stretch is a scene fade rather than a boundary: five
 * films in the first sample picked one, The Beastmaster at 77.9% and Blow at
 * 77.6%, and every one moved to a plausible position once the search started
 * at 88%. Above WindowHi it is the file ending — the rule that took the *last*
 * black run put 20 of its 33 answers there.
 *
 * PreferLen is a confident boundary; FallbackLen is accepted only when no
 * confident one exists, and the difference is recorded on the marker as its
 * confidence rather than thrown away.
 *
 * These were derived from 40 films and then tested, frozen, against 40 the
 * rule had never seen: 38 of 40 answered, median 94.3% against 94.1%, and not
 * one answer pressed against WindowLo — which is the shape overfitting would
 * have taken. What that establishes is that the rule is *consistent*, not that
 * it is correct — and when its answers were finally checked frame by frame,
 * one in five was a fade inside the film. That is what the gate below is for.
 *
 * FallbackLen is deliberately not lowered to 1.5s. It would answer one more
 * film in the held-out sample, and choosing it for that reason would mean
 * choosing it by looking at the held-out set, which is the one thing that set
 * cannot survive.
 */
const (
	WindowLo    = 0.88
	WindowHi    = 0.995
	PreferLen   = 5.0
	FallbackLen = 2.0
)

var reBlack = regexp.MustCompile(`black_start:(\d+\.?\d*)\s+black_end:(\d+\.?\d*)`)

// ParseBlackDetect reads ffmpeg's blackdetect lines out of its stderr.
//
// offset is where the scan was seeked to: the filter reports times relative to
// the seek point, and a candidate six minutes into the scan is not the same
// fact as one six minutes into the film. Shifting here rather than at the call
// site is deliberate — it is the mistake this signature exists to prevent.
func ParseBlackDetect(stderr string, offset float64) []Run {
	m := reBlack.FindAllStringSubmatch(stderr, -1)
	out := make([]Run, 0, len(m))
	for _, g := range m {
		start, err1 := strconv.ParseFloat(g[1], 64)
		end, err2 := strconv.ParseFloat(g[2], 64)
		if err1 != nil || err2 != nil || end < start {
			continue
		}
		out = append(out, Run{Start: offset + start, End: offset + end})
	}
	return out
}

// Credits is where the credits begin, or Found false when no run qualifies.
type Credits struct {
	Found      bool
	StartMS    int64
	Confidence float64
}

// Shape is what one frame looks like, in the two numbers the gate reads.
type Shape struct {
	// PBlack is the percentage of pixels darker than 32 of 255.
	PBlack int
	// Edge is the mean of an edge map of the frame, 0 to 255.
	Edge float64
}

/*
 * The gate, and what it was tuned and then tested on (ADR 0054, 2026-10-03
 * amendment).
 *
 * The black-run rule alone was consistent and, once somebody looked, wrong one
 * time in five: 9 of 40 films in the first sample put the marker on a fade
 * inside the film, with the third act still to run. A black run says only that
 * the picture went dark. What follows it is what says whether the film ended.
 *
 * Credits are mostly black *and* sharp: lines of text on a dark ground. A dark
 * scene is black but soft; nearly every other scene is not black. So a
 * candidate is accepted only if, of the frames at GateOffsets after it, at
 * least GateBlack are PBlackMin% near-black and at least GateEdge carry edge
 * density EdgeMin. The edge half is what turned away Fantasia, whose fade goes
 * to a dark, empty concert stage.
 *
 * Tuned on the first 40 films: early answers fell from 9 to 1. Then frozen and
 * run against 40 it had never seen: early answers fell from 5 to **0**, at a
 * cost of 9 abstentions, 4 of which the old rule had right — credits drawn as
 * comic panels, credits too dim to register, a short whose end card is mostly
 * logo. Across all 80, early went from 14 to 1. The one left is a dark,
 * computer-animated film whose last scene looks, to these two numbers, exactly
 * like text on black.
 *
 * Every answer in both samples was judged by eye from frames around it, not
 * from the numbers that chose it.
 *
 * Abstaining is the cheap failure: a film with no marker shows no button. An
 * early marker drops somebody out of the third act, and that asymmetry is what
 * these numbers are set by.
 */
const (
	PBlackMin = 80
	GateBlack = 4
	EdgeMin   = 2.0
	GateEdge  = 2
)

// GateOffsets are the seconds after a candidate at which frames are read.
var GateOffsets = []float64{15, 30, 45, 60, 90}

/*
 * LooksLikeCredits reports whether frames read after a candidate look like
 * credits rather than a scene.
 *
 * shapes holds one entry for each of GateOffsets that fell before the end of
 * the file, in order. A candidate near the end has fewer, and the bar scales
 * with what could be read: three frames need two black and one sharp, rather
 * than four and two that cannot exist. A frame that could not be read is
 * passed as a zero Shape — it counts against the candidate, never for it.
 */
func LooksLikeCredits(shapes []Shape) bool {
	if len(shapes) == 0 {
		return false
	}
	scale := float64(len(shapes)) / float64(len(GateOffsets))
	needBlack := int(math.Round(GateBlack * scale))
	needEdge := int(math.Round(GateEdge * scale))
	black, sharp := 0, 0
	for _, s := range shapes {
		if s.PBlack >= PBlackMin {
			black++
		}
		if s.Edge >= EdgeMin {
			sharp++
		}
	}
	return black >= needBlack && sharp >= needEdge
}

var (
	rePBlack = regexp.MustCompile(`pblack:(\d+)`)
	reEdge   = regexp.MustCompile(`lavfi\.signalstats\.YAVG=(\d+\.?\d*)`)
)

/*
 * ParseShape reads one frame's Shape out of ffmpeg's stderr.
 *
 * The first frame reported, not the last: the null muxer pushes a second frame
 * through the filters before it stops, and the frame that was asked for is the
 * first. A missing number reads as zero, which LooksLikeCredits counts against
 * the candidate.
 */
func ParseShape(stderr string) Shape {
	var s Shape
	if m := rePBlack.FindStringSubmatch(stderr); m != nil {
		s.PBlack, _ = strconv.Atoi(m[1])
	}
	if m := reEdge.FindStringSubmatch(stderr); m != nil {
		s.Edge, _ = strconv.ParseFloat(m[1], 64)
	}
	return s
}

/*
 * CreditsFrom picks the boundary out of a film's black runs.
 *
 * The earliest qualifying run, not the longest and not the last. A film's
 * credits start once; everything black after that is within them, and the
 * longest stretch is usually the final fade to nothing.
 *
 * gate is asked about each candidate in that order, and the first it accepts
 * is the answer. When it accepts none the film has no marker — the earliest
 * run is not used as a fallback, because the earliest run is exactly what the
 * gate exists to doubt. A nil gate accepts everything, which is the rule as it
 * stood before the gate; the worker always passes one.
 *
 * durationSec is the file's real length. It must come from ffprobe and never
 * from media_item.duration_ms, which was TMDB's runtime on every film in a
 * real library until v0.8.51 — read against that, The Outsiders' black frames
 * landed at 120% of "its" own length.
 *
 * Abstaining is a real answer. A film whose credits begin on a cut rather than
 * a fade has nothing here to detect, and saying so is better than pointing at
 * the last four seconds of the file.
 */
func CreditsFrom(runs []Run, durationSec float64, gate func(startSec float64) bool) Credits {
	if durationSec <= 0 {
		return Credits{}
	}
	for _, tier := range []struct {
		minLen, maxLen float64
		confidence     float64
	}{
		{PreferLen, math.Inf(1), 0.9},
		// Below PreferLen only: a longer run was already put to the gate
		// above, and asking again costs five more decodes for the same answer.
		{FallbackLen, PreferLen, 0.5},
	} {
		for _, r := range runs {
			if r.Len() < tier.minLen || r.Len() >= tier.maxLen || r.Start > durationSec {
				continue
			}
			at := r.Start / durationSec
			if at < WindowLo || at >= WindowHi {
				continue
			}
			if gate != nil && !gate(r.Start) {
				continue
			}
			return Credits{
				Found:      true,
				StartMS:    int64(r.Start * 1000),
				Confidence: tier.confidence,
			}
		}
	}
	return Credits{}
}

// ScanFrom is where a tail scan should begin for a file of this length.
//
// A quarter is generous — the boundary has never been observed before 88% —
// but the cost of decoding is linear in this number and the margin is what
// makes an unusually long credit roll visible rather than assumed away.
func ScanFrom(durationSec float64) float64 {
	if durationSec <= 0 {
		return 0
	}
	return durationSec * 0.75
}
