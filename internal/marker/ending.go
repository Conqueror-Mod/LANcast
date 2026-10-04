package marker

import "math"

/*
 * Where an episode's credits begin, from two kinds of evidence (ADR 0054,
 * 2026-10-04 amendment).
 *
 * Pure, like the rest of the decisions in this package: it takes what the
 * worker measured and returns a position, so every case below is a test with
 * no audio and no ffmpeg.
 *
 * # The two signals, and how each one fails
 *
 * **The black run** is exact to the frame when it is right, and a third-act
 * fade looks exactly like it: Voyager S5E14 warps out of a scene to black at
 * 94.6%, two and a half minutes before its credits.
 *
 * **The ending audio** — the stretch an episode shares with its siblings at
 * the end, which is the closing theme — cannot be fooled by a fade, because a
 * scene is not shared across a season. It has two failure modes of its own. It
 * lands on a channel ident when the closing music is not shared (HBO's on
 * Silicon Valley, the network card on Sunny), always within seconds of the end;
 * and where the closing theme starts over the final shot it lands a few
 * seconds early.
 *
 * So each one checks the other. Agreement takes the frame-exact black run. A
 * black run well before the music is a fade or text on black, and the frame
 * gate — wrong for television on its own, right as a tie-breaker — says which.
 * Music well before the black run is a closing song whose end the black run
 * found (Death Parade), and the music is right.
 *
 * # What it was tested on
 *
 * Tuned by eye on episodes from every series in the library, then frozen and
 * run on 40 episodes nobody had looked at: **0 early** against 3 for the black
 * run alone, 40 answered against 36, and the 2 late answers were late under
 * both (TNG's first season, whose credits begin over the final shot).
 */

const (
	/*
	 * EndingIdentSeconds: shared audio beginning this close to the end is an
	 * ident, not a closing theme. Measured across 934 episodes: idents
	 * clustered at 0–19 s from the end (Silicon Valley, Lanterns, Blue Mountain
	 * State, most of Sunny), closing themes at 30 s and beyond (Futurama and
	 * The League ~35, Star Trek ~50, Cowboy Bebop ~127).
	 */
	EndingIdentSeconds = 20.0
	// EndingAgreeSeconds is how far apart the two may be and still agree.
	EndingAgreeSeconds = 20.0
)

// Credit sources an episode's marker can carry.
const (
	// SourceEnding is a marker placed by the closing theme a season shares.
	SourceEnding = "ending-audio"
)

// EpisodeCredits is the decision and which evidence it rests on.
type EpisodeCredits struct {
	Credits
	Source string
}

/*
 * EpisodeCreditsFrom decides an episode's credits.
 *
 * black is the ungated black-run answer (CreditsFrom with no gate); ending is
 * the shared-audio answer, with StartSec in this episode's own timeline;
 * gate is asked only to break the one tie that needs it, so its frames are
 * read for a minority of episodes. A nil gate is treated as a refusal, which
 * sends that tie to the music.
 */
func EpisodeCreditsFrom(black Credits, ending Intro, durationSec float64, gate func(startSec float64) bool) EpisodeCredits {
	if durationSec <= 0 {
		return EpisodeCredits{}
	}
	var a *float64
	if ending.Found && durationSec-ending.StartSec >= EndingIdentSeconds && ending.StartSec > 0 {
		v := ending.StartSec
		a = &v
	}
	bSec := float64(black.StartMS) / 1000

	fromBlack := EpisodeCredits{Credits: black, Source: SourceUngated}
	fromEnding := func() EpisodeCredits {
		return EpisodeCredits{
			Credits: Credits{Found: true, StartMS: int64(*a * 1000), Confidence: ending.Confidence},
			Source:  SourceEnding,
		}
	}

	switch {
	case a == nil && !black.Found:
		return EpisodeCredits{}
	case a == nil:
		return fromBlack
	case !black.Found:
		return fromEnding()
	case math.Abs(*a-bSec) <= EndingAgreeSeconds:
		return fromBlack
	case bSec < *a:
		if gate != nil && gate(bSec) {
			return fromBlack
		}
		return fromEnding()
	default:
		return fromEnding()
	}
}
