package marker

import "testing"

/*
 * A short title card, found only when every comparison agrees.
 *
 * Numbers from introlab over The League S2, the season the detector marked
 * nothing in: each episode matched all four siblings starting on the same
 * second, with runs of roughly four seconds.
 */

func card(start float64, lens ...float64) []Candidate {
	out := make([]Candidate, len(lens))
	for i, l := range lens {
		out[i] = Candidate{StartSec: start, EndSec: start + l}
	}
	return out
}

func TestAUnanimousShortTitleCardIsAnIntro(t *testing.T) {
	in := IntroFrom(card(96, 4.1, 4.1, 4.1, 4.1))
	if !in.Found {
		t.Fatal("four of four comparisons agreed on a four-second card at 96s, and it was refused")
	}
	if in.StartSec != 96 || in.Agreed != 4 || in.Confidence != 1 {
		t.Errorf("got %+v, want start 96, agreed 4, confidence 1", in)
	}
}

// Three of four on a short run is what a shared network sting looks like, and
// is exactly what the eight-second floor exists to refuse.
func TestAShortRunWithoutUnanimityIsNot(t *testing.T) {
	cands := append(card(96, 4.1, 4.2, 3.9), Candidate{})
	if in := IntroFrom(cands); in.Found {
		t.Errorf("a short run agreed by three of four was called an intro: %+v", in)
	}
}

// One comparison dissenting on *where* also breaks unanimity.
func TestAShortRunStartingElsewhereInOneComparisonIsNot(t *testing.T) {
	cands := append(card(96, 4.1, 4.2, 3.9), Candidate{StartSec: 193, EndSec: 197.2})
	if in := IntroFrom(cands); in.Found {
		t.Errorf("a short run with one comparison 97s away was called an intro: %+v", in)
	}
}

// Two episodes agreeing with each other is two files that might share anything.
func TestAShortRunNeedsEnoughComparisons(t *testing.T) {
	if in := IntroFrom(card(42, 4.4, 4.4)); in.Found {
		t.Errorf("a short run from only two comparisons was called an intro: %+v", in)
	}
}

func TestBelowTheCardFloorIsNothing(t *testing.T) {
	if in := IntroFrom(card(49, 2.4, 2.4, 2.4, 2.4)); in.Found {
		t.Errorf("a %.1fs run was called a title card: %+v", 2.4, in)
	}
}

// A real intro still goes through the majority rule, unchanged.
func TestALongIntroStillNeedsOnlyAMajority(t *testing.T) {
	cands := append(card(55, 30.2, 30.1, 30.3), Candidate{})
	if in := IntroFrom(cands); !in.Found || in.Agreed != 3 {
		t.Errorf("a 30s intro agreed by three of four was refused: %+v", in)
	}
}
