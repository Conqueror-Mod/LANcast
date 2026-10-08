package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"log/slog"
	"math"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
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

	// volume is a float64's bits, set from any goroutine (SetVolume); scaled
	// is the core thread's buffer for samples at less than full volume.
	volume atomic.Uint64
	scaled []int16

	stats frameStats // core thread only
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
	s := &Session{cfg: cfg, cmds: make(chan command, 16), vars: map[string]libretro.Variable{}}
	s.volume.Store(math.Float64bits(1))
	return s
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
 * Run plays the game until Stop, a core's own shutdown, or ctx ends.
 *
 * Every session in the process runs on one OS thread, the same one each
 * time — not merely a locked one. A locked thread per session was enough for
 * one game and crashed the second: Mupen64Plus-Next turns the thread it first
 * runs on into a fiber and keeps that fiber in a static, and the DLL stays
 * loaded between games. The next session, on another thread, switched to a
 * fiber that belonged to the old one, and the process died in KERNELBASE
 * (0xc0000005 writing 0x1e18). RetroArch never meets this because every core
 * it runs is on its main thread; one permanent thread gives cores the same
 * promise. Sessions never overlap — the client waits for the previous game
 * before starting the next — so one thread costs nothing.
 */
func (s *Session) Run(ctx context.Context) error {
	return onCoreThread(func() error { return s.run(ctx) })
}

var (
	coreThreadOnce sync.Once
	coreThreadWork chan func()
)

// onCoreThread runs f on the process's one core thread and waits for it. A
// panic is left to happen where it is, with its own stack: carrying it back
// to the caller would trade the trace that says why for one that says where
// it was re-raised, and the process ends either way.
func onCoreThread(f func() error) error {
	coreThreadOnce.Do(func() {
		coreThreadWork = make(chan func())
		go func() {
			// Never unlocked: the thread must outlive every session.
			runtime.LockOSThread()
			for w := range coreThreadWork {
				w()
			}
		}()
	})
	var err error
	done := make(chan struct{})
	coreThreadWork <- func() {
		defer close(done)
		err = f()
	}
	<-done
	return err
}

func (s *Session) run(ctx context.Context) (err error) {
	core := s.cfg.Core
	if core == nil {
		return ErrNoCore
	}
	s.info = core.SystemInfo()
	if err := core.Init(s); err != nil {
		return err
	}
	/*
	 * Registered before Close, so it runs after it: RetroArch's order, which
	 * cores are written against. The game is unloaded (finish), the core is
	 * deinitialised, and only then is it told its GL context is going, and
	 * the context goes. Mupen64Plus-Next still touches GL while unloading and
	 * deinitialising, and killed the process when the context had already
	 * been deleted under it.
	 */
	defer s.teardownGL()
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
	/*
	 * Continue. Most cores take a state as soon as the game is loaded, but
	 * Mupen64Plus-Next starts its emulator on the first retro_run and refuses
	 * any state before then. So a refusal here is retried after each frame
	 * for a while, rather than taken as final: taking it as final made
	 * Continue on every N64 game start from the beginning, with only a line
	 * in the log to say so.
	 */
	var resume []byte
	resumeSlot, resumeTries, resumed := s.cfg.ResumeState, 0, false
	if resumeSlot != "" {
		if data, c, v, err := s.cfg.Saves.LoadState(resumeSlot); err == nil && len(data) > 0 && s.stateMismatch(c, v) == "" {
			if resumed = core.Unserialize(data); !resumed {
				resume = data
			}
		}
	}
	s.cfg.OnEvent(Event{Kind: "started"})
	if resumed {
		// After "started", like the retried case, so the page hears it while
		// the game is on screen rather than over its loading message.
		s.cfg.OnEvent(Event{Kind: "state-loaded", Slot: resumeSlot})
	}
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
			// A pause is not a slow frame; the count starts again after it.
			s.stats = frameStats{}
			continue
		}
		runStart := time.Now()
		core.Run()
		s.frameDone(time.Since(runStart))
		if resume != nil {
			resumeTries++
			switch {
			case core.Unserialize(resume):
				resume = nil
				s.cfg.Log.Info("retro session: resumed after the core's first frames", "slot", resumeSlot, "frames", resumeTries)
				s.cfg.OnEvent(Event{Kind: "state-loaded", Slot: resumeSlot})
			case resumeTries >= resumeFrames:
				resume = nil
				s.cfg.Log.Warn("retro session: resume state refused by the core", "slot", resumeSlot, "frames", resumeTries)
				s.cfg.OnEvent(Event{Kind: "error", Slot: resumeSlot, Text: "This game could not carry on from where you left off, so it has started from the beginning. In-game saves are not affected."})
			}
		}
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
	s.cfg.Core.UnloadGame()
}

