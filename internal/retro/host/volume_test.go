package host

import (
	"testing"
	"time"
)

// recAudio keeps every sample the session hands the sink.
type recAudio struct{ got chan []int16 }

func (a *recAudio) Open(int) error { return nil }
func (a *recAudio) Close()         {}
func (a *recAudio) Write(s []int16) {
	select {
	case a.got <- append([]int16(nil), s...):
	default:
	}
}

/*
 * The game's volume is the game's alone (ADR 0076): applied to the samples,
 * never by a device or session volume a film in the corner shares, and never
 * by writing into the core's own buffer.
 */
func TestGameVolumeScalesTheSamplesTheSinkGets(t *testing.T) {
	audio := &recAudio{got: make(chan []int16, 64)}
	h := start(t, func(c *Config) { c.Audio = audio })
	defer h.stop(t)

	// fakeCore sends {1, 2, 3, 4}; at full volume they arrive as they are.
	if s := <-audio.got; s[3] != 4 {
		t.Fatalf("full volume changed the samples: %v", s)
	}
	h.s.SetVolume(0.5)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case s := <-audio.got:
			if s[3] == 2 && s[1] == 1 {
				goto half
			}
		case <-deadline:
			t.Fatal("the samples never came through at half volume")
		}
	}
half:
	h.s.SetVolume(0)
	deadline = time.After(2 * time.Second)
	for {
		select {
		case s := <-audio.got:
			if s[0] == 0 && s[3] == 0 {
				return
			}
		case <-deadline:
			t.Fatal("the samples never came through silent")
		}
	}
}

func TestScalingNeverWritesIntoTheCoresBuffer(t *testing.T) {
	s := New(Config{})
	s.SetVolume(0.5)
	core := []int16{100, -100, 32767, -32768}
	out := s.scale(core)
	if core[0] != 100 || core[3] != -32768 {
		t.Fatalf("the core's buffer was changed: %v", core)
	}
	if out[0] != 50 || out[1] != -50 || out[2] != 16383 || out[3] != -16384 {
		t.Fatalf("half volume = %v", out)
	}
}

/*
 * A paused game must not be logged as one enormous frame. The menu pauses
 * from inside a frame, the paused drain waits for the resume, and the
 * counter used to see only the running state either side of the wait.
 */
func TestAPauseIsNotASlowFrame(t *testing.T) {
	h := start(t, nil)
	defer h.stop(t)
	h.s.Pause()
	h.waitFor(t, "paused")
	time.Sleep(300 * time.Millisecond)
	h.s.Resume()
	h.waitFor(t, "resumed")
	time.Sleep(50 * time.Millisecond)
	h.s.Pause()
	h.waitFor(t, "paused")
	// Read on the core thread's side of a pause: stats are core-thread only,
	// and a paused session is parked in its drain, not touching them.
	if gap := h.s.stats.slowest; gap >= 250*time.Millisecond {
		t.Fatalf("the pause was counted as a %v frame", gap)
	}
}

func TestVolumeIsClamped(t *testing.T) {
	s := New(Config{})
	for _, v := range []float64{-1, 2} {
		s.SetVolume(v)
		out := s.scale([]int16{1000})
		if out[0] != 0 && out[0] != 1000 {
			t.Errorf("SetVolume(%v) gave %d", v, out[0])
		}
	}
	s.SetVolume(2)
	if out := s.scale([]int16{1000}); out[0] != 1000 {
		t.Errorf("above full volume = %d", out[0])
	}
}
