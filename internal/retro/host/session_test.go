package host

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lancast/internal/retro/libretro"
)

// fakeCore behaves like testdata/testcore.c in the libretro package: it
// emulates nothing and makes every answer visible.
type fakeCore struct {
	fe       libretro.Frontend
	counter  atomic.Uint32
	sram     [16]byte
	colour   string
	loaded   bool
	unloaded bool
	closed   bool
	onRun    func(*fakeCore)
	name     string
}

func (c *fakeCore) SystemInfo() libretro.SystemInfo {
	return libretro.SystemInfo{LibraryName: c.name, LibraryVersion: "1.0"}
}
func (c *fakeCore) Init(fe libretro.Frontend) error {
	c.fe = fe
	fe.SetPixelFormat(libretro.FormatXRGB8888)
	fe.SetVariables([]libretro.Variable{
		libretro.ParseVariable("colour", "Colour; red|green|blue"),
	})
	return nil
}
func (c *fakeCore) LoadGame(string, []byte) error { c.loaded = true; return nil }
func (c *fakeCore) AVInfo() libretro.AVInfo {
	return libretro.AVInfo{BaseWidth: 2, BaseHeight: 1, AspectRatio: 2, FPS: 240, SampleRate: 32000}
}
func (c *fakeCore) SetControllerPortDevice(uint32, uint32) {}
func (c *fakeCore) Reset()                                 { c.counter.Store(0) }
func (c *fakeCore) Run() {
	c.fe.InputPoll()
	c.colour, _ = c.fe.Variable("colour")
	if c.fe.InputState(0, libretro.DeviceJoypad, 0, libretro.JoypadA) != 0 {
		c.sram[1] = 1
	}
	px := make([]byte, 8)
	binary.LittleEndian.PutUint32(px, 0x00FF0000)
	binary.LittleEndian.PutUint32(px[4:], c.counter.Load())
	c.fe.VideoRefresh(libretro.Frame{Data: px, Width: 2, Height: 1, Pitch: 8})
	c.fe.AudioBatch([]int16{1, 2, 3, 4})
	c.sram[0] = byte(c.counter.Add(1))
	if c.onRun != nil {
		c.onRun(c)
	}
}
func (c *fakeCore) SerializeSize() int { return 4 }
func (c *fakeCore) Serialize(b []byte) bool {
	binary.LittleEndian.PutUint32(b, c.counter.Load())
	return true
}
func (c *fakeCore) Unserialize(b []byte) bool {
	if len(b) != 4 {
		return false
	}
	c.counter.Store(binary.LittleEndian.Uint32(b))
	return true
}
func (c *fakeCore) Memory(id uint32) []byte {
	if id == libretro.MemorySaveRAM {
		return c.sram[:]
	}
	return nil
}
func (c *fakeCore) UnloadGame()     { c.unloaded = true }
func (c *fakeCore) ContextReset()   {}
func (c *fakeCore) ContextDestroy() {}
func (c *fakeCore) Close()          { c.closed = true }

type fakeSaves struct {
	mu        sync.Mutex
	sram      []byte
	sramPuts  int
	states    map[string][]byte
	stateCore map[string][2]string
	failSRAM  error
}

func newFakeSaves() *fakeSaves {
	return &fakeSaves{states: map[string][]byte{}, stateCore: map[string][2]string{}}
}
func (f *fakeSaves) LoadSRAM() ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sram, f.failSRAM
}
func (f *fakeSaves) StoreSRAM(d []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sram = d
	f.sramPuts++
	return nil
}
func (f *fakeSaves) StoreState(slot string, d []byte, core, ver string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[slot] = d
	f.stateCore[slot] = [2]string{core, ver}
	return nil
}
func (f *fakeSaves) LoadState(slot string) ([]byte, string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.states[slot]
	if !ok {
		return nil, "", "", errors.New("no such state")
	}
	cv := f.stateCore[slot]
	return d, cv[0], cv[1], nil
}

type fakeVideo struct {
	mu     sync.Mutex
	frames int
	last   []byte
	aspect float64
}

func (v *fakeVideo) Present(b []byte, w, h int, aspect float64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.frames++
	v.last = append(v.last[:0], b...)
	v.aspect = aspect
}

type fakeInput struct {
	mu     sync.Mutex
	pad    Pad
	escape bool
}

func (in *fakeInput) Poll() ([2]Pad, bool, bool) {
	in.mu.Lock()
	defer in.mu.Unlock()
	return [2]Pad{in.pad}, false, in.escape
}

type harness struct {
	core   *fakeCore
	saves  *fakeSaves
	video  *fakeVideo
	input  *fakeInput
	s      *Session
	events chan Event
	done   chan error
}

