package host

import (
	"testing"
	"time"
)

/*
 * Speed and rewind (2026-10-18), against the fake core, whose frame counter
 * is its whole state: it counts up one per frame, and a state restores it.
 */

func (h *harness) hold(hot uint8) {
	h.input.mu.Lock()
	h.input.pad.Hot = hot
	h.input.mu.Unlock()
}

func (h *harness) counts() (frames uint32, shown int) {
	h.video.mu.Lock()
	defer h.video.mu.Unlock()
	return h.core.counter.Load(), h.video.frames
}

func waitSpeed(t *testing.T, h *harness, mode string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case e := <-h.events:
			if e.Kind == "speed" && e.Text == mode {
				return
			}
		case <-deadline:
			t.Fatalf("no speed %q event", mode)
		}
	}
}

// Held, fast-forward runs three frames for every one it shows.
func TestFastForwardRunsThreeFramesPerPicture(t *testing.T) {
	h := start(t, nil)
	defer h.stop(t)
	h.hold(HotFastForward)
	waitSpeed(t, h, "fast")
	f0, s0 := h.counts()
	time.Sleep(300 * time.Millisecond)
	f1, s1 := h.counts()
	ran, shown := int(f1-f0), s1-s0
	if shown == 0 || ran < shown*5/2 {
		t.Errorf("ran %d frames and showed %d; want about three to one", ran, shown)
	}
	h.hold(0)
	waitSpeed(t, h, "normal")
}

// The menu's toggle does the same without a key held.
func TestTheMenuTurnsFastForwardOn(t *testing.T) {
	h := start(t, nil)
	defer h.stop(t)
	h.s.SetFastForward(true)
	waitSpeed(t, h, "fast")
	h.s.SetFastForward(false)
	waitSpeed(t, h, "normal")
}

// With rewind on, holding the key takes the game backwards, and letting go
// carries on from there.
func TestRewindGoesBackAndCarriesOn(t *testing.T) {
	h := start(t, nil)
	defer h.stop(t)
	h.s.SetRewind(true)
	time.Sleep(400 * time.Millisecond)
	before, _ := h.counts()

	h.hold(HotRewind)
	waitSpeed(t, h, "rewind")
	time.Sleep(150 * time.Millisecond)
	back, _ := h.counts()
	if back >= before {
		t.Fatalf("rewinding did not go back: frame %d before, %d after", before, back)
	}

	h.hold(0)
	waitSpeed(t, h, "normal")
	time.Sleep(100 * time.Millisecond)
	if again, _ := h.counts(); again <= back {
		t.Errorf("after letting go the game did not carry on (%d, then %d)", back, again)
	}
}

// Off by default: holding the key does nothing to the game.
func TestRewindIsOffUnlessTurnedOn(t *testing.T) {
	h := start(t, nil)
	defer h.stop(t)
	time.Sleep(100 * time.Millisecond)
	before, _ := h.counts()
	h.hold(HotRewind)
	time.Sleep(150 * time.Millisecond)
	if after, _ := h.counts(); after <= before {
		t.Errorf("with rewind off the game stopped or went back (%d, then %d)", before, after)
	}
	h.hold(0)
}

func TestTheRewindBufferStaysWithinBothBounds(t *testing.T) {
	r := newRewindRing(3, 10)
	for i := byte(1); i <= 5; i++ {
		r.push([]byte{i, i})
	}
	if r.len() != 3 || r.bytes != 6 {
		t.Fatalf("count bound: %d states, %d bytes", r.len(), r.bytes)
	}
	if s, _ := r.pop(); s[0] != 5 {
		t.Errorf("newest first: got %d", s[0])
	}
	r = newRewindRing(100, 10)
	for i := byte(1); i <= 4; i++ {
		r.push([]byte{i, i, i, i})
	}
	if r.bytes > 10 || r.len() != 2 {
		t.Errorf("byte bound: %d states, %d bytes", r.len(), r.bytes)
	}
	r.push(make([]byte, 11)) // larger than the whole budget: not kept
	if r.len() != 2 {
		t.Errorf("an oversized state was kept")
	}
	r.clear()
	if _, ok := r.pop(); ok {
		t.Error("cleared, but a state remained")
	}
}
