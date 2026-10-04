package marker

import "testing"

/*
 * One case per branch of EpisodeCreditsFrom, each from a real episode judged
 * by eye. Seconds are rounded from the measurements.
 */

func black(sec float64) Credits {
	return Credits{Found: true, StartMS: int64(sec * 1000), Confidence: 0.9}
}

func ending(sec float64) Intro {
	return Intro{Found: true, StartSec: sec, EndSec: sec + 30, Agreed: 4, Compared: 4, Confidence: 1}
}

func never(t *testing.T) func(float64) bool {
	return func(float64) bool {
		t.Error("the gate was asked about a case that does not need it")
		return false
	}
}

// Futurama: the two agree to within seconds. The black run is exact to the
// frame; the music can start a beat early over the final shot.
func TestEndingAgreementTakesTheBlackRun(t *testing.T) {
	got := EpisodeCreditsFrom(black(1262), ending(1266), 1302, never(t))
	if !got.Found || got.StartMS != 1_262_000 || got.Source != SourceUngated {
		t.Errorf("got %+v, want the black run at 1262s", got)
	}
}

// Voyager S5E14: a warp-out to black at 94.6%, then two minutes more of the
// episode. The closing theme starts at 98.2%, and the frames after the black
// run are a scene. The music is right.
func TestEndingOverrulesAFadeTheGateRejects(t *testing.T) {
	asked := 0
	got := EpisodeCreditsFrom(black(2611), ending(2711), 2760, func(at float64) bool {
		asked++
		if at != 2611 {
			t.Errorf("gate asked about %v, want the black run", at)
		}
		return false
	})
	if got.StartMS != 2_711_000 || got.Source != SourceEnding {
		t.Errorf("got %+v, want the closing theme at 2711s", got)
	}
	if asked != 1 {
		t.Errorf("gate asked %d times, want 1", asked)
	}
}

// Sunny S4E13: credits as text on black well before the music the season
// shares. The gate passes them, and the black run stands.
func TestEndingYieldsToTextOnBlack(t *testing.T) {
	got := EpisodeCreditsFrom(black(1252), ending(1290), 1302, func(float64) bool { return true })
	if got.StartMS != 1_252_000 || got.Source != SourceUngated {
		t.Errorf("got %+v, want the black run the gate accepted", got)
	}
}

// Death Parade: the black run is the end of the closing song; the song itself
// began a minute earlier, and that is where the credits start.
func TestEndingEarlierThanTheBlackRunWins(t *testing.T) {
	got := EpisodeCreditsFrom(black(1338), ending(1271), 1392, never(t))
	if got.StartMS != 1_271_000 || got.Source != SourceEnding {
		t.Errorf("got %+v, want the closing song at 1271s", got)
	}
}

// Black Books: no black run anywhere in the window. The closing theme is the
// only evidence, and it is enough.
func TestEndingAloneIsAnAnswer(t *testing.T) {
	got := EpisodeCreditsFrom(Credits{}, ending(1453), 1488, never(t))
	if !got.Found || got.StartMS != 1_453_000 || got.Source != SourceEnding {
		t.Errorf("got %+v, want the closing theme", got)
	}
}

// Silicon Valley: what the season shares is the HBO ident, five seconds from
// the end. That is not a closing theme, and it overrules nothing.
func TestEndingIgnoresAnIdent(t *testing.T) {
	got := EpisodeCreditsFrom(black(1640), ending(1705), 1710, never(t))
	if got.StartMS != 1_640_000 || got.Source != SourceUngated {
		t.Errorf("got %+v, want the black run; the shared audio is an ident", got)
	}
	if got := EpisodeCreditsFrom(Credits{}, ending(1705), 1710, never(t)); got.Found {
		t.Errorf("got %+v from an ident alone, want nothing", got)
	}
}

func TestEndingWithNeitherIsNothing(t *testing.T) {
	if got := EpisodeCreditsFrom(Credits{}, Intro{}, 1300, never(t)); got.Found {
		t.Errorf("got %+v, want nothing", got)
	}
}

// Without a gate the tie goes to the music: a fade cannot be told from text,
// and the music cannot be a fade.
func TestEndingWithoutAGateTrustsTheMusic(t *testing.T) {
	got := EpisodeCreditsFrom(black(2611), ending(2711), 2760, nil)
	if got.Source != SourceEnding {
		t.Errorf("got %+v, want the closing theme", got)
	}
}
