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
	if !reflect.DeepEqual(ev, []string{"loadedmetadata", "loadeddata"}) {
		t.Fatalf("events = %v", ev)
	}
	if s.Duration != 5400.2 {
		t.Errorf("duration = %v", s.Duration)
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
