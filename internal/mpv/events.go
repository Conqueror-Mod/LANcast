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
			// The element raises loadedmetadata once it knows the duration
			// and dimensions, then loadeddata when a frame is ready. mpv
			// knowing the duration means the demuxer has the file open; the
			// provider only uses loadeddata to drop the spinner, which
			// `playing` also does, so raising both here is faithful enough
			// and keeps one source of truth for "the file opened".
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

// Reset is the state after a new source is loaded: position and duration
// forgotten, pause kept, because the element keeps `paused` across a `load()`
// until `play()` is called.
func Reset(s State) State {
	n := NewState()
	n.Paused = s.Paused
	return n
}