func start(t *testing.T, mutate func(*Config)) *harness {
	t.Helper()
	h := &harness{
		core: &fakeCore{name: "fakecore"}, saves: newFakeSaves(),
		video: &fakeVideo{}, input: &fakeInput{},
		events: make(chan Event, 256), done: make(chan error, 1),
	}
	cfg := Config{
		Core: h.core, Video: h.video, Input: h.input, Saves: h.saves,
		FlushEvery: time.Millisecond,
		OnEvent:    func(e Event) { h.events <- e },
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if mutate != nil {
		mutate(&cfg)
	}
	h.s = New(cfg)
	go func() { h.done <- h.s.Run(context.Background()) }()
	h.waitFor(t, "started")
	return h
}

func (h *harness) waitFor(t *testing.T, kind string) Event {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case e := <-h.events:
			if e.Kind == kind {
				return e
			}
			if e.Kind == "error" && kind != "error" {
				t.Logf("event: %+v", e)
			}
		case <-deadline:
			t.Fatalf("no %q event", kind)
		}
	}
}

func (h *harness) stop(t *testing.T) {
	t.Helper()
	h.s.Stop()
	select {
	case err := <-h.done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the session did not stop")
	}
}

// The saved game is in the core before its first frame, changes reach the
// store while playing, and closing sends the last of them.
func TestSaveRAMFollowsTheGame(t *testing.T) {
	saves := newFakeSaves()
	saves.sram = []byte{0, 0, 0x5A}
	var seenAtFirstFrame byte
	h := start(t, func(c *Config) {
		c.Saves = saves
		c.Core.(*fakeCore).onRun = func(fc *fakeCore) {
			if fc.counter.Load() == 1 {
				seenAtFirstFrame = fc.sram[2]
			}
		}
	})
	h.waitFor(t, "sram-saved")
	h.stop(t)
	if seenAtFirstFrame != 0x5A {
		t.Errorf("the saved game was not loaded before the first frame (byte = %#x)", seenAtFirstFrame)
	}
	saves.mu.Lock()
	defer saves.mu.Unlock()
	if saves.sram[0] != byte(h.core.counter.Load()) {
		t.Errorf("the save on stop is from frame %d, the game stopped at %d", saves.sram[0], h.core.counter.Load())
	}
	if !h.core.unloaded || !h.core.closed {
		t.Error("the core was not unloaded and closed")
	}
}

// Unchanged save RAM is not uploaded again, however often it is checked.
func TestUnchangedSaveRAMIsNotUploaded(t *testing.T) {
	h := start(t, func(c *Config) {
		c.Core.(*fakeCore).onRun = func(fc *fakeCore) { fc.sram[0] = 7 } // constant after frame 1
	})
	time.Sleep(100 * time.Millisecond)
	h.stop(t)
	h.saves.mu.Lock()
	defer h.saves.mu.Unlock()
	if h.saves.sramPuts > 2 {
		t.Errorf("%d uploads of a save that stopped changing", h.saves.sramPuts)
	}
}

// A save state carries the core that wrote it, and loads back.
func TestSaveAndLoadState(t *testing.T) {
	h := start(t, nil)
	h.s.Pause()
	h.waitFor(t, "paused")
	at := h.core.counter.Load()
	h.s.SaveState("state-1")
	h.waitFor(t, "state-saved")
	h.s.Resume()
	h.waitFor(t, "resumed")
	time.Sleep(30 * time.Millisecond)
	h.s.Pause()
	h.waitFor(t, "paused")
	if h.core.counter.Load() == at {
		t.Fatal("the game did not run between save and load")
	}
	h.s.LoadState("state-1")
	h.waitFor(t, "state-loaded")
	if h.core.counter.Load() != at {
		t.Errorf("counter = %d after loading, want %d", h.core.counter.Load(), at)
	}
	h.saves.mu.Lock()
	cv := h.saves.stateCore["state-1"]
	h.saves.mu.Unlock()
	if cv != [2]string{"fakecore", "1.0"} {
		t.Errorf("state recorded as %v", cv)
	}
	h.stop(t)
}

// A state from another core, or another version of this one, is refused
// before it reaches the core — loaded, it would corrupt the emulator.
func TestStateFromAnotherCoreIsRefused(t *testing.T) {
	h := start(t, nil)
	h.saves.StoreState("state-2", []byte{1, 0, 0, 0}, "fakecore", "0.9")
	h.s.Pause()
	h.waitFor(t, "paused")
	before := h.core.counter.Load()
	h.s.LoadState("state-2")
	e := h.waitFor(t, "error")
	if e.Text == "" || h.core.counter.Load() != before {
		t.Errorf("event %+v, counter %d -> %d", e, before, h.core.counter.Load())
	}
	h.stop(t)
}

