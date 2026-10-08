package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"log/slog"
	"runtime"
	"sort"
	"sync"
	"time"

	"lancast/internal/retro/libretro"
)

// VideoSink shows a frame. Called on the core's thread with BGRA rows the
// session reuses for the next frame, so it must draw or copy before returning.
type VideoSink interface {
	Present(bgra []byte, width, height int, aspect float64)
}

// AudioSink plays interleaved stereo samples. Write blocks while its buffer
// is full, and that blocking is what paces the game: the sound card's clock
// is the one clock that must not drift, because drift is audible.
type AudioSink interface {
	Open(sampleRate int) error
	Write(samples []int16)
	Close()
}

/*
 * GLSink is the GPU path a hardware-rendering core draws through (stage 3).
 *
 * Every method is called on the core's locked thread, with the context the
 * sink made current there: OpenGL contexts belong to a thread, and a core
 * that found its context current somewhere else would draw into nothing.
 */
type GLSink interface {
	// Init creates a context the core asked for and a framebuffer at least
	// maxW by maxH for it to draw into.
	Init(req libretro.HWRender, maxW, maxH int) error
	Framebuffer() uintptr
	ProcAddress(name string) uintptr
	// Present shows the part of the framebuffer the core drew this frame.
	Present(width, height int, aspect float64, bottomLeftOrigin bool)
	Close()
}

// InputSource reports the controllers once per frame.
type InputSource interface {
	Poll() (pads [2]Pad, guide, escape bool)
}

// SaveStore is where saves go — the server, through the player's ticket. Its
// calls may be slow and are never made from inside a frame.
type SaveStore interface {
	// LoadSRAM returns the saved game RAM, or nil with no error when there
	// is none yet.
	LoadSRAM() ([]byte, error)
	StoreSRAM(data []byte) error
	StoreState(slot string, data []byte, core, version string) error
	// LoadState returns a state and the core that wrote it.
	LoadState(slot string) (data []byte, core, version string, err error)
}

// Event is something the person should hear about.
type Event struct {
	Kind string // started, options, paused, resumed, menu, state-saved, state-loaded, sram-saved, message, error, stopped
	Slot string
	Text string
	Err  error
	// Options, on an "options" event, are the core options the core
	// declared, each with its current value.
	Options []Option
}

// Option is one core option as a menu shows it.
type Option struct {
	Key         string   `json:"key"`
	Description string   `json:"description"`
	Values      []string `json:"values"`
	Value       string   `json:"value"`
}

// Config is one game session.
type Config struct {
	Core      libretro.Core
	GamePath  string
	GameData  []byte
	SystemDir string
	SaveDir   string
	// Options are the person's choices for core options, by key. A key the
	// core does not declare is ignored; an undeclared value is replaced by the
	// core's default rather than handed over.
	Options map[string]string

	Video VideoSink
	// GL is the GPU path, for cores that render through OpenGL. Nil refuses
	// a core's request for a hardware context, which a framebuffer core never
	// makes and an N64 core cannot run without.
	GL    GLSink
	Audio AudioSink // nil runs on a timer instead
	Input InputSource
	Saves SaveStore
	// ResumeState loads this slot after the game starts, typically "auto",
	// which Stop writes. Empty starts the game fresh.
	ResumeState string
	// SaveStateOnStop writes the "auto" slot when the game is closed.
	SaveStateOnStop bool
	// FlushEvery is how often changed save RAM is sent to the server while
	// playing. Zero means every five seconds.
	FlushEvery time.Duration

	OnEvent func(Event)
	Log     *slog.Logger
}

type command struct {
	kind string // pause, resume, stop, reset, save-state, apply-state, set-option
	slot string
	key  string
	val  string
	data []byte
}

// Session runs one game. Its exported methods may be called from any
// goroutine; everything that touches the core happens on Run's thread.
type Session struct {
	cfg  Config
	cmds chan command
	info libretro.SystemInfo

	// Core-thread state.
	format    libretro.PixelFormat
	vars      map[string]libretro.Variable
	varsDirty bool
	av        libretro.AVInfo
	bgra      []byte
	pads      [2]Pad
	menu      MenuRequest
	paused    bool
	stopping  bool
	sramSum   uint32
	sramAt    time.Time
	hw        *libretro.HWRender // what the core asked for, if anything
	glReady   bool

	pending sync.WaitGroup // uploads in flight
}

