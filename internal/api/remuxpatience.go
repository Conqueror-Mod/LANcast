package api

import (
	"time"

	"lancast/internal/probe"
	"lancast/internal/transcode"
)

/*
 * How long the playlist route waits for a copied session to finish its own
 * playlist before serving it growing.
 *
 * The rule that matters is **progress, not elapsed time**: a session still
 * writing segments is going to finish, and one that has stopped writing them
 * is not. The cap exists only so a pathological source cannot hold a response
 * open for ever.
 *
 * A flat cap was wrong for one shape, and the shape is common. `video=copy
 * audio=encode` copies the picture — which is why it needs this wait at all,
 * since copied segments cannot be listed in advance — while re-encoding the
 * sound, and the sound is then the whole cost. Jay and Silent Bob Reboot
 * (H.264 + DTS, 1h45) wrote segments steadily and was still writing them at
 * sixty seconds, so it was served growing: the engine took the growing
 * playlist for a live stream, joined at its edge, and froze within half a
 * minute. Reported as *took over a minute to play, then froze and never
 * played again*.
 *
 * So the cap is scaled by the work rather than fixed. A pure remux is hundreds
 * of times realtime and never comes near even the floor; an audio encode runs
 * at tens of times realtime, so a film's own length is the thing that predicts
 * how long it needs.
 */

const (
	// remuxStall gives up when no new segment has appeared for this long. Four
	// seconds of silence from a copy means it is not coming; the same silence
	// from an encode would mean nothing at all, which is why this is only ever
	// applied to a session that copies its video.
	remuxStall = 4 * time.Second
	// remuxCapFloor is the wait for a pure remux, which is what sixty seconds
	// was chosen for: roughly a 15GB copy at the rate measured on this machine.
	remuxCapFloor = 60 * time.Second
	// remuxCapCeiling bounds the whole thing. Past five minutes the honest
	// answer is that this is not going to be a quick start, and holding the
	// response open longer helps nobody.
	remuxCapCeiling = 5 * time.Minute
	/*
	 * audioEncodeRealtime is the conservative multiple of realtime an audio
	 * encode is assumed to run at.
	 *
	 * Measured on this machine, DTS to AAC ran at a little over a hundred times
	 * realtime — the 1h45 film above took a bit over sixty seconds. Thirty is
	 * deliberately pessimistic: the cost of guessing low is a wait that ends
	 * before the work does, which is the fault being fixed, and the cost of
	 * guessing high is only that a genuinely stuck session is held by the stall
	 * rule instead, four seconds after it stops.
	 */
	audioEncodeRealtime = 30
)

/*
 * remuxPatienceFor is the wait for one session, from what it has been asked to
 * do and how long the film is.
 *
 * Pure, and tested that way: the alternative is discovering the numbers are
 * wrong from a film that freezes.
 */
func remuxPatienceFor(d probe.Decision, mediaSeconds float64) transcode.Patience {
	p := transcode.Patience{Stall: remuxStall, Cap: remuxCapFloor}
	if d.AudioAction != "encode" || mediaSeconds <= 0 {
		// A straight copy of both streams: the floor already covers far more
		// than it takes.
		return p
	}
	need := time.Duration(mediaSeconds/audioEncodeRealtime) * time.Second
	if need > p.Cap {
		p.Cap = need
	}
	if p.Cap > remuxCapCeiling {
		p.Cap = remuxCapCeiling
	}
	return p
}
