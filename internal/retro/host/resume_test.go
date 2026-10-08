package host

import (
	"encoding/binary"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

/*
 * Continue against a core that takes no state before its first frame, as
 * Mupen64Plus-Next does. Applying the state only before the first frame made
 * Continue on every N64 game start from the beginning. The real core is
 * checked by TestRealN64ContinueResumes; this is the rule, where CI runs it.
 */

func resumeFrom(counter uint32) func(*Config) {
	return func(c *Config) {
		state := make([]byte, 4)
		binary.LittleEndian.PutUint32(state, counter)
		c.Saves.(*fakeSaves).states["auto"] = state
		c.ResumeState = "auto"
	}
}

func TestContinueWaitsForACoreThatNeedsAFrameFirst(t *testing.T) {
	h := start(t, func(c *Config) {
		c.Core.(*fakeCore).stateNeedsFrame = true
		resumeFrom(5000)(c)
	})
	defer h.stop(t)
	if e := h.waitFor(t, "state-loaded"); e.Slot != "auto" {
		t.Fatalf("loaded %q", e.Slot)
	}
	// The counter carries on from the state, not from the frames run before.
	if got := h.core.counter.Load(); got < 5000 {
		t.Fatalf("counter %d: the state was not applied", got)
	}
}

func TestContinueSaysSoWhenTheCoreNeverTakesTheState(t *testing.T) {
	h := start(t, func(c *Config) {
		c.Core.(*fakeCore).refuseStates = true
		resumeFrom(5000)(c)
	})
	defer h.stop(t)
	e := h.waitFor(t, "error")
	if e.Slot != "auto" || !strings.Contains(e.Text, "started from the beginning") {
		t.Fatalf("got %+v", e)
	}
}

func TestContinueOnAnOrdinaryCoreLoadsBeforeTheFirstFrame(t *testing.T) {
	var counterAtFirstFrame atomic.Uint32
	h := start(t, func(c *Config) {
		c.Core.(*fakeCore).onRun = func(fc *fakeCore) {
			counterAtFirstFrame.CompareAndSwap(0, fc.counter.Load())
		}
		resumeFrom(5000)(c)
	})
	defer h.stop(t)
	h.waitFor(t, "state-loaded")
	// The event can come before the first frame has run; wait for it.
	for deadline := time.Now().Add(time.Second); counterAtFirstFrame.Load() == 0 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	// onRun sees the counter after the frame's own increment.
	if got := counterAtFirstFrame.Load(); got != 5001 {
		t.Fatalf("first frame ran from %d, want the state's 5000", got-1)
	}
}