// New prepares a session. Nothing runs until Run.
func New(cfg Config) *Session {
	if cfg.FlushEvery <= 0 {
		cfg.FlushEvery = 5 * time.Second
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.OnEvent == nil {
		cfg.OnEvent = func(Event) {}
	}
	return &Session{cfg: cfg, cmds: make(chan command, 16), vars: map[string]libretro.Variable{}}
}

func (s *Session) send(c command) {
	select {
	case s.cmds <- c:
	default:
		// A full queue is a person pressing faster than frames run; the
		// newest press is the one to drop rather than block the caller.
		s.cfg.Log.Warn("retro session: command dropped", "kind", c.kind)
	}
}

func (s *Session) Pause()  { s.send(command{kind: "pause"}) }
func (s *Session) Resume() { s.send(command{kind: "resume"}) }
func (s *Session) Reset()  { s.send(command{kind: "reset"}) }
func (s *Session) Stop()   { s.send(command{kind: "stop"}) }

// SaveState writes the game's state to a slot.
func (s *Session) SaveState(slot string) { s.send(command{kind: "save-state", slot: slot}) }

// LoadState fetches a slot and applies it. The fetch happens off the core's
// thread; the core only sees the bytes once they have arrived.
func (s *Session) LoadState(slot string) {
	go func() {
		data, core, ver, err := s.cfg.Saves.LoadState(slot)
		if err != nil {
			s.cfg.OnEvent(Event{Kind: "error", Slot: slot, Text: "That save could not be fetched.", Err: err})
			return
		}
		if msg := s.stateMismatch(core, ver); msg != "" {
			s.cfg.OnEvent(Event{Kind: "error", Slot: slot, Text: msg})
			return
		}
		s.send(command{kind: "apply-state", slot: slot, data: data})
	}()
}

// SetOption changes a core option; the core sees it on its next frame.
func (s *Session) SetOption(key, value string) {
	s.send(command{kind: "set-option", key: key, val: value})
}

/*
 * stateMismatch explains why a state cannot be loaded, or returns "".
 *
 * A save state is a memory dump of one build of one emulator. Loaded into
 * another it does not fail cleanly — it corrupts the emulator's memory and
 * usually takes the process with it. So the refusal is here, before the
 * bytes reach the core, and says what would make it loadable.
 */
func (s *Session) stateMismatch(core, version string) string {
	if core == "" {
		return ""
	}
	if core != s.info.LibraryName || version != s.info.LibraryVersion {
		return fmt.Sprintf("This save was made by %s %s, and this game is running on %s %s. In-game saves still work; a save state only loads into the version that made it.",
			core, version, s.info.LibraryName, s.info.LibraryVersion)
	}
	return ""
}

/*
 * Run plays the game until Stop, a core's own shutdown, or ctx ends. It
 * locks its OS thread for its whole life: libretro assumes one thread, and
 * the OpenGL cores of stage 3 cannot survive moving.
 */
func (s *Session) Run(ctx context.Context) (err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	core := s.cfg.Core
	if core == nil {
		return ErrNoCore
	}
	s.info = core.SystemInfo()
	if err := core.Init(s); err != nil {
		return err
	}
	defer core.Close()
	if err := core.LoadGame(s.cfg.GamePath, s.cfg.GameData); err != nil {
		return err
	}
	s.av = core.AVInfo()
	/*
	 * A hardware-rendering core asked for its context during LoadGame; it is
	 * made now, on this thread, and only then is the core told it exists.
	 * The framebuffer is sized to the largest picture the core declared, so
	 * a resolution change mid-game never needs a new one.
	 */
	if s.hw != nil {
		w, h := int(s.av.MaxWidth), int(s.av.MaxHeight)
		if w <= 0 || h <= 0 {
			w, h = int(s.av.BaseWidth), int(s.av.BaseHeight)
		}
		if err := s.cfg.GL.Init(*s.hw, w, h); err != nil {
			core.UnloadGame()
			return fmt.Errorf("the graphics this game needs could not start: %w", err)
		}
		s.glReady = true
		core.ContextReset()
	}
	core.SetControllerPortDevice(0, libretro.DeviceJoypad)
	core.SetControllerPortDevice(1, libretro.DeviceJoypad)

	if s.cfg.Audio != nil {
		if err := s.cfg.Audio.Open(int(s.av.SampleRate + 0.5)); err != nil {
			s.cfg.Log.Warn("retro session: no sound", "error", err)
			s.cfg.Audio = nil
		} else {
			defer s.cfg.Audio.Close()
		}
	}

	s.loadSRAM()
	if slot := s.cfg.ResumeState; slot != "" {
		if data, c, v, err := s.cfg.Saves.LoadState(slot); err == nil && len(data) > 0 && s.stateMismatch(c, v) == "" {
			if !core.Unserialize(data) {
				s.cfg.Log.Warn("retro session: resume state refused by the core", "slot", slot)
			}
		}
	}
	s.cfg.OnEvent(Event{Kind: "started"})
	s.announceOptions()

	defer func() {
		s.finish()
		s.cfg.OnEvent(Event{Kind: "stopped", Err: err})
	}()

	frame := time.Duration(float64(time.Second) / clampFPS(s.av.FPS))
	next := time.Now()
	for !s.stopping {
		if ctx.Err() != nil {
			return nil
		}
		s.drain(s.paused)
		if s.stopping {
			break
		}
		if s.paused {
			continue
		}
		core.Run()
		s.maybeFlushSRAM(false)
		/*
		 * The frame clock, always — not only when there is no sound.
		 *
		 * With sound, the audio sink blocks while its buffers are full and
		 * that sets the pace. But it only blocks if the core feeds it a
		 * frame's worth of samples per frame, and a core that emits less (or
		 * a stretch of none) would otherwise run as fast as the CPU allows:
		 * the binding's test core did, at 3,458 frames in 350ms. So the clock
		 * runs too, two per cent fast when sound is playing so the sound
		 * card stays the one that decides whenever it is being fed — two
		 * clocks at the same rate drift, and drift against audio is a
		 * crackle.
		 */
		period := frame
		if s.cfg.Audio != nil {
			period = time.Duration(float64(frame) / 1.02)
		}
		next = next.Add(period)
		if d := time.Until(next); d > 0 {
			time.Sleep(d)
		} else if d < -time.Second {
			// A long stall (a breakpoint, a sleeping laptop) is not caught
			// up by running hundreds of frames at once.
			next = time.Now()
		}
	}
	return nil
}

func clampFPS(f float64) float64 {
	if f < 1 || f > 240 {
		return 60
	}
	return f
}

// drain handles queued commands; while paused it waits for one.
func (s *Session) drain(block bool) {
	for {
		var c command
		if block {
			c = <-s.cmds
		} else {
			select {
			case c = <-s.cmds:
			default:
				return
			}
		}
		s.handle(c)
		if s.stopping || (block && !s.paused) {
			return
		}
		block = s.paused
	}
}

func (s *Session) handle(c command) {
	core := s.cfg.Core
	switch c.kind {
	case "pause":
		if !s.paused {
			s.paused = true
			s.maybeFlushSRAM(true)
			s.cfg.OnEvent(Event{Kind: "paused"})
		}
	case "resume":
		if s.paused {
			s.paused = false
			s.cfg.OnEvent(Event{Kind: "resumed"})
		}
	case "stop":
		s.stopping = true
	case "reset":
		core.Reset()
	case "set-option":
		if v, ok := s.vars[c.key]; ok {
			s.cfg.Options = cloneWith(s.cfg.Options, c.key, validOption(v, c.val))
			s.varsDirty = true
			s.announceOptions()
		}
	case "save-state":
		data, ok := s.serialize()
		if !ok {
			s.cfg.OnEvent(Event{Kind: "error", Slot: c.slot, Text: "This game cannot be saved here."})
			return
		}
		s.upload(func() error {
			return s.cfg.Saves.StoreState(c.slot, data, s.info.LibraryName, s.info.LibraryVersion)
		}, Event{Kind: "state-saved", Slot: c.slot}, "That save did not reach the server.")
	case "apply-state":
		if core.Unserialize(c.data) {
			s.cfg.OnEvent(Event{Kind: "state-loaded", Slot: c.slot})
		} else {
			s.cfg.OnEvent(Event{Kind: "error", Slot: c.slot, Text: "The game refused that save."})
		}
	}
}

/*
 * announceOptions tells the page which options the core declared and what
 * each is set to. The page decides which few to show (ADR 0073 asks for a
 * short curated list, not every switch a core has); the session reports
 * what is true.
 */
func (s *Session) announceOptions() {
	if len(s.vars) == 0 {
		return
	}
	keys := make([]string, 0, len(s.vars))
	for k := range s.vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Option, 0, len(keys))
	for _, k := range keys {
		v := s.vars[k]
		val, _ := s.Variable(k)
		out = append(out, Option{Key: k, Description: v.Description, Values: v.Values, Value: val})
	}
	s.cfg.OnEvent(Event{Kind: "options", Options: out})
}

