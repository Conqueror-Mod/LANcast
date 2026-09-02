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
 * have taken. What that establishes is that the rule is *consistent*. Nobody
 * has yet watched a film and written down where its credits begin, so nothing
 * here is known to be *correct*, and that is why the worker stores what it
 * finds and nothing acts on it.
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

/*
 * CreditsFrom picks the boundary out of a film's black runs.
 *
 * The earliest qualifying run, not the longest and not the last. A film's
 * credits start once; everything black after that is within them, and the
 * longest stretch is usually the final fade to nothing.
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
func CreditsFrom(runs []Run, durationSec float64) Credits {
	if durationSec <= 0 {
		return Credits{}
	}
	for _, tier := range []struct {
		minLen     float64
		confidence float64
	}{
		{PreferLen, 0.9},
		{FallbackLen, 0.5},
	} {
		for _, r := range runs {
			if r.Len() < tier.minLen || r.Start > durationSec {
				continue
			}
			at := r.Start / durationSec
			if at < WindowLo || at >= WindowHi {
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
