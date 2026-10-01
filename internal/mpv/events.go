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
	// What the audio filter is built for (audiofx.go). Read from the decoder
	// rather than the probe, because the track can change mid-film and a
	// stereo commentary after a 5.1 main track is ordinary.
	{"audio-params/channel-count", true},
}

// AudioChannelsEvent is raised when the decoded channel count changes. It is
// for the native player's own use — the audio filter is rebuilt on it — and
// is not a media-element event, so it never reaches the page.
const AudioChannelsEvent = "audiochannels"

// State is the backend's view of the player, as the page will read it.
type State struct {
	CurrentTime float64
	Duration    float64 // NaN until known, like the element
	Paused      bool
	Waiting     bool
	Ended       bool
	Loaded      bool // loadedmetadata has been raised for this source
	// Channels is the decoded audio's channel count, 0 while there is none.
	Channels int
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
		changed := s.Duration != c.Double // NaN compares unequal: first value counts
		s.Duration = c.Double
		if !s.Loaded {
			// Duration arriving is one way to learn a file is open, and it is
			// not dependable on its own — see Opened, which is the other.
			s.Loaded = true
			ev = append(ev, "loadedmetadata", "loadeddata")
		} else if changed {
			/*
			 * A length learned after the file was reported open.
			 *
			 * Opened often wins the race: mpv's file-loaded event can arrive
			 * before the duration property does, so loadedmetadata goes out
			 * with no length at all and the page reads NaN. With nothing after
			 * it, the page never heard the real length -- which is half of how
			 * Randomize all came to show a film's total as a few minutes. The
			 * element raises durationchange for exactly this.
			 */
			ev = append(ev, "durationchange")
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
	case "audio-params/channel-count":
		// Unavailable counts too: it is the gap between one file's audio and
		// the next, and a graph built for six channels must not be left on
		// whatever opens after it until that file reports its own count.
		n := 0
		if !c.Unavailable && c.Double > 0 {
			n = int(c.Double)
		}
		if n != s.Channels {
			s.Channels = n
			ev = append(ev, AudioChannelsEvent)
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
//
// The channel count is kept, for the reason Opened exists: mpv reports a
// property only when its value changes, so a 5.1 film after a 5.1 film reports
// no count at all. Forgetting it here would leave the state saying 0 for the
// whole second film, and dialogue boost would switch itself off.
func Reset(s State) State {
	n := NewState()
	n.Paused = s.Paused
	n.Channels = s.Channels
	return n
}
