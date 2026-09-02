package marker

import (
	"math"
	"testing"
)

func cand(start, length float64) Candidate {
	return Candidate{StartSec: start, EndSec: start + length}
}

/*
 * The rule the measurement produced: length decides whether, position decides
 * where. Every fixture below is from real episodes.
 */

// It's Always Sunny season 3, as measured: five runs agreeing on ~30s at
// positions from 44s to 193s. Averaging the positions gives 105s, which is
// inside the episode in all five, so the position must come from this
// episode's own match.
// Within one episode every candidate describes that episode, so their starts
// agree. Across episodes they do not, which is a different fact and the reason
// nothing here averages positions between episodes.
func TestIntroTakesThePositionFromThisEpisodesOwnMatches(t *testing.T) {
	got := IntroFrom([]Candidate{
		cand(55.3, 30.2), cand(57.1, 27.1), cand(54.9, 30.2), cand(56.4, 30.1),
	})
	if !got.Found {
		t.Fatal("Found = false, want an intro — four runs begin within three seconds")
	}
	if got.Agreed != 4 {
		t.Errorf("Agreed = %d, want 4", got.Agreed)
	}
	// The median start of the agreeing group, and a real position in this
	// episode rather than an average of unrelated ones.
	starts := map[float64]bool{55.3: true, 57.1: true, 54.9: true, 56.4: true}
	if !starts[got.StartSec] {
		t.Errorf("StartSec = %.1f, want one of the observed positions", got.StartSec)
	}
	if math.Abs(got.EndSec-got.StartSec-30.0) > 4 {
		t.Errorf("length %.1f, want about 30s", got.EndSec-got.StartSec)
	}
}

func TestIntroRefusesWhenStartsDisagree(t *testing.T) {
	// Four candidates pointing at four different places in the episode. No
	// majority begins anywhere near the same point, so nothing is claimed.
	got := IntroFrom([]Candidate{
		cand(10, 20), cand(60, 22), cand(120, 21), cand(200, 20),
	})
	if got.Found {
		t.Errorf("got %+v, want no intro — the starts do not agree", got)
	}
}

/*
 * The measurement that corrected the rule.
 *
 * Black Books S1E01 against its four siblings: lengths 23.4, 15.1, 24.9 and
 * 28.2 — a 13 second spread — while the starts sit at 4, 3, 2 and 0. Clustering
 * on length refuses this episode; clustering on start finds it, which is the
 * right answer.
 */
func TestIntroFindsItWhereLengthsScatterButStartsAgree(t *testing.T) {
	got := IntroFrom([]Candidate{
		cand(4, 23.4), cand(3, 15.1), cand(2, 24.9), cand(0, 28.2),
	})
	if !got.Found {
		t.Fatalf("got %+v, want an intro — all four begin within four seconds", got)
	}
	if got.StartSec > 5 {
		t.Errorf("StartSec = %.1f, want near the start of the episode", got.StartSec)
	}
	if got.Agreed != 4 {
		t.Errorf("Agreed = %d, want 4", got.Agreed)
	}
}

func TestIntroRequiresAMajorityOfWhatWasCompared(t *testing.T) {
	// Two agreeing out of five compared is two files sharing something, not a
	// title sequence that recurs across a season.
	got := IntroFrom([]Candidate{
		cand(30, 20), cand(31, 20.5), cand(10, 1), cand(12, 2), cand(15, 1.5),
	})
	if got.Found {
		t.Errorf("got %+v, want no intro — 2 of 5 is not a majority", got)
	}
	if got.Agreed != 2 {
		t.Errorf("Agreed = %d, want 2 reported even when refused", got.Agreed)
	}
}

func TestIntroIgnoresRunsTooShortToBeATitleSequence(t *testing.T) {
	got := IntroFrom([]Candidate{cand(10, 3), cand(12, 3.2), cand(9, 2.9)})
	if got.Found {
		t.Errorf("got %+v, want nothing — three seconds is a stinger", got)
	}
}

func TestIntroIgnoresRunsTooLongToBeATitleSequence(t *testing.T) {
	// Two files sharing four minutes are not sharing an intro; most likely
	// they are the same episode twice on disk.
	got := IntroFrom([]Candidate{cand(0, 240), cand(0, 241), cand(0, 239)})
	if got.Found {
		t.Errorf("got %+v, want nothing — that is larger than a title sequence", got)
	}
}

func TestIntroFromNothingIsNotAnIntro(t *testing.T) {
	if got := IntroFrom(nil); got.Found {
		t.Errorf("got %+v, want nothing", got)
	}
}

func TestIntroConfidenceIsTheShareThatAgreed(t *testing.T) {
	got := IntroFrom([]Candidate{
		cand(50, 30), cand(52, 30.4), cand(51, 29.8), cand(80, 3),
	})
	if !got.Found {
		t.Fatal("want an intro")
	}
	if math.Abs(got.Confidence-0.75) > 0.001 {
		t.Errorf("Confidence = %.3f, want 0.75 — three of four agreed", got.Confidence)
	}
}

func TestPeersAreSpreadAcrossTheSeason(t *testing.T) {
	// A two-part opener shares more than its intro with the episode beside it,
	// so neighbours are a bad sample.
	peers := IntroPeers(26, 0, 4)
	if len(peers) != 4 {
		t.Fatalf("got %d peers, want 4", len(peers))
	}
	for _, p := range peers {
		if p == 0 {
			t.Error("an episode was compared against itself")
		}
	}
	if peers[0] == 1 {
		t.Errorf("first peer is the adjacent episode: %v", peers)
	}
}

func TestPeersCannotExceedTheSeason(t *testing.T) {
	if got := IntroPeers(3, 0, 10); len(got) != 2 {
		t.Errorf("got %v, want the other two episodes only", got)
	}
	if got := IntroPeers(1, 0, 4); len(got) != 0 {
		t.Errorf("got %v, want none — one episode has nothing to compare with", got)
	}
}
