package marker

import "sort"

/*
 * Deciding a season's intros from what its episodes share (ADR 0055).
 *
 * Pure, like the credits rule and for the same reason: the aggregation is
 * where the judgement lives, and it is testable against match structs with no
 * audio, no ffmpeg and no files.
 */

const (
	// IntroMinSeconds is the shortest run worth calling an intro. Below this a
	// shared stretch is a stinger, a network ident, or a coincidence.
	IntroMinSeconds = 8.0
	// IntroMaxSeconds caps it. A run longer than this is two episodes sharing
	// something larger than a title sequence — a recap, a clip show, or the
	// same episode twice on disk.
	IntroMaxSeconds = 180.0
	// IntroHeadSeconds is how much of an episode is fingerprinted. An intro
	// later than this is not one.
	IntroHeadSeconds = 420
	// IntroTolerance is how many of the 16 bits may differ frame to frame.
	IntroTolerance = 3
	/*
	 * IntroStartSlack is how far apart two candidates may begin and still be
	 * called the same intro, in seconds.
	 *
	 * Clustering is on the **start**, and the first version of this clustered
	 * on length, which was wrong for a reason the raw candidates made obvious.
	 * Black Books S1E01 matched its four siblings at lengths 23.4, 15.1, 24.9
	 * and 28.2 seconds — a 13 second spread — while starting at 4, 3, 2 and 0.
	 * A length is the difference of two noisy quantities and carries both
	 * errors; a start carries one.
	 *
	 * This does not contradict what Sunny showed. Position varies wildly
	 * *between* episodes, which is why no marker may assume a fixed timestamp.
	 * Within a single episode every candidate describes that same episode's
	 * intro, so they agree — and the two facts were conflated in the first
	 * rule.
	 */
	IntroStartSlack = 5.0
)

// Candidate is one episode's match against one other episode.
type Candidate struct {
	// StartSec and EndSec are where the shared stretch sits in *this* episode.
	StartSec, EndSec float64
}

// Len is how long the candidate runs.
func (c Candidate) Len() float64 { return c.EndSec - c.StartSec }

// Intro is the decision for one episode.
type Intro struct {
	Found            bool
	StartSec, EndSec float64
	// Agreed is how many of the compared episodes produced a run starting in
	// the same place, and Compared is how many were compared at all.
	Agreed, Compared int
	Confidence       float64
}

/*
 * IntroFrom decides one episode's intro from its matches against its siblings.
 *
 * Every candidate describes *this* episode, so agreement is judged on where
 * they say the shared stretch begins. Nothing here compares one episode's
 * timestamp with another's, and nothing averages across episodes: Sunny's five
 * intros sit between 44s and 193s, and a marker built from that average would
 * land inside the episode in all five cases.
 *
 * A majority is required. One sibling agreeing with another is two files that
 * might share anything — a recap, a rip artefact, an identical cold open — and
 * a title sequence is the thing that recurs across the whole season.
 */
func IntroFrom(cands []Candidate) Intro {
	usable := make([]Candidate, 0, len(cands))
	for _, c := range cands {
		if c.Len() >= IntroMinSeconds && c.Len() <= IntroMaxSeconds && c.StartSec >= 0 {
			usable = append(usable, c)
		}
	}
	if len(usable) == 0 {
		return Intro{Compared: len(cands)}
	}

	// The largest group of candidates that begin near the same place. Sorting
	// by start makes the group a window.
	sort.Slice(usable, func(i, j int) bool { return usable[i].StartSec < usable[j].StartSec })
	bestStart, bestLen := 0, 0
	for i := range usable {
		j := i
		for j < len(usable) && usable[j].StartSec-usable[i].StartSec <= IntroStartSlack {
			j++
		}
		if j-i > bestLen {
			bestStart, bestLen = i, j-i
		}
	}

	// A majority of what was actually compared, not of what survived the
	// filter: three usable candidates out of eight comparisons is three
	// agreeing and five saying nothing, which is not agreement.
	if len(cands) == 0 || bestLen*2 <= len(cands) {
		return Intro{Compared: len(cands), Agreed: bestLen}
	}

	group := usable[bestStart : bestStart+bestLen]
	starts := make([]float64, len(group))
	ends := make([]float64, len(group))
	for i, c := range group {
		starts[i], ends[i] = c.StartSec, c.EndSec
	}
	sort.Float64s(starts)
	sort.Float64s(ends)

	/*
	 * Median of each end independently, rather than one candidate's pair.
	 *
	 * The ends are noisier than the starts — a match runs on into whatever the
	 * two episodes happen to share after the titles, or stops early where they
	 * diverge — so the end is the quantity most worth taking a median of. Both
	 * come from candidates describing this episode, so neither can land
	 * outside it.
	 */
	return Intro{
		Found:      true,
		StartSec:   starts[len(starts)/2],
		EndSec:     ends[len(ends)/2],
		Agreed:     bestLen,
		Compared:   len(cands),
		Confidence: float64(bestLen) / float64(len(cands)),
	}
}

/*
 * IntroPeers picks which siblings an episode is compared against.
 *
 * Not all of them. Pairwise over a 26-episode season is 325 comparisons to
 * learn what four would say, and the decode dominates the cost — so a bounded
 * sample is taken, spread across the season rather than clustered, because a
 * two-part opener shares more than its intro with the episode beside it.
 */
func IntroPeers(n, self, want int) []int {
	if n <= 1 || want <= 0 {
		return nil
	}
	if want > n-1 {
		want = n - 1
	}
	step := n / (want + 1)
	if step < 1 {
		step = 1
	}
	out := make([]int, 0, want)
	for i := 1; len(out) < want && i <= n*2; i++ {
		p := (self + i*step) % n
		if p == self {
			continue
		}
		dup := false
		for _, q := range out {
			if q == p {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, p)
		}
	}
	return out
}
