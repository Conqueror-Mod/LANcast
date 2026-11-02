package host

import (
	"bytes"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeAchievements records what the session asked of it (ADR 0112).
type fakeAchievements struct {
	mu            sync.Mutex
	started       int
	startedLoaded bool  // the core had loaded the game when Start came
	startedAfter  int64 // frames the core had run when Start came
	path          string
	frames        atomic.Int64
	idles         atomic.Int64
	resets        int
	lists         int
	stopped       bool
	stopUnloaded  bool // the core had already unloaded when Stop came
	progress      []byte
	restored      [][]byte
	core          *fakeCore
}

func (a *fakeAchievements) Start(_ GameMemory, path string, _ []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.started++
	a.path = path
	a.startedLoaded = a.core.loaded
	a.startedAfter = int64(a.core.counter.Load())
	return nil
}

func (a *fakeAchievements) hasStarted() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.started > 0
}
func (a *fakeAchievements) Frame() { a.frames.Add(1) }
func (a *fakeAchievements) Idle()  { a.idles.Add(1) }
func (a *fakeAchievements) Reset() { a.mu.Lock(); a.resets++; a.mu.Unlock() }
func (a *fakeAchievements) List()  { a.mu.Lock(); a.lists++; a.mu.Unlock() }
func (a *fakeAchievements) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopped = true
	a.stopUnloaded = a.core.unloaded
}
func (a *fakeAchievements) Progress() []byte {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.progress
}
func (a *fakeAchievements) SetProgress(p []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.restored = append(a.restored, bytes.Clone(p))
}

func startWithAchievements(t *testing.T, mutate func(*Config)) (*harness, *fakeAchievements) {
	t.Helper()
	core := &fakeCore{name: "fakecore"}
	a := &fakeAchievements{core: core}
	h := start(t, func(c *Config) {
		c.Core = core
		c.GamePath = `C:\games\Sample.nes`
		c.Achievements = a
		c.Input = &padInput{}
		if mutate != nil {
			mutate(c)
		}
	})
	h.core = core
	return h, a
}

// Started once the game has run long enough to have sent its memory map,
// told of every played frame and of a reset, kept going while paused, and
// stopped before the game unloads.
func TestAchievementsFollowTheGame(t *testing.T) {
	h, a := startWithAchievements(t, nil)
	deadline := time.Now().Add(5 * time.Second)
	for !a.hasStarted() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	h.s.Reset()
	h.s.Pause()
	h.waitFor(t, "paused")
	for a.idles.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	h.s.ListAchievements()
	h.s.Resume()
	h.waitFor(t, "resumed")
	h.stop(t)

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.started != 1 || !a.startedLoaded || a.path != `C:\games\Sample.nes` {
		t.Errorf("start: %d times, after load %v, path %q", a.started, a.startedLoaded, a.path)
	}
	if a.startedAfter < memoryLogAfter {
		t.Errorf("started after %d frames; a core may not have sent its map before %d", a.startedAfter, memoryLogAfter)
	}
	if a.frames.Load() < 5 || a.idles.Load() == 0 {
		t.Errorf("frames %d, idles %d", a.frames.Load(), a.idles.Load())
	}
	if a.resets != 1 || a.lists != 1 {
		t.Errorf("resets %d, lists %d", a.resets, a.lists)
	}
	if !a.stopped || a.stopUnloaded {
		t.Errorf("stopped %v, after unload %v", a.stopped, a.stopUnloaded)
	}
}

// Progress rides with a state, and is cut off before the core sees it: the
// fake core refuses any state that is not exactly its own four bytes.
func TestProgressRidesWithStates(t *testing.T) {
	h, a := startWithAchievements(t, func(c *Config) { c.SaveStateOnStop = true })
	a.mu.Lock()
	a.progress = []byte("P1")
	a.mu.Unlock()
	h.s.Pause()
	h.waitFor(t, "paused")
	at := h.core.counter.Load()
	h.s.SaveState("state-1")
	h.waitFor(t, "state-saved")
	h.saves.mu.Lock()
	stored := h.saves.states["state-1"]
	h.saves.mu.Unlock()
	if len(stored) != 4+2+progressFooter {
		t.Fatalf("stored %d bytes", len(stored))
	}
	h.s.Resume()
	h.waitFor(t, "resumed")
	time.Sleep(20 * time.Millisecond)
	h.s.Pause()
	h.waitFor(t, "paused")
	h.s.LoadState("state-1")
	h.waitFor(t, "state-loaded")
	if h.core.counter.Load() != at {
		t.Errorf("counter %d after loading, want %d", h.core.counter.Load(), at)
	}
	h.stop(t)
	a.mu.Lock()
	if len(a.restored) != 1 || string(a.restored[0]) != "P1" {
		t.Errorf("restored %q", a.restored)
	}
	a.mu.Unlock()

	// Continue brings it back too.
	h2, a2 := startWithAchievements(t, func(c *Config) { c.Saves = h.saves; c.ResumeState = "auto" })
	h2.waitFor(t, "state-loaded")
	h2.stop(t)
	a2.mu.Lock()
	defer a2.mu.Unlock()
	if len(a2.restored) == 0 || string(a2.restored[0]) != "P1" {
		t.Errorf("resume restored %q", a2.restored)
	}
}

// Stepping back is not playing: rewound frames are not evaluated.
func TestRewoundFramesAreNotEvaluated(t *testing.T) {
	var runs atomic.Int64
	h, a := startWithAchievements(t, func(c *Config) {
		c.Core.(*fakeCore).onRun = func(*fakeCore) { runs.Add(1) }
	})
	in := h.s.cfg.Input.(*padInput)
	h.s.SetRewind(true)
	time.Sleep(80 * time.Millisecond)
	in.mu.Lock()
	in.pad.Hot = HotRewind
	in.mu.Unlock()
	time.Sleep(20 * time.Millisecond) // the hold reaches the session
	f0, r0 := a.frames.Load(), runs.Load()
	time.Sleep(60 * time.Millisecond)
	f1, r1 := a.frames.Load(), runs.Load()
	in.mu.Lock()
	in.pad.Hot = 0
	in.mu.Unlock()
	h.stop(t)
	if r1 == r0 {
		t.Fatal("no rewound frames ran")
	}
	if f1 != f0 {
		t.Errorf("%d rewound frames were evaluated", f1-f0)
	}
}

func TestSplitProgress(t *testing.T) {
	state := []byte{1, 2, 3, 4}
	s, p := splitProgress(withProgress(state, []byte("progress")))
	if !bytes.Equal(s, state) || string(p) != "progress" {
		t.Errorf("round trip: %v %q", s, p)
	}
	if got := withProgress(state, nil); !bytes.Equal(got, state) {
		t.Errorf("no progress changed the state: %v", got)
	}
	for _, raw := range [][]byte{state, {}, append([]byte{9, 9, 9, 9, 0xff, 0xff, 0, 0}, progressTag[:]...)} {
		if s, p := splitProgress(raw); !bytes.Equal(s, raw) || p != nil {
			t.Errorf("%v split into %v + %v", raw, s, p)
		}
	}
}
