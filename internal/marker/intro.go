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
	// IntroCardMinSeconds is the shortest title card, believed only when every
	// comparison agrees on where it starts (see IntroFrom). IntroCardMinCompared
	// is how many comparisons that takes: two files agreeing is not a season.
	IntroCardMinSeconds  = 3.0
	IntroCardMinCompared = 3
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
	 * IntroGapFrames is how many consecutive disagreeing frames a run may cross,
	 * at ten frames a second: half a second.
	 *
	 * Measured with introlab on seasons the detector had marked nothing in.
	 * It's Always Sunny S8 went from 0 of 10 episodes to 9 of 10, every one a
	 * ~22s intro whose four comparisons started within a second of each other,
	 * where the strict walk had broken each into pieces of 1.5 to 10 seconds.
	 * On seasons that already worked it tightened rather than moved the answer:
	 * Sunny S3 stayed 8 of 8 with every candidate starting on the same second,
	 * and Black Books S1 went from 5 of 6 to 6 of 6. Two seconds found nothing
	 * half a second did not, and let runs drift a few seconds past the titles.
	 */
	IntroGapFrames = 5
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
	/*
	 * IntroStrongMinSeconds, IntroStrongSlack and IntroStrongAgreed describe a
	 * run so few comparisons need agree, because they agree so closely.
	 *
	 * The majority rule is right when the minority found *nothing*: three of
	 * eight agreeing is three agreeing and five saying nothing. It is wrong when
	 * the minority found something else and far too short to be an intro —
	 * a network ident — because the ident never reaches the clustering step at
	 * all, being under IntroMinSeconds, and so counts only against the episode.
	 *
	 * Sunny S15E02 is the case: 115s+20.6s, 115s+19.9s, 18s+3.9s, 18s+3.9s. Two
	 * comparisons find the intro to within a second and the other two find a
	 * four-second sting, which is 2 of 4 and no majority. Measured with introlab
	 * across the library: TNG S4 11→24 of 25, DS9 S7 15→21 of 25, Sunny S14
	 * 5→8 of 10, Futurama S5 12→15 of 16, Sunny S15 1→3 of 8, and *no* change
	 * to Sunny S3, Black Books S1, The League S2, Voyager S4 or Cowboy Bebop,
	 * nor to the seasons whose right answer is nothing: Storm of the Century,
	 * The League S1, Silicon Valley S1.
	 *
	 * The guard is what keeps it honest. A one-second spread is five times
	 * tighter than IntroStartSlack and twelve seconds is half again
	 * IntroMinSeconds, so this cannot promote the scattered near-misses the
	 * majority rule refuses for good reason. Raising the peer count instead was
	 * measured and rejected: it dilutes the majority, and took Sunny S15 from
	 * 1 to 0.
	 */
	IntroStrongMinSeconds = 12.0
	IntroStrongSlack      = 1.0
	IntroStrongAgreed     = 2
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
	if in := introFrom(cands, IntroMinSeconds, false); in.Found {
		return in
	}
	/*
	 * A title card, which is short, and is believed only unanimously.
	 *
	 * The League has one: about four seconds. introlab over season 2 found every
	 * one of 13 episodes matching all four of its siblings at the same second —
	 * 96, 96, 96, 96; 82, 82, 82, 82 — with runs of 3.6 to 5.9 seconds, and the
	 * detector marked none of them, because eight seconds was the floor. The
	 * floor is right for a majority: three of four agreeing on a short run is
	 * what a shared network sting looks like. Every comparison agreeing on the
	 * same start, at least three of them, is what a title card looks like.
	 */
	majority := introFrom(cands, IntroMinSeconds, false)
	if in := introFrom(cands, IntroCardMinSeconds, true); in.Found {
		return in
	}
	/*
	 * Last: two comparisons that agree closely on a long run, where the rest
	 * found something far too short to be an intro. See IntroStrongMinSeconds.
	 */
	if in := strongPair(cands); in.Found {
		return in
	}
	/*
	 * Refused — and the refusal keeps the best evidence there was.
	 *
	 * Agreed is reported even when nothing is written, so a season that nearly
	 * answered can be told from one that shared nothing at all. Returning this
	 * rule's own empty refusal threw that away: three of eight agreeing came
	 * back as zero, which reads as "no two episodes share anything".
	 */
	return majority
}