func (s *Session) serialize() ([]byte, bool) {
	n := s.cfg.Core.SerializeSize()
	if n <= 0 {
		return nil, false
	}
	data := make([]byte, n)
	if !s.cfg.Core.Serialize(data) {
		return nil, false
	}
	return data, true
}

// upload runs a store in the background, so the network never costs a frame.
func (s *Session) upload(store func() error, ok Event, failText string) {
	s.pending.Add(1)
	go func() {
		defer s.pending.Done()
		if err := store(); err != nil {
			s.cfg.Log.Warn("retro session: save upload failed", "slot", ok.Slot, "error", err)
			s.cfg.OnEvent(Event{Kind: "error", Slot: ok.Slot, Text: failText, Err: err})
			return
		}
		s.cfg.OnEvent(ok)
	}()
}

// loadSRAM puts the saved game RAM into the core before the first frame.
func (s *Session) loadSRAM() {
	mem := s.cfg.Core.Memory(libretro.MemorySaveRAM)
	if mem == nil {
		return
	}
	data, err := s.cfg.Saves.LoadSRAM()
	if err != nil {
		// Playing on is right: the game starts as if new, and the save on
		// the server is untouched until this one is newer.
		s.cfg.OnEvent(Event{Kind: "error", Slot: "sram", Text: "Your saved game could not be fetched.", Err: err})
	} else if len(data) > 0 {
		copy(mem, data)
	}
	s.sramSum = crc32.ChecksumIEEE(mem)
	s.sramAt = time.Now()
}