// Paused, no frames run; closing writes "auto" so the game resumes there.
func TestPauseAndAutoState(t *testing.T) {
	h := start(t, func(c *Config) { c.SaveStateOnStop = true })
	h.s.Pause()
	h.waitFor(t, "paused")
	n := h.core.counter.Load()
	time.Sleep(50 * time.Millisecond)
	if h.core.counter.Load() != n {
		t.Errorf("%d frames ran while paused", h.core.counter.Load()-n)
	}
	h.stop(t)
	h.saves.mu.Lock()
	auto := h.saves.states["auto"]
	h.saves.mu.Unlock()
	if len(auto) != 4 || binary.LittleEndian.Uint32(auto) != n {
		t.Errorf("auto state = %v, want the counter at stop (%d)", auto, n)
	}

	// And the next session resumes from it.
	h2 := start(t, func(c *Config) { c.Saves = h.saves; c.ResumeState = "auto" })
	h2.s.Pause()
	h2.waitFor(t, "paused")
	if h2.core.counter.Load() < n {
		t.Errorf("resumed at %d, want at least %d", h2.core.counter.Load(), n)
	}
	h2.stop(t)
}

// Escape opens the menu, which pauses the game on the frame it was asked.
func TestMenuPauses(t *testing.T) {
	h := start(t, nil)
	h.input.mu.Lock()
	h.input.escape = true
	h.input.mu.Unlock()
	h.waitFor(t, "menu")
	n := h.core.counter.Load()
	time.Sleep(30 * time.Millisecond)
	if h.core.counter.Load() != n {
		t.Error("the game ran on behind its menu")
	}
	h.stop(t)
}

// An option is the core's default until chosen, a chosen value the core
// does not offer falls back to the default, and the core is told it changed.
func TestOptions(t *testing.T) {
	h := start(t, func(c *Config) { c.Options = map[string]string{"colour": "purple"} })
	h.s.Pause()
	h.waitFor(t, "paused")
	if h.core.colour != "red" {
		t.Errorf("an undeclared value reached the core: %q", h.core.colour)
	}
	h.s.SetOption("colour", "blue")
	h.s.Resume()
	h.waitFor(t, "resumed")
	time.Sleep(20 * time.Millisecond)
	h.s.Pause()
	h.waitFor(t, "paused")
	if h.core.colour != "blue" {
		t.Errorf("colour = %q after choosing blue", h.core.colour)
	}
	h.stop(t)
}

// Frames reach the sink as BGRA at the declared aspect, and the controller
// reaches the core.
func TestFramesAndInput(t *testing.T) {
	h := start(t, nil)
	h.input.mu.Lock()
	h.input.pad = FromXInput(XInputGamepad{Buttons: XInputB}) // RetroPad A
	h.input.mu.Unlock()
	time.Sleep(30 * time.Millisecond)
	h.stop(t)
	h.video.mu.Lock()
	defer h.video.mu.Unlock()
	if h.video.frames == 0 || len(h.video.last) != 8 || h.video.last[2] != 0xFF || h.video.aspect != 2 {
		t.Errorf("frames %d last %v aspect %v", h.video.frames, h.video.last, h.video.aspect)
	}
	if h.core.sram[1] != 1 {
		t.Error("RetroPad A never reached the core")
	}
}

// A core that asks to shut down ends the session.
func TestCoreShutdownEndsTheSession(t *testing.T) {
	h := start(t, func(c *Config) {
		c.Core.(*fakeCore).onRun = func(fc *fakeCore) {
			if fc.counter.Load() == 3 {
				fc.fe.Shutdown()
			}
		}
	})
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the session ran on after the core shut down")
	}
}

// A save that cannot be fetched starts the game fresh and says so; it does
// not stop the game.
func TestUnfetchableSaveStartsFresh(t *testing.T) {
	saves := newFakeSaves()
	saves.failSRAM = errors.New("server away")
	h := &harness{events: make(chan Event, 64)}
	core := &fakeCore{name: "fakecore"}
	s := New(Config{Core: core, Saves: saves, OnEvent: func(e Event) { h.events <- e },
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	done := make(chan error, 1)
	go func() { done <- s.Run(context.Background()) }()
	e := h.waitFor(t, "error")
	if e.Slot != "sram" {
		t.Errorf("event = %+v", e)
	}
	h.waitFor(t, "started")
	s.Stop()
	<-done
}

// The page is told which options the core declared and their values, at
// start and again when one changes.
func TestOptionsAreAnnounced(t *testing.T) {
	var mu sync.Mutex
	var last []Option
	h := start(t, func(c *Config) {
		inner := c.OnEvent
		c.Options = map[string]string{"colour": "green"}
		c.OnEvent = func(e Event) {
			if e.Kind == "options" {
				mu.Lock()
				last = e.Options
				mu.Unlock()
			}
			inner(e)
		}
	})
	// Waited for, not slept for: the options are announced just after
	// "started", and a fixed sleep is a race the race detector's slowdown
	// can lose.
	var got []Option
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		mu.Lock()
		got = last
		mu.Unlock()
		if got != nil {
			break
		}
	}
	if len(got) != 1 || got[0].Key != "colour" || got[0].Value != "green" || len(got[0].Values) != 3 {
		t.Fatalf("options = %+v", got)
	}
	h.s.SetOption("colour", "blue")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		v := last[0].Value
		mu.Unlock()
		if v == "blue" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if last[0].Value != "blue" {
		t.Errorf("after choosing blue the page was told %q", last[0].Value)
	}
	h.stop(t)
}