/*
 * strongPair accepts IntroStrongAgreed comparisons that begin within
 * IntroStrongSlack of each other on a run of at least IntroStrongMinSeconds.
 *
 * The end is the median of the group's ends, as everywhere else here — which
 * for exactly two is the later of them. An intro's end is the noisier
 * quantity (a match runs on into whatever two episodes happen to share after
 * the titles), so this can overstate where the titles stop. That is tolerable
 * only because nothing skips on these markers: ADR 0055 stores evidence and
 * offers no control. It would need revisiting before anything did.
 */
func strongPair(cands []Candidate) Intro {
	strong := make([]Candidate, 0, len(cands))
	for _, c := range cands {
		if c.StartSec >= 0 && c.Len() >= IntroStrongMinSeconds && c.Len() <= IntroMaxSeconds {
			strong = append(strong, c)
		}
	}
	if len(strong) < IntroStrongAgreed {
		return Intro{Compared: len(cands)}
	}
	/*
	 * And they must still be half of what was compared.
	 *
	 * Without this the rule accepts two agreeing out of five, which
	 * TestIntroRequiresAMajorityOfWhatWasCompared has refused since this ADR
	 * was written, for the reason the majority rule exists: two files sharing
	 * something is not a title sequence that recurs across a season. Closeness
	 * does not make two out of five into evidence about a season — it is the
	 * denominator that separates Sunny S15E02's two of four from it.
	 *
	 * It also means this rule cannot rescue an episode compared against six or
	 * eight peers, which agrees with the measurement that raising the peer
	 * count makes such seasons worse rather than better.
	 */
	if len(cands) > 2*IntroStrongAgreed {
		return Intro{Compared: len(cands)}
	}

	sort.Slice(strong, func(i, j int) bool { return strong[i].StartSec < strong[j].StartSec })
	bestAt, bestLen := 0, 0
	for i := range strong {
		j := i
		for j < len(strong) && strong[j].StartSec-strong[i].StartSec <= IntroStrongSlack {
			j++
		}
		if j-i > bestLen {
			bestAt, bestLen = i, j-i
		}
	}
	if bestLen < IntroStrongAgreed {
		return Intro{Compared: len(cands), Agreed: bestLen}
	}

	group := strong[bestAt : bestAt+bestLen]
	starts := make([]float64, len(group))
	ends := make([]float64, len(group))
	for i, c := range group {
		starts[i], ends[i] = c.StartSec, c.EndSec
	}
	sort.Float64s(starts)
	sort.Float64s(ends)
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
 * IntroCardEarliestSec is how far into an episode a title card must begin.
 *
 * A short stretch every episode shares at 0:00 is the network's ident, which the
 * rip carries, not the show's title. Measured: Silicon Valley S1 and Lanterns
 * S1, both HBO, returned unanimous 5–6 second runs starting at exactly 0.0s,
 * while The League's cards sat between 38 and 139 seconds in every episode of
 * two seasons. A long intro at the very start is untouched — Black Books opens
 * on its titles — because that goes through the majority rule, not this one.
 */
const IntroCardEarliestSec = 2.0

/*
 * IntroFromRule is one rule on its own, for the instrument.
 *
 * introlab compares a proposed rule against the previous one, and calling
 * IntroFrom for both makes every column measure the same thing the moment a
 * change lands — which is how a before-and-after table came to be produced
 * from five identical columns. Spelling the old rule out needs its parts.
 */
func IntroFromRule(cands []Candidate, minSeconds float64, unanimous bool) Intro {
	return introFrom(cands, minSeconds, unanimous)
}

func introFrom(cands []Candidate, minSeconds float64, unanimous bool) Intro {
	usable := make([]Candidate, 0, len(cands))
	for _, c := range cands {
		if unanimous && c.StartSec < IntroCardEarliestSec {
			continue
		}
		if c.Len() >= minSeconds && c.Len() <= IntroMaxSeconds && c.StartSec >= 0 {
			usable = append(usable, c)
		}
	}
	if len(usable) == 0 {
		return Intro{Compared: len(cands)}
	}
	if unanimous && (len(usable) < len(cands) || len(cands) < IntroCardMinCompared) {
		return Intro{Compared: len(cands), Agreed: len(usable)}
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
	// Unanimous means every comparison in the one group: runs that passed the
	// length filter but began somewhere else are dissent, not agreement.
	if len(cands) == 0 || bestLen*2 <= len(cands) || (unanimous && bestLen < len(cands)) {
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