/*
 * maybeFlushSRAM sends save RAM to the server when it has changed.
 *
 * Checked at most every FlushEvery, by checksum, so a game that writes its
 * battery RAM every frame (some do) costs one upload per interval rather than
 * one per frame. force skips the interval: pausing and closing must not lose
 * the last few seconds of a save someone just made.
 */
func (s *Session) maybeFlushSRAM(force bool) {
	if !force && time.Since(s.sramAt) < s.cfg.FlushEvery {
		return
	}
	s.sramAt = time.Now()
	mem := s.cfg.Core.Memory(libretro.MemorySaveRAM)
	if mem == nil {
		return
	}
	sum := crc32.ChecksumIEEE(mem)
	if sum == s.sramSum {
		return
	}
	s.sramSum = sum
	data := bytes.Clone(mem)
	s.upload(func() error { return s.cfg.Saves.StoreSRAM(data) },
		Event{Kind: "sram-saved", Slot: "sram"}, "Your game's save did not reach the server.")
}

// finish writes what closing must not lose, and waits for it to be sent.
func (s *Session) finish() {
	s.maybeFlushSRAM(true)
	if s.cfg.SaveStateOnStop {
		if data, ok := s.serialize(); ok {
			s.upload(func() error {
				return s.cfg.Saves.StoreState("auto", data, s.info.LibraryName, s.info.LibraryVersion)
			}, Event{Kind: "state-saved", Slot: "auto"}, "Where you stopped could not be saved.")
		}
	}
	done := make(chan struct{})
	go func() { s.pending.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		s.cfg.Log.Warn("retro session: saves still uploading after 30s; closing anyway")
	}
	// The core lets go of its GPU objects while the context still exists,
	// then the game is unloaded, then the context goes.
	if s.glReady {
		s.cfg.Core.ContextDestroy()
	}
	s.cfg.Core.UnloadGame()
	if s.glReady {
		s.cfg.GL.Close()
		s.glReady = false
	}
}

func cloneWith(m map[string]string, k, v string) map[string]string {
	out := make(map[string]string, len(m)+1)
	for a, b := range m {
		out[a] = b
	}
	out[k] = v
	return out
}

func validOption(v libretro.Variable, val string) string {
	for _, x := range v.Values {
		if x == val {
			return val
		}
	}
	return v.Default()
}

// ---- libretro.Frontend, called on the core's thread ----

func (s *Session) SetPixelFormat(f libretro.PixelFormat) bool {
	s.format = f
	return true
}

func (s *Session) SystemDirectory() string { return s.cfg.SystemDir }
func (s *Session) SaveDirectory() string   { return s.cfg.SaveDir }

