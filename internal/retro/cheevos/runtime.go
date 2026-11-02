package cheevos

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"lancast/internal/media"
	"lancast/internal/retro/libretro"
	"lancast/internal/retro/ra"
)

/*
 * Runtime is one game's achievements in the desktop player (ADR 0112 step
 * 4): the Client, driven from the game session.
 *
 * Every call into the DLL is made on the core's thread, and so is every
 * callback out of it. The answers to rcheevos' requests come back on other
 * goroutines; they wait in a channel and are handed to rcheevos in Frame or
 * Idle, so an unlock, a load finishing or a sign-in failing is always heard
 * on the core thread, between frames, never in the middle of one.
 *
 * The player signs in as ra.Placeholder. The server, which holds the token,
 * checks every request, puts the token in, and takes it out of the answers.
 */

// Sender posts one rcheevos request to the LANcast server and returns
// RetroAchievements' status and answer. An error means it should be tried
// again later (nothing usable came back); a refusal is an answer, with a
// status and a body in RetroAchievements' own shape.
type Sender func(ctx context.Context, body, userAgentClause string) (status int, answer []byte, err error)

// Notice is something the person should see while playing.
type Notice struct {
	// Kind is one of: ready (achievements loaded: Title, Total, Unlocked),
	// none (this game has no achievements, or is not recognised), off
	// (earning is off or not connected: Text says why), unlocked, mastered,
	// progress, progress-hide, offline (an unlock is waiting to be sent),
	// online (it went), problem (RetroAchievements refused something).
	Kind        string
	ID          uint32
	Title       string
	Description string
	Badge       string // LANcast's own URL for the picture, or ""
	Points      uint32
	Progress    string // "3/10", for progress
	Text        string
	Total       uint32
	Unlocked    uint32
}

