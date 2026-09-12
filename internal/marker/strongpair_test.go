package marker

import "testing"

/*
 * Two comparisons that agree closely on a long run.
 *
 * The numbers are from introlab over the real library. It's Always Sunny S15E02
 * returned 115s+20.6s, 115s+19.9s, 18s+3.9s, 18s+3.9s: two comparisons finding
 * the intro to within a second, two finding a four-second network sting. That
 * is 2 of 4, no majority, and nothing was written — while the two short ones
 * never reached the clustering step at all, being under IntroMinSeconds.
 */

// strongCands builds candidates at one start, each with its own length.
func strongCands(start float64, lens ...float64) []Candidate {
	out := make([]Candidate, len(lens))
	for i, l := range lens {
		out[i] = Candidate{StartSec: start, EndSec: start + l}
	}
	return out
}

func TestTwoComparisonsAgreeingCloselyOnALongRunIsAnIntro(t *testing.T) {
	cands := append(strongCands(115, 20.6, 19.9), strongCands(18, 3.9, 3.9)...)
	in := IntroFrom(cands)
	if !in.Found {
		t.Fatal("two comparisons agreeing to within a second on a ~20s run were refused, " +
			"because the other two found a four-second sting")
	}
	if in.StartSec != 115 || in.Agreed != 2 || in.Compared != 4 {
		t.Errorf("got %+v, want start 115 agreed 2 of 4", in)
	}
}

// Twelve seconds is the floor for this rule, well above IntroMinSeconds: two
// agreeing on something shorter is what a shared sting looks like.
func TestTwoAgreeingOnAShortRunIsNotEnough(t *testing.T) {
	cands := append(strongCands(115, 9.0, 9.0), strongCands(18, 3.9, 3.9)...)
	if in := IntroFrom(cands); in.Found {
		t.Errorf("two agreeing on a nine-second run was called an intro: %+v", in)
	}
}

// A second of slack, not five. Candidates that start further apart than that
// are not describing the same stretch closely enough for two to decide it.
func TestTwoStartingSecondsApartIsNotEnough(t *testing.T) {
	cands := []Candidate{
		{StartSec: 115, EndSec: 135},
		{StartSec: 118, EndSec: 138},
		{StartSec: 18, EndSec: 21.9},
		{StartSec: 18, EndSec: 21.9},
	}
	if in := IntroFrom(cands); in.Found {
		t.Errorf("candidates three seconds apart were called one intro: %+v", in)
	}
}

/*
 * Two agreeing is half of four and a minority of five, and closeness does not
 * change that. This is the boundary of the rule, and it is the case
 * TestIntroRequiresAMajorityOfWhatWasCompared has refused since ADR 0055 was
 * written — the first version of this rule broke it, accepting 2 of 5 at a
 * one-second spread.
 */
func TestTwoAgreeingIsNotEnoughOutOfFive(t *testing.T) {
	cands := append(strongCands(115, 20.6, 19.9),
		Candidate{StartSec: 18, EndSec: 21.9},
		Candidate{StartSec: 20, EndSec: 23},
		Candidate{StartSec: 25, EndSec: 28})
	if in := IntroFrom(cands); in.Found {
		t.Errorf("two of five was called an intro: %+v", in)
	}
}

func TestOneLongRunAloneIsNotEnough(t *testing.T) {
	cands := append(strongCands(115, 20.6), strongCands(18, 3.9, 3.9, 3.9)...)
	if in := IntroFrom(cands); in.Found {
		t.Errorf("a single comparison decided an intro: %+v", in)
	}
}

/*
 * The majority rule still decides where it can, and says so: three agreeing is
 * reported as three, not as a pair. This is the ordering — majority, then the
 * unanimous card, then this — and it matters because the three rules answer
 * with different confidence.
 */
func TestTheMajorityRuleStillDecidesWhereItCan(t *testing.T) {
	in := IntroFrom(strongCands(55, 30.2, 30.1, 30.3, 30.0))
	if !in.Found || in.Agreed != 4 {
		t.Errorf("got %+v, want the majority rule agreeing 4 of 4", in)
	}
}

/*
 * The negative control that matters: Silicon Valley S1, whose episodes share
 * only the network ident at 0:00 — four unanimous runs of about six seconds.
 * The card rule refuses it for starting at zero, and this rule must refuse it
 * for being short, or a season with no intro gains one.
 */
func TestANetworkIdentIsStillRefused(t *testing.T) {
	if in := IntroFrom(strongCands(0, 6.5, 6.5, 6.2, 6.4)); in.Found {
		t.Errorf("the Silicon Valley ident was called an intro: %+v", in)
	}
}