// teardownGL tells a hardware-rendering core its context is going, then
// deletes it. It runs after the core is deinitialised (see Run).
func (s *Session) teardownGL() {
	if !s.glReady {
		return
	}
	s.cfg.Core.ContextDestroy()
	s.cfg.GL.Close()
	s.glReady = false
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
	start := time.Now()
	defer func() { s.stats.present += time.Since(start) }()
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

/*
 * frameStats is where a game's frames go, logged every few seconds.
 *
 * Built when a game became unplayably laggy with a film playing in the
 * corner (ADR 0076), and nothing recorded why. A frame is the core's run —
 * inside which it presents the picture and hands over its sound — and then
 * the frame clock's sleep. Splitting run into presenting, waiting on audio
 * and the rest names which of the three a slow frame spent its time in:
 * a present stuck behind another presenter's vsync, a sound card draining
 * slowly, or emulation itself.
 */
type frameStats struct {
	since               time.Time
	frames              int
	run, present, audio time.Duration
	slowest, slowestRun time.Duration
	lastFrame           time.Time
}

const statsEvery = 5 * time.Second

func (s *Session) frameDone(runTook time.Duration) {
	now := time.Now()
	st := &s.stats
	if st.since.IsZero() {
		st.since, st.lastFrame = now, now
		return
	}
	st.frames++
	st.run += runTook
	if gap := now.Sub(st.lastFrame); gap > st.slowest {
		st.slowest = gap
	}
	if runTook > st.slowestRun {
		st.slowestRun = runTook
	}
	st.lastFrame = now
	if el := now.Sub(st.since); el >= statsEvery && st.frames > 0 {
		n := time.Duration(st.frames)
		s.cfg.Log.Info("retro frames",
			"fps", fmt.Sprintf("%.1f", float64(st.frames)/el.Seconds()),
			"target", fmt.Sprintf("%.1f", s.av.FPS),
			"run_avg", (st.run / n).Round(10*time.Microsecond),
			"present_avg", (st.present / n).Round(10*time.Microsecond),
			"audio_avg", (st.audio / n).Round(10*time.Microsecond),
			"run_max", st.slowestRun.Round(10*time.Microsecond),
			"frame_max", st.slowest.Round(10*time.Microsecond))
		*st = frameStats{since: now, lastFrame: now}
	}
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
		start := time.Now()
		s.cfg.Audio.Write(s.scale(samples))
		s.stats.audio += time.Since(start)
	}
	return len(samples) / 2
}

/*
 * SetVolume sets the game's own volume, 0 silent to 1 as the core made it,
 * from any goroutine (ADR 0076).
 *
 * Applied to the samples here rather than through waveOutSetVolume, which on
 * Windows Vista and later sets the whole application's audio session — the
 * one libmpv shares — so turning a game down would turn the film in the
 * corner down with it. Linear in amplitude: the menu offers a handful of
 * steps, not a fader, and each step is plainly quieter than the last.
 */
func (s *Session) SetVolume(v float64) {
	if v != v || v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	s.volume.Store(math.Float64bits(v))
}

// scale returns samples at the session's volume. Full volume hands the
// core's own slice through untouched; anything else is written to a buffer
// of the session's, never back into the core's memory.
func (s *Session) scale(samples []int16) []int16 {
	v := math.Float64frombits(s.volume.Load())
	if v >= 1 {
		return samples
	}
	if cap(s.scaled) < len(samples) {
		s.scaled = make([]int16, len(samples))
	}
	out := s.scaled[:len(samples)]
	for i, x := range samples {
		out[i] = int16(float64(x) * v)
	}
	return out
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

// resumeFrames is how long a refused Continue is retried: two seconds at
// 60 fps, far longer than any core yet seen needs to start its emulator.
const resumeFrames = 120

// ErrNoCore is returned when a session is started without a core.
var ErrNoCore = errors.New("retro session: no core")
