package mpv

import (
	"math"
	"reflect"
	"testing"
)

func TestOptionsKeepTheNoPhoneHomeRule(t *testing.T) {
	// ADR 0067. Each of these stops mpv running code or reaching the network
	// on its own; losing one is a principle change, not a tweak.
	want := map[string]string{
		"config":                 "no",
		"load-scripts":           "no",
		"ytdl":                   "no",
		"input-default-bindings": "no",
	}
	got := map[string]string{}
	for _, o := range Options(0x1234, "") {
		got[o.Name] = o.Value
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if got["sid"] != "no" {
		t.Error("mpv would draw the file's default subtitles over the page's own")
	}
	if got["wid"] != "4660" {
		t.Errorf("wid = %q, want decimal 4660", got["wid"])
	}
	if _, ok := got["log-file"]; ok {
		t.Error("log-file set without being asked for")
	}
}

func run(s State, cs ...Change) (State, []string) {
	var all []string
	for _, c := range cs {
		var ev []string
		s, ev = Apply(s, c)
		all = append(all, ev...)
	}
	return s, all
}

func TestOpeningAFileRaisesMetadataOnce(t *testing.T) {
	s, ev := run(NewState(),
		Change{Name: "duration", Double: 5400},
		Change{Name: "duration", Double: 5400.2}, // refined as the demuxer reads on
	)
	// Metadata once; the refinement is a durationchange, as the element
	// raises for a length that moves.
	if !reflect.DeepEqual(ev, []string{"loadedmetadata", "loadeddata", "durationchange"}) {
		t.Fatalf("events = %v", ev)
	}
	if s.Duration != 5400.2 {
		t.Errorf("duration = %v", s.Duration)
	}
}

/*
 * mpv's file-loaded event can beat the duration property. The page is then
 * told the file is open with no length, and before durationchange it never
 * heard the real one -- half of how Randomize all showed a 1:30:00 film as a
 * few minutes long.
 */
func TestADurationAfterTheFileOpenedIsADurationChange(t *testing.T) {
	s, opened := Opened(Reset(NewState()))
	if !reflect.DeepEqual(opened, []string{"loadedmetadata", "loadeddata"}) {
		t.Fatalf("opened = %v", opened)
	}
	if !math.IsNaN(s.Duration) {
		t.Fatalf("duration at open = %v, want NaN", s.Duration)
	}
	s, ev := Apply(s, Change{Name: "duration", Double: 5400})
	if !reflect.DeepEqual(ev, []string{"durationchange"}) {
		t.Errorf("events = %v, want durationchange", ev)
	}
	if s.Duration != 5400 {
		t.Errorf("duration = %v", s.Duration)
	}
	// The same value again is not a change.
	if _, ev := Apply(s, Change{Name: "duration", Double: 5400}); ev != nil {
		t.Errorf("an unchanged duration raised %v", ev)
	}
}

func TestDurationUnknownIsNaNLikeTheElement(t *testing.T) {
	s, ev := run(NewState(), Change{Name: "duration", Unavailable: true})
	if !math.IsNaN(s.Duration) || ev != nil {
		t.Fatalf("duration = %v events = %v", s.Duration, ev)
	}
}

func TestPlayAfterPauseRaisesPlayThenPlaying(t *testing.T) {
	_, ev := run(NewState(), Change{Name: "pause", Flag: false})
	if !reflect.DeepEqual(ev, []string{"play", "playing"}) {
		t.Fatalf("events = %v", ev)
	}
}

func TestRepeatedPauseValueRaisesNothing(t *testing.T) {
	// mpv re-reports a property on observe; the provider saves progress on
	// every `pause`, so a duplicate would write a record for nothing.
	_, ev := run(NewState(), Change{Name: "pause", Flag: true})
	if ev != nil {
		t.Fatalf("events = %v", ev)
	}
}

func TestBufferingWhilePlayingIsWaitingThenPlaying(t *testing.T) {
	s := NewState()
	s.Paused = false
	_, ev := run(s,
		Change{Name: "paused-for-cache", Flag: true},
		Change{Name: "paused-for-cache", Flag: false},
	)
	if !reflect.DeepEqual(ev, []string{"waiting", "playing"}) {
		t.Fatalf("events = %v", ev)
	}
}

func TestCacheRecoveryWhilePausedIsNotPlaying(t *testing.T) {
	// A stall that clears while the viewer has paused must not tell the
	// provider frames are arriving; it would drop a note it still needs.
	s := NewState()
	s.Waiting = true
	_, ev := run(s, Change{Name: "paused-for-cache", Flag: false})
	if ev != nil {
		t.Fatalf("events = %v", ev)
	}
}

func TestEndOfFileRaisesEndedOnceAndLeavesPaused(t *testing.T) {
	s := NewState()
	s.Paused = false
	s, ev := run(s,
		Change{Name: "eof-reached", Flag: true},
		Change{Name: "eof-reached", Flag: true},
	)
	if !reflect.DeepEqual(ev, []string{"ended"}) {
		t.Fatalf("events = %v", ev)
	}
	if !s.Paused {
		t.Error("ended must leave paused true, as the element does")
	}
}

func TestTimeUpdates(t *testing.T) {
	s, ev := run(NewState(), Change{Name: "time-pos", Double: 12.5}, Change{Name: "time-pos", Unavailable: true})
	if s.CurrentTime != 12.5 || !reflect.DeepEqual(ev, []string{"timeupdate"}) {
		t.Fatalf("t = %v events = %v", s.CurrentTime, ev)
	}
}

func TestResetKeepsPauseAndForgetsTheFile(t *testing.T) {
	s := State{CurrentTime: 99, Duration: 100, Paused: false, Loaded: true, Ended: true}
	n := Reset(s)
	if n.Paused || n.Loaded || n.Ended || n.CurrentTime != 0 || !math.IsNaN(n.Duration) {
		t.Fatalf("reset = %+v", n)
	}
}

func TestOptionsThatMayBeAbsentAreMarked(t *testing.T) {
	/*
	 * LANcast's own libmpv (ADR 0069) has no scripting, and the options
	 * implemented by scripts go with it — `ytdl` is not an option when there is
	 * no Lua to run ytdl_hook. Starting must survive that, and must not survive
	 * a typo in one of the options that really do exist.
	 */
	byName := map[string]Option{}
	for _, o := range Options(1, "") {
		byName[o.Name] = o
	}
	for _, name := range []string{"ytdl", "osc"} {
		if !byName[name].IfPresent {
			t.Errorf("%s must tolerate a build without scripting", name)
		}
	}
	for _, name := range []string{"config", "load-scripts", "input-default-bindings", "wid"} {
		if byName[name].IfPresent {
			t.Errorf("%s is core mpv: a missing one is a typo, not a smaller build", name)
		}
	}
}

func TestReopeningTheSameFileStillSaysItOpened(t *testing.T) {
	/*
	 * Changing the audio track re-opens the same file, so its duration does
	 * not change — and mpv reports a property only when its value changes.
	 * Waiting for duration therefore misses the second open entirely, and the
	 * page goes on distrusting a clock that never becomes trustworthy: 0:00
	 * for the rest of the film, with nothing saved to the server.
	 */
	s, ev := run(NewState(), Change{Name: "duration", Double: 7741})
	if len(ev) == 0 {
		t.Fatal("the first open said nothing")
	}

	// A new file: the player resets, and mpv says the file is open before any
	// property it happens to share with the last one.
	s = Reset(s)
	s, ev = Opened(s)
	if !reflect.DeepEqual(ev, []string{"loadedmetadata", "loadeddata"}) {
		t.Fatalf("reopen events = %v", ev)
	}

	// And it is said once, however the news arrives.
	_, again := Opened(s)
	if again != nil {
		t.Errorf("a second announcement for the same file: %v", again)
	}
	// The length arriving afterwards is news about the length, not a second
	// open: durationchange, never loadedmetadata again.
	if _, dup := Apply(s, Change{Name: "duration", Double: 7741}); !reflect.DeepEqual(dup, []string{"durationchange"}) {
		t.Errorf("duration after the open raised %v, want only durationchange", dup)
	}
}

func TestAChannelCountChangeIsRaisedOnce(t *testing.T) {
	s, ev := run(NewState(),
		Change{Name: "audio-params/channel-count", Double: 6},
		Change{Name: "audio-params/channel-count", Double: 6}, // re-reported on observe
	)
	if !reflect.DeepEqual(ev, []string{AudioChannelsEvent}) || s.Channels != 6 {
		t.Fatalf("channels = %d, events = %v", s.Channels, ev)
	}
}

func TestSwitchingToAStereoTrackMidFilmRebuildsForTwo(t *testing.T) {
	// A commentary track after the 5.1 main one: the filter was built for six
	// channels and must hear that it now has two.
	s, ev := run(NewState(),
		Change{Name: "audio-params/channel-count", Double: 6},
		Change{Name: "audio-params/channel-count", Unavailable: true}, // aid switch reinitialises audio
		Change{Name: "audio-params/channel-count", Double: 2},
	)
	if s.Channels != 2 || len(ev) != 3 {
		t.Fatalf("channels = %d, events = %v; want 2 and three rebuilds", s.Channels, ev)
	}
}

func TestResetKeepsTheChannelCount(t *testing.T) {
	// mpv does not re-report an unchanged count, so a 5.1 film after a 5.1
	// film says nothing. The state has to remember it or dialogue boost turns
	// itself off for the second film.
	s, _ := run(NewState(), Change{Name: "audio-params/channel-count", Double: 6})
	if got := Reset(s).Channels; got != 6 {
		t.Fatalf("Reset forgot the channel count: %d", got)
	}
}
