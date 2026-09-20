package mpv

import "math"

/*
 * Translating mpv into the media-element events the provider already consumes.
 *
 * web/src/playback/backend.ts defines the contract: the provider listens for
 * loadedmetadata, playing, timeupdate, pause, ended and the rest, by their
 * HTMLMediaElement names. mpv speaks in property changes instead. This file is
 * the whole mapping, pure, so every rule is a table test rather than something
 * discovered by watching a film.
 */

// Observed is every property the backend watches, with the format it is read
// in. The order is the observation id, which is how a change is told apart
// without comparing names on every tick.
var Observed = []struct {
	Name   string
	Double bool // otherwise a flag
}{
	{"time-pos", true},
	{"duration", true},
	{"pause", false},
	{"paused-for-cache", false},
	{"eof-reached", false},
	{"core-idle", false},
}

// State is the backend's view of the player, as the page will read it.
type State struct {
	CurrentTime float64
	Duration    float64 // NaN until known, like the element
	Paused      bool
	Waiting     bool
	Ended       bool
	Loaded      bool // loadedmetadata has been raised for this source
}

// NewState is the state of a backend with nothing loaded.
func NewState() State { return State{Duration: math.NaN(), Paused: true} }

// Change is one property update from mpv.
type Change struct {
	Name   string
	Double float64
	Flag   bool
	// Unavailable is mpv's MPV_FORMAT_NONE: the property has no value now,
	// as duration does before a file is open.
	Unavailable bool
}

// Apply folds one change into the state and returns the media events it
// raises, in the order an element would raise them.
func Apply(s State, c Change) (State, []string) {
	var ev []string
	switch c.Name {
	case "duration":
		if c.Unavailable {
			s.Duration = math.NaN()
			return s, nil
		}
		s.Duration = c.Double
		if !s.Loaded {
			// Duration arriving is one way to learn a file is open, and it is
			// not dependable on its own — see Opened, which is the other.
			s.Loaded = true
			ev = append(ev, "loadedmetadata", "loadeddata")
		}
	case "time-pos":
		if c.Unavailable {
			return s, nil
		}
		s.CurrentTime = c.Double
		ev = append(ev, "timeupdate")
	case "pause":
		if c.Flag == s.Paused {
			return s, nil
		}
		s.Paused = c.Flag
		if c.Flag {
			ev = append(ev, "pause")
		} else {
			s.Ended = false
			ev = append(ev, "play")
			if !s.Waiting {
				ev = append(ev, "playing")
			}
		}
	case "paused-for-cache":
		if c.Flag == s.Waiting {
			return s, nil
		}
		s.Waiting = c.Flag
		if c.Flag {
			ev = append(ev, "waiting")
		} else if !s.Paused {
			ev = append(ev, "playing")
		}
	case "eof-reached":
		// keep-open holds the last frame and sets this; the element's `ended`
		// also leaves `paused` true, which the provider relies on when it
		// decides not to roll on.
		if c.Flag && !s.Ended {
			s.Ended = true
			s.Paused = true
			ev = append(ev, "ended")
		} else if !c.Flag {
			s.Ended = false
		}
	}
	return s, ev
}

/*
 * Opened is mpv's own "the file is open" event, and it is what the page waits
 * for before it trusts anything the player says.
 *
 * Duration alone cannot carry that news. mpv reports a property when its
 * **value changes**, so re-opening the same file — which is what changing the
 * audio track does — leaves the duration exactly as it was and reports
 * nothing. The page then never hears that a file opened: it goes on
 * suppressing the position it does not yet trust, the clock sits at 0:00 for
 * the rest of the film, and nothing is written to the server either, because
 * progress is saved from the same events.
 *
 * Seen twice before it was understood, both times after changing the audio
 * track and both times diagnosed as something else.
 *
 * The element raises loadedmetadata once it knows the file, then loadeddata
 * when a frame is ready. The provider only uses loadeddata to drop the
 * spinner, which `playing` also does, so raising both here is faithful enough
 * and keeps one source of truth for "the file opened".
 */
func Opened(s State) (State, []string) {
	if s.Loaded {
		return s, nil
	}
	s.Loaded = true
	return s, []string{"loadedmetadata", "loadeddata"}
}

// Reset is the state after a new source is loaded: position and duration
// forgotten, pause kept, because the element keeps `paused` across a `load()`
// until `play()` is called.
func Reset(s State) State {
	n := NewState()
	n.Paused = s.Paused
	return n
}