func (s *Session) Variable(key string) (string, bool) {
	v, ok := s.vars[key]
	if !ok {
		return "", false
	}
	if val, ok := s.cfg.Options[key]; ok {
		return validOption(v, val), true
	}
	return v.Default(), true
}

func (s *Session) SetVariables(vars []libretro.Variable) {
	s.vars = make(map[string]libretro.Variable, len(vars))
	for _, v := range vars {
		s.vars[v.Key] = v
	}
	s.varsDirty = true
}

func (s *Session) VariablesChanged() bool {
	d := s.varsDirty
	s.varsDirty = false
	return d
}

func (s *Session) SetGeometry(av libretro.AVInfo) {
	s.av.BaseWidth, s.av.BaseHeight = av.BaseWidth, av.BaseHeight
	s.av.AspectRatio = av.AspectRatio
}

func (s *Session) SetSystemAVInfo(av libretro.AVInfo) {
	rate := s.av.SampleRate
	s.av = av
	if s.cfg.Audio != nil && av.SampleRate != rate {
		s.cfg.Audio.Close()
		if err := s.cfg.Audio.Open(int(av.SampleRate + 0.5)); err != nil {
			s.cfg.Log.Warn("retro session: sound could not follow the core's new rate", "error", err)
			s.cfg.Audio = nil
		}
	}
}

func (s *Session) Message(text string, _ uint32) {
	s.cfg.OnEvent(Event{Kind: "message", Text: text})
}

func (s *Session) Shutdown() { s.stopping = true }

func (s *Session) VideoRefresh(f libretro.Frame) {
	if f.HW {
		if s.glReady {
			s.cfg.GL.Present(int(f.Width), int(f.Height), s.aspect(f), s.hw.BottomLeftOrigin)
		}
		return
	}
	if f.Data == nil || s.cfg.Video == nil {
		return // a repeated frame: what is on screen is already right
	}
	f.Format = s.format
	s.bgra = ToBGRA(f, s.bgra)
	s.cfg.Video.Present(s.bgra, int(f.Width), int(f.Height), s.aspect(f))
}

// aspect is the display aspect for a frame. A core that changes resolution
// mid-game without saying so keeps the declared aspect.
func (s *Session) aspect(f libretro.Frame) float64 {
	if s.av.AspectRatio > 0 {
		return float64(s.av.AspectRatio)
	}
	if f.Height == 0 {
		return 4.0 / 3.0
	}
	return float64(f.Width) / float64(f.Height)
}

/*
 * SetHWRender accepts desktop OpenGL — compatibility or core profile — when
 * there is a GL sink, and nothing else. OpenGL ES and Vulkan are refused, so
 * a core that can fall back to another renderer does, and one that cannot
 * says so rather than drawing into a context it did not ask for.
 */
func (s *Session) SetHWRender(req libretro.HWRender) bool {
	if s.cfg.GL == nil {
		return false
	}
	if req.Context != libretro.HWContextOpenGL && req.Context != libretro.HWContextOpenGLCore {
		return false
	}
	r := req
	s.hw = &r
	return true
}

func (s *Session) CurrentFramebuffer() uintptr {
	if !s.glReady {
		return 0
	}
	return s.cfg.GL.Framebuffer()
}

func (s *Session) ProcAddress(name string) uintptr {
	if s.cfg.GL == nil {
		return 0
	}
	return s.cfg.GL.ProcAddress(name)
}

func (s *Session) AudioBatch(samples []int16) int {
	if s.cfg.Audio != nil {
		s.cfg.Audio.Write(samples)
	}
	return len(samples) / 2
}

func (s *Session) InputPoll() {
	if s.cfg.Input == nil {
		return
	}
	pads, guide, escape := s.cfg.Input.Poll()
	s.pads = pads
	if s.menu.Update(pads[0], guide, escape, s.av.FPS) {
		// The menu pauses the game from inside the frame that asked; the
		// person sees the menu and the game stops on the same frame.
		s.paused = true
		s.maybeFlushSRAM(true)
		s.cfg.OnEvent(Event{Kind: "menu"})
	}
}

func (s *Session) InputState(port, device, index, id uint32) int16 {
	if port > 1 {
		return 0
	}
	return s.pads[port].State(device, index, id)
}

// ErrNoCore is returned when a session is started without a core.
var ErrNoCore = errors.New("retro session: no core")
