package meta

import (
	"strings"

	"lancast/internal/media"
)

// Match states, mirrored in media_item.match_state.
const (
	StateMatched   = "matched"
	StateReview    = "review"
	StateUnmatched = "unmatched"
	StateLocked    = "locked"

	// StateLocal means the identity came from a local source — an NFO the
	// user or another tool wrote. There is nothing to review: the user
	// already said what this is, and reporting it as "no match found" would
	// bury real problems under items that are perfectly resolved.
	StateLocal = "local"
)

// Confidence thresholds. The middle band carries the design: a low-confidence
// match is usually still right, so applying it is a good default — but it is
// recorded as uncertain rather than presented as fact.
const (
	ThresholdAuto   = 0.85 // at or above: applied silently
	ThresholdReview = 0.55 // at or above: applied, and flagged for review
)

// Scoring weights. Popularity is a tiebreak only and must never rescue a weak
// title match — that is how servers confidently match obscure files to
// blockbusters.
const (
	weightTitle      = 0.60
	weightYear       = 0.30
	weightPopularity = 0.10
)

// StateFor maps a score to the match state it produces.
func StateFor(score float64) string {
	switch {
	case score >= ThresholdAuto:
		return StateMatched
	case score >= ThresholdReview:
		return StateReview
	default:
		return StateUnmatched
	}
}

// Score rates a candidate against a query, 0.0 to 1.0.
func Score(q Query, c Candidate) float64 {
	if q.Kind == KindEpisode {
		return scoreEpisode(q, c)
	}

	// An absent year scores neutral (half credit) rather than redistributing
	// its weight onto the title. That distinction matters: with no year, an
	// exact title match is genuinely ambiguous — "Solaris" is two different
	// films — so the result lands in the review band instead of auto-applying.
	// Inflating the title weight to compensate would manufacture confidence
	// the evidence does not support.
	return clamp(titleScore(q.Title, c.Title)*weightTitle +
		yearScore(q.Year, c.Year)*weightYear +
		popularityScore(c.Popularity)*weightPopularity)
}

// scoreEpisode matches on the parent series name. Season and episode numbers
// are not scored — they are looked up exactly when fetching the episode, so a
// numbering mismatch is not a weak match, it is a different episode and never
// reaches scoring at all.
func scoreEpisode(q Query, c Candidate) float64 {
	series := q.Series
	if series == "" {
		series = q.Title
	}
	return clamp(titleScore(series, c.Title)*(weightTitle+weightYear) +
		popularityScore(c.Popularity)*weightPopularity)
}

// SortTitleOf derives the sort key for a title. It exists so callers outside
// this package do not reach past it into internal/media and grow a second
// opinion about normalization.
func SortTitleOf(title string) string { return media.SortTitle(title) }

// titleScore compares normalized titles. It reuses the normalizer in
// internal/media so scanning and matching cannot disagree about what a title
// is — two normalizers that drift is a bug factory (see CLAUDE.md).
func titleScore(a, b string) float64 {
	na, nb := normalize(a), normalize(b)
	if na == "" || nb == "" {
		return 0
	}
	if na == nb {
		return 1
	}
	return jaroWinkler(na, nb)
}

// normalize lowercases, drops leading articles, and strips punctuation so that
// "Ocean's Eleven" and "Oceans Eleven" compare equal.
func normalize(s string) string {
	s = media.SortTitle(s)
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevSpace = false
		case r == ' ':
			if !prevSpace && b.Len() > 0 {
				b.WriteRune(' ')
				prevSpace = true
			}
		default:
			// Punctuation is dropped entirely rather than becoming a space, so
			// "Ocean's" normalizes to "oceans" and not "ocean s".
		}
	}
	return strings.TrimSpace(b.String())
}

// yearScore rewards exact release years and punishes distant ones hard. A
// two-year gap is almost always a different film with a similar name.
func yearScore(want, got int) float64 {
	if want == 0 || got == 0 {
		return 0.5
	}
	switch d := abs(want - got); {
	case d == 0:
		return 1.0
	case d == 1:
		return 0.8
	case d == 2:
		return 0.25
	default:
		return 0
	}
}

// popularityScore compresses an unbounded popularity figure into 0..1 with
// diminishing returns, so a runaway blockbuster cannot dominate the tiebreak.
func popularityScore(p float64) float64 {
	if p <= 0 {
		return 0
	}
	return p / (p + 20)
}

// jaroWinkler returns similarity in 0..1, favouring strings that agree on their
// opening characters — which is how release names usually differ from titles.
func jaroWinkler(a, b string) float64 {
	j := jaro(a, b)
	if j < 0.7 {
		return j
	}
	prefix := 0
	for i := 0; i < min(4, min(len(a), len(b))); i++ {
		if a[i] != b[i] {
			break
		}
		prefix++
	}
	return j + float64(prefix)*0.1*(1-j)
}

func jaro(a, b string) float64 {
	if a == b {
		return 1
	}
	la, lb := len(a), len(b)
	if la == 0 || lb == 0 {
		return 0
	}

	window := max(la, lb)/2 - 1
	if window < 0 {
		window = 0
	}

	aMatched := make([]bool, la)
	bMatched := make([]bool, lb)
	matches := 0

	for i := 0; i < la; i++ {
		lo := max(0, i-window)
		hi := min(i+window+1, lb)
		for k := lo; k < hi; k++ {
			if bMatched[k] || a[i] != b[k] {
				continue
			}
			aMatched[i] = true
			bMatched[k] = true
			matches++
			break
		}
	}
	if matches == 0 {
		return 0
	}

	transpositions := 0
	k := 0
	for i := 0; i < la; i++ {
		if !aMatched[i] {
			continue
		}
		for !bMatched[k] {
			k++
		}
		if a[i] != b[k] {
			transpositions++
		}
		k++
	}

	m := float64(matches)
	return (m/float64(la) + m/float64(lb) + (m-float64(transpositions)/2)/m) / 3
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
