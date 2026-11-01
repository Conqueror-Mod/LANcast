package main

/*
 * matchGate holds a film's start while the display is matched to it
 * (docs/theater-output-plan.md, step 1).
 *
 * A TV takes one to three seconds to resync HDMI after a mode change, and
 * shows black while it does. Started straight away, a film loses its opening
 * seconds to that black screen. So while a match is due, the page's play is
 * remembered rather than obeyed, and honoured once the display has settled.
 *
 * The page's intent is kept the same way mpvBackend.ts keeps wantPlay: a pause
 * asked for during the wait cancels the play, and a new film or a stop makes
 * any match still running for the old one stale, by generation.
 *
 * Pure, so every order those can arrive in is a table test, and a stuck film
 * is not something found by watching one.
 */
type matchGate struct {
	gen     int
	pending bool
	want    bool
}

// open starts a new film. With matching on, its start is held until settled.
// It returns the generation the match for this film must present.
func (g *matchGate) open(matching bool) int {
	g.gen++
	g.pending, g.want = matching, false
	return g.gen
}

// play reports whether to unpause now. While a match is due it is
// remembered instead.
func (g *matchGate) play() bool {
	if g.pending {
		g.want = true
		return false
	}
	return true
}

// pause cancels a remembered play.
func (g *matchGate) pause() { g.want = false }

// settled ends the wait for generation gen. ok is false when gen is stale (a
// newer film or a stop came first) or nothing was waiting; play is whether the
// page asked for the film to start meanwhile.
func (g *matchGate) settled(gen int) (play, ok bool) {
	if gen != g.gen || !g.pending {
		return false, false
	}
	g.pending = false
	return g.want, true
}

// stop forgets the film: a match still running for it changes nothing.
func (g *matchGate) stop() {
	g.gen++
	g.pending, g.want = false, false
}