// Achievement is one of the loaded game's achievements, for the menu.
type Achievement struct {
	ID          uint32 `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Badge       string `json:"badge,omitempty"`
	Points      uint32 `json:"points"`
	// State is locked, unlocked or unsupported (the set uses something this
	// core does not provide).
	State    string `json:"state"`
	Progress string `json:"progress,omitempty"`
}

// Summary is the loaded game's set, for the menu's Achievements page.
type Summary struct {
	GameID         uint32        `json:"game_id"`
	Title          string        `json:"title"`
	Badge          string        `json:"badge,omitempty"`
	Total          uint32        `json:"total"`
	Unlocked       uint32        `json:"unlocked"`
	Points         uint32        `json:"points"`
	PointsUnlocked uint32        `json:"points_unlocked"`
	RichPresence   string        `json:"rich_presence,omitempty"`
	Achievements   []Achievement `json:"achievements"`
}

// Memory is the game's memory as the session knows it, after the core has
// run long enough to have sent its map.
type Memory struct {
	Map       []libretro.MemoryDescriptor
	SystemRAM []byte
	SaveRAM   []byte
}

// ConsoleFor is RetroAchievements' console for a LANcast platform, or 0 for
// one it has none for.
func ConsoleFor(platform string) Console {
	switch platform {
	case media.PlatformGenesis:
		return MegaDrive
	case media.PlatformN64:
		return Nintendo64
	case media.PlatformSNES:
		return SNES
	case media.PlatformGB:
		return GameBoy
	case media.PlatformGBA:
		return GameBoyAdv
	case media.PlatformGBC:
		return GameBoyColor
	case media.PlatformNES:
		return NES
	case media.PlatformSMS:
		return MasterSystem
	case media.PlatformPS1:
		return PlayStation
	}
	return 0
}

// engine is what the runtime needs of a Client, so the runtime can be
// tested anywhere without the DLL.
type engine interface {
	Close()
	UserAgentClause() string
	Respond(id uint32, body []byte, status int)
	Login(username, token string) error
	SetMemory(console Console, mmap []libretro.MemoryDescriptor, system, save []byte) bool
	LoadGame(console Console, path string, data []byte)
	UnloadGame()
	DoFrame()
	Idle()
	Reset()
	Progress() ([]byte, error)
	RestoreProgress([]byte) error
	Game() (GameInfo, bool)
	Achievements() []Listed
	RichPresence() string
}

const (
	maxAnswer          = 16 << 20
	stopWaitForSending = 5 * time.Second
)

type answer struct {
	id     uint32
	status int
	body   []byte
}

// Runtime is one game's achievements. Its methods are called on the core's
// thread only.
type Runtime struct {
	open     func(Handler) (engine, error)
	send     Sender
	onNotice func(Notice)
	log      *slog.Logger

	ctx     context.Context
	cancel  context.CancelFunc
	sending sync.WaitGroup
	answers chan answer

	eng     engine
	clause  string
	started bool
	loaded  bool // the load finished well, and ready has not been said yet
	ready   bool
	off     bool
	// early is progress from a state loaded before the set had (Continue
	// does this: the state goes in at once, the set arrives over the
	// network), handed over when it does.
	early    []byte
	hasEarly bool
}

// New prepares a runtime around lancast-cheevos.dll beside the app. Nothing
// is loaded until Start.
func New(send Sender, onNotice func(Notice), log *slog.Logger) *Runtime {
	return newRuntime(func(h Handler) (engine, error) {
		c, err := Open(DLLPath(), h)
		if err != nil {
			return nil, err
		}
		return c, nil
	}, send, onNotice, log)
}

func newRuntime(open func(Handler) (engine, error), send Sender, onNotice func(Notice), log *slog.Logger) *Runtime {
	if log == nil {
		log = slog.Default()
	}
	if onNotice == nil {
		onNotice = func(Notice) {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Runtime{open: open, send: send, onNotice: onNotice, log: log,
		ctx: ctx, cancel: cancel, answers: make(chan answer, 64)}
}

// Start signs in and identifies the game. Console 0 starts nothing.
func (r *Runtime) Start(console Console, mem Memory, path string, data []byte) error {
	if console == 0 {
		r.onNotice(Notice{Kind: "none"})
		return nil
	}
	eng, err := r.open(r)
	if err != nil {
		return err
	}
	r.eng, r.started = eng, true
	r.clause = eng.UserAgentClause()
	if !eng.SetMemory(console, mem.Map, mem.SystemRAM, mem.SaveRAM) {
		r.log.Warn("rcheevos: the core exposes no memory for this console", "console", console)
	}
	// rcheevos waits for the sign-in before loading, so both start now.
	if err := eng.Login(ra.Placeholder, ra.Placeholder); err != nil {
		r.log.Warn("rcheevos: sign-in not started", "error", err)
	}
	eng.LoadGame(console, path, data)
	r.settle()
	return nil
}

// Frame runs after each emulated frame: answers that came back are handed
// to rcheevos, then it evaluates this frame.
func (r *Runtime) Frame() {
	if !r.started {
		return
	}
	r.deliver()
	if r.ready {
		r.eng.DoFrame()
	} else {
		r.eng.Idle()
	}
	r.settle()
}

// Idle keeps rcheevos' own timers (retries, the session ping) going while
// the game is paused.
func (r *Runtime) Idle() {
	if !r.started {
		return
	}
	r.deliver()
	r.eng.Idle()
	r.settle()
}

func (r *Runtime) deliver() {
	for {
		select {
		case a := <-r.answers:
			r.eng.Respond(a.id, a.body, a.status)
			r.settle()
		default:
			return
		}
	}
}

// settle finishes what a callback could only note: Done is called from
// inside the DLL, where it may not call back in, so "ready" and its counts
// are read here, once the call has returned.
func (r *Runtime) settle() {
	if !r.loaded {
		return
	}
	r.loaded, r.ready = false, true
	g, _ := r.eng.Game()
	r.onNotice(Notice{Kind: "ready", Title: g.Title, Total: g.Total, Unlocked: g.Unlocked})
	if r.hasEarly {
		r.hasEarly = false
		r.SetProgress(r.early)
		r.early = nil
	}
}

// Reset tells rcheevos the game was reset, so progress starts again.
func (r *Runtime) Reset() {
	if r.started {
		r.eng.Reset()
	}
}

// Progress is rcheevos' progress, to be kept with a save state, or nil.
func (r *Runtime) Progress() []byte {
	if !r.ready {
		return nil
	}
	p, err := r.eng.Progress()
	if err != nil {
		r.log.Warn("rcheevos: progress not kept with the state", "error", err)
		return nil
	}
	return p
}

// SetProgress restores progress kept with a state; nil means the state had
// none, and rcheevos starts its counting again from that point. Before the
// set has loaded it is held, and handed over when it does.
func (r *Runtime) SetProgress(b []byte) {
	if !r.ready {
		if !r.off {
			r.early, r.hasEarly = b, true
		}
		return
	}
	if err := r.eng.RestoreProgress(b); err != nil {
		r.log.Warn("rcheevos: progress from the state not restored", "error", err)
	}
}

// List is the loaded game's set, for the menu.
func (r *Runtime) List() Summary {
	s := Summary{Achievements: []Achievement{}}
	if !r.ready {
		return s
	}
	g, _ := r.eng.Game()
	s.GameID, s.Title = g.ID, g.Title
	if g.Badge != "" {
		s.Badge = mediaURL("/Images/" + g.Badge + ".png")
	}
	s.Total, s.Unlocked, s.Points, s.PointsUnlocked = g.Total, g.Unlocked, g.Points, g.PointsUnlocked
	s.RichPresence = r.eng.RichPresence()
	for _, a := range r.eng.Achievements() {
		state := "locked"
		switch a.State {
		case Unlocked:
			state = "unlocked"
		case Unsupported:
			state = "unsupported"
		}
		s.Achievements = append(s.Achievements, Achievement{ID: a.ID, Title: a.Title, Description: a.Description,
			Badge: badgeURL(a.Badge, a.State == Unlocked), Points: a.Points, State: state, Progress: a.Progress})
	}
	return s
}

// Stop unloads the game. An unlock already on its way is given a few
// seconds to reach the server, which then finishes it whatever happens.
func (r *Runtime) Stop() {
	defer r.cancel()
	if !r.started {
		return
	}
	done := make(chan struct{})
	go func() { r.sending.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(stopWaitForSending):
	}
	r.deliver()
	r.eng.UnloadGame()
	r.eng.Close()
	r.started, r.ready = false, false
}

// Request is rcheevos asking for a request to be sent (Handler). It returns
// at once; the answer is handed back on the core thread in Frame or Idle.
func (r *Runtime) Request(q Request) {
	r.sending.Add(1)
	go func() {
		defer r.sending.Done()
		status, body, err := r.send(r.ctx, q.PostData, r.clause)
		a := answer{id: q.ID, status: status, body: body}
		if err != nil {
			a.status, a.body = StatusRetry, nil
		}
		if len(a.body) > maxAnswer {
			a.status, a.body = StatusFail, nil
		}
		select {
		case r.answers <- a:
		case <-r.ctx.Done():
		}
	}()
}

// Event turns what rcheevos raised into what the page shows (Handler).
func (r *Runtime) Event(e Event) {
	n := Notice{ID: e.ID, Title: e.Title, Description: e.Description, Points: e.Points}
	switch e.Type {
	case EventAchievementTriggered:
		n.Kind, n.Badge = "unlocked", badgeURL(e.Badge, true)
	case EventProgressIndicatorShow, EventProgressIndicatorUpdate:
		n.Kind, n.Progress = "progress", e.Text
	case EventProgressIndicatorHide:
		n = Notice{Kind: "progress-hide"}
	case EventGameCompleted:
		n = Notice{Kind: "mastered"}
	case EventDisconnected:
		n = Notice{Kind: "offline"}
	case EventReconnected:
		n = Notice{Kind: "online"}
	case EventServerError:
		r.log.Warn("rcheevos: RetroAchievements refused a request", "api", e.Title, "error", e.Text)
		n = Notice{Kind: "problem", Text: e.Text}
	default:
		return
	}
	r.onNotice(n)
}

// Log passes rcheevos' own lines to the app's log (Handler).
func (r *Runtime) Log(s string) { r.log.Info("rcheevos", "message", s) }

// Done is the end of the sign-in or the load (Handler).
func (r *Runtime) Done(op Op, result int, msg string) {
	switch op {
	case OpLogin:
		if result != OK {
			r.off = true
			r.early, r.hasEarly = nil, false
			r.log.Info("rcheevos: not signed in", "code", result, "error", msg)
			r.onNotice(Notice{Kind: "off", Text: msg})
		}
	case OpLoadGame:
		switch {
		case result == OK:
			r.loaded = true
		case r.off:
			// Already said: the sign-in failed, and the load with it.
		default:
			r.log.Info("rcheevos: no achievements for this game", "code", result, "error", msg)
			r.onNotice(Notice{Kind: "none", Text: msg})
		}
	}
}

// badgeURL is LANcast's route for an achievement's badge: its locked
// picture until it is unlocked.
func badgeURL(name string, unlocked bool) string {
	if name == "" {
		return ""
	}
	if !unlocked {
		name += "_lock"
	}
	return mediaURL("/Badge/" + name + ".png")
}

// mediaURL turns a RetroAchievements picture path into LANcast's own route
// for it (ADR 0090 §5), so the page never fetches from RetroAchievements.
// Anything else is dropped.
func mediaURL(p string) string {
	if !ra.MediaPath(p) {
		return ""
	}
	return "/api/retroachievements/media" + p
}
