//go:build windows

package cheevos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"lancast/internal/retro/libretro"
	"lancast/internal/retro/ra"
)

/*
 * The binding to rcheevos.dll, which is rcheevos with LANcast's lancast_rc.c
 * (third_party/rcheevos). Loaded by full path through syscall, as libmpv and
 * the cores are, with no cgo.
 *
 * rcheevos is one client per process, and its callbacks carry no context,
 * so one Runtime is active at a time: the player plays one game at a time.
 */

// rcOK and the two "nothing came back" codes rcheevos defines for a host's
// HTTP answer (rc_api_request.h).
const (
	rcOK                  = 0
	responseClientError   = -1
	responseRetryableFail = -2
)

// What lancast_rc.c's done callback reports.
const (
	opLogin    = 1
	opLoadGame = 2
)

// rcheevos' event types (rc_client.h, v12.5), and the shim's list item.
const (
	evTriggered        = 1
	evProgressShow     = 7
	evProgressHide     = 8
	evProgressUpdate   = 9
	evGameCompleted    = 15
	evServerError      = 16
	evDisconnected     = 17
	evReconnected      = 18
	itemAchievement    = 1000
	maxCString         = 1 << 16
	maxAnswer          = 16 << 20
	stopWaitForSending = 5 * time.Second
)

var (
	kernel32      = syscall.NewLazyDLL("kernel32.dll")
	procGetHeap   = kernel32.NewProc("GetProcessHeap")
	procHeapAlloc = kernel32.NewProc("HeapAlloc")
	procHeapFree  = kernel32.NewProc("HeapFree")

	loadMu   sync.Mutex
	lib      *dll
	cbOnce   sync.Once
	cbHTTP   uintptr
	cbEvent  uintptr
	cbDone   uintptr
	cbLog    uintptr
	cbMemory uintptr

	activeMu sync.Mutex
	active   *Runtime
)

type dll struct {
	d     *syscall.DLL
	procs map[string]*syscall.Proc
}

var exports = []string{
	"lrc_init", "lrc_shutdown", "lrc_respond", "lrc_user_agent_clause", "lrc_login_with_token",
	"lrc_set_memory", "lrc_load_game", "lrc_unload_game", "lrc_do_frame", "lrc_idle", "lrc_reset",
	"lrc_progress_size", "lrc_progress_save", "lrc_progress_load", "lrc_game", "lrc_summary",
	"lrc_list_achievements", "lrc_rich_presence",
}

// Load loads rcheevos.dll from path, once per process. An error is a client
// without achievements, which plays exactly as before.
func Load(path string) error {
	loadMu.Lock()
	defer loadMu.Unlock()
	if lib != nil {
		return nil
	}
	d, err := syscall.LoadDLL(path)
	if err != nil {
		return fmt.Errorf("rcheevos: %w", err)
	}
	l := &dll{d: d, procs: map[string]*syscall.Proc{}}
	for _, name := range exports {
		p, err := d.FindProc(name)
		if err != nil {
			return fmt.Errorf("rcheevos: %s is not a LANcast build (%w)", path, err)
		}
		l.procs[name] = p
	}
	cbOnce.Do(func() {
		cbHTTP = syscall.NewCallback(httpCB)
		cbEvent = syscall.NewCallback(eventCB)
		cbDone = syscall.NewCallback(doneCB)
		cbLog = syscall.NewCallback(logCB)
		cbMemory = syscall.NewCallback(memoryCB)
	})
	lib = l
	return nil
}

func call(name string, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(lib.procs[name].Addr(), args...)
	return r
}

// cstr is s as a NUL-terminated byte slice, valid while the slice lives.
func cstr(s string) []byte { return append([]byte(s), 0) }

func goString(p uintptr) string {
	if p == 0 {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(nil), p+uintptr(n))) != 0 {
		n++
		if n >= maxCString {
			break
		}
	}
	return string(unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(nil), p)), n))
}

func cAlloc(n int) unsafe.Pointer {
	h, _, _ := procGetHeap.Call()
	p, _, _ := procHeapAlloc.Call(h, 0, uintptr(max(n, 1)))
	return unsafe.Add(unsafe.Pointer(nil), p)
}

func cFree(p unsafe.Pointer) {
	if p == nil {
		return
	}
	h, _, _ := procGetHeap.Call()
	_, _, _ = procHeapFree.Call(h, 0, uintptr(p))
}

type answer struct {
	callback, data uintptr
	status         int
	body           []byte
}

// Runtime is one game's achievements. Start, Frame, Idle, Reset, Progress,
// SetProgress, List and Stop are called on the core's thread only.
type Runtime struct {
	send    Sender
	onEvent func(Event)
	log     *slog.Logger

	ctx     context.Context
	cancel  context.CancelFunc
	sending sync.WaitGroup
	answers chan answer

	core    libretro.Core
	game    unsafe.Pointer // a C copy of the game's bytes, for hashing
	started bool
	ready   bool
	off     bool
	listing []Achievement
}

// New prepares a runtime. Nothing is loaded until Start.
func New(send Sender, onEvent func(Event), log *slog.Logger) *Runtime {
	if log == nil {
		log = slog.Default()
	}
	if onEvent == nil {
		onEvent = func(Event) {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Runtime{send: send, onEvent: onEvent, log: log, ctx: ctx, cancel: cancel, answers: make(chan answer, 64)}
}

// Start signs in and identifies the game, after the core has loaded it.
// console is Console(platform); 0 starts nothing.
func (r *Runtime) Start(core libretro.Core, console uint32, path string, data []byte) error {
	if lib == nil {
		return errors.New("rcheevos is not loaded")
	}
	if console == 0 {
		r.onEvent(Event{Kind: "none"})
		return nil
	}
	activeMu.Lock()
	if active != nil {
		activeMu.Unlock()
		return errors.New("rcheevos: another game is already running")
	}
	active = r
	activeMu.Unlock()

	if call("lrc_init", cbHTTP, cbEvent, cbDone, cbLog) != 0 {
		r.release()
		return errors.New("rcheevos could not start")
	}
	r.started = true
	r.core = core
	var mmap uintptr
	if m, ok := core.(libretro.MemoryMapper); ok {
		mmap = m.MemoryMap()
	}
	if call("lrc_set_memory", uintptr(console), mmap, cbMemory) == 0 {
		r.log.Warn("rcheevos: the core exposes no memory for this console", "console", console)
	}
	user := cstr(ra.Placeholder)
	call("lrc_login_with_token", uintptr(unsafe.Pointer(&user[0])), uintptr(unsafe.Pointer(&user[0])))

	// rcheevos waits for the sign-in before loading, so both start now. The
	// game's bytes are copied out of Go memory: rcheevos may hash them after
	// this call returns.
	p := cstr(path)
	var ptr unsafe.Pointer
	if len(data) > 0 {
		r.game = cAlloc(len(data))
		copy(unsafe.Slice((*byte)(r.game), len(data)), data)
		ptr = r.game
	}
	call("lrc_load_game", uintptr(console), uintptr(unsafe.Pointer(&p[0])), uintptr(ptr), uintptr(len(data)))
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
		call("lrc_do_frame")
	} else {
		call("lrc_idle")
	}
}

// Idle keeps rcheevos' own timers (retries, the session ping) going while
// the game is paused.
func (r *Runtime) Idle() {
	if !r.started {
		return
	}
	r.deliver()
	call("lrc_idle")
}

func (r *Runtime) deliver() {
	for {
		select {
		case a := <-r.answers:
			var ptr unsafe.Pointer
			if len(a.body) > 0 {
				ptr = unsafe.Pointer(&a.body[0])
			}
			call("lrc_respond", a.callback, a.data, uintptr(ptr), uintptr(len(a.body)), uintptr(a.status))
		default:
			return
		}
	}
}

// Reset tells rcheevos the game was reset, so progress starts again.
func (r *Runtime) Reset() {
	if r.started {
		call("lrc_reset")
	}
}

// Progress is rcheevos' progress, to be kept with a save state, or nil.
func (r *Runtime) Progress() []byte {
	if !r.ready {
		return nil
	}
	n := int(call("lrc_progress_size"))
	if n <= 0 || n > 64<<20 {
		return nil
	}
	b := make([]byte, n)
	if int32(call("lrc_progress_save", uintptr(unsafe.Pointer(&b[0])), uintptr(n))) != rcOK {
		return nil
	}
	return b
}

// SetProgress restores progress kept with a state; nil means the state had
// none, and rcheevos starts its counting again from that point.
func (r *Runtime) SetProgress(b []byte) {
	if !r.ready {
		return
	}
	var ptr unsafe.Pointer
	if len(b) > 0 {
		ptr = unsafe.Pointer(&b[0])
	}
	call("lrc_progress_load", uintptr(ptr), uintptr(len(b)))
}

// List is the loaded game's set, for the menu.
func (r *Runtime) List() Summary {
	var s Summary
	if !r.ready {
		return s
	}
	title := make([]byte, 256)
	badge := make([]byte, 512)
	s.GameID = uint32(call("lrc_game", uintptr(unsafe.Pointer(&title[0])), uintptr(len(title)),
		uintptr(unsafe.Pointer(&badge[0])), uintptr(len(badge))))
	s.Title = cutNUL(title)
	s.Badge = mediaURL(cutNUL(badge))
	call("lrc_summary", uintptr(unsafe.Pointer(&s.Total)), uintptr(unsafe.Pointer(&s.Unlocked)),
		uintptr(unsafe.Pointer(&s.Points)), uintptr(unsafe.Pointer(&s.PointsUnlocked)))
	rp := make([]byte, 256)
	if call("lrc_rich_presence", uintptr(unsafe.Pointer(&rp[0])), uintptr(len(rp))) > 0 {
		s.RichPresence = cutNUL(rp)
	}
	r.listing = []Achievement{}
	call("lrc_list_achievements")
	s.Achievements, r.listing = r.listing, nil
	return s
}

func cutNUL(b []byte) string {
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

// Stop unloads the game. An unlock already on its way is given a few
// seconds to reach the server, which then finishes it whatever happens.
func (r *Runtime) Stop() {
	if !r.started {
		r.cancel()
		return
	}
	done := make(chan struct{})
	go func() { r.sending.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(stopWaitForSending):
	}
	r.deliver()
	call("lrc_unload_game")
	call("lrc_shutdown")
	r.cancel()
	r.started, r.ready = false, false
	r.release()
}

func (r *Runtime) release() {
	cFree(r.game)
	r.game = nil
	activeMu.Lock()
	if active == r {
		active = nil
	}
	activeMu.Unlock()
}

func current() *Runtime {
	activeMu.Lock()
	defer activeMu.Unlock()
	return active
}

// httpCB is rcheevos asking for a request to be sent. It returns at once;
// the answer is handed back on the core thread in Frame or Idle.
func httpCB(url, post, contentType, callback, data uintptr) uintptr {
	r := current()
	if r == nil {
		return 0
	}
	body := goString(post)
	agent := make([]byte, 128)
	call("lrc_user_agent_clause", uintptr(unsafe.Pointer(&agent[0])), uintptr(len(agent)))
	clause := cutNUL(agent)
	r.sending.Add(1)
	go func() {
		defer r.sending.Done()
		status, ans, err := r.send(r.ctx, body, clause)
		a := answer{callback: callback, data: data, status: status, body: ans}
		if err != nil {
			a.status, a.body = responseRetryableFail, nil
		}
		if len(a.body) > maxAnswer {
			a.status, a.body = responseClientError, nil
		}
		select {
		case r.answers <- a:
		case <-r.ctx.Done():
		}
	}()
	return 0
}

func eventCB(kind, id, title, desc, badge, points, extra uintptr) uintptr {
	r := current()
	if r == nil {
		return 0
	}
	e := Event{ID: uint32(id), Title: goString(title), Description: goString(desc),
		Badge: mediaURL(goString(badge)), Points: uint32(points)}
	switch uint32(kind) {
	case itemAchievement:
		state, progress, _ := strings.Cut(goString(extra), "|")
		if r.listing != nil {
			r.listing = append(r.listing, Achievement{ID: e.ID, Title: e.Title, Description: e.Description,
				Badge: e.Badge, Points: e.Points, State: state, Progress: progress})
		}
		return 0
	case evTriggered:
		e.Kind = "unlocked"
	case evProgressShow, evProgressUpdate:
		e.Kind, e.Progress = "progress", goString(extra)
	case evProgressHide:
		e = Event{Kind: "progress-hide"}
	case evGameCompleted:
		e = Event{Kind: "mastered"}
	case evDisconnected:
		e = Event{Kind: "offline"}
	case evReconnected:
		e = Event{Kind: "online"}
	case evServerError:
		r.log.Warn("rcheevos: RetroAchievements refused a request", "api", e.Title, "error", e.Description)
		e = Event{Kind: "problem", Text: e.Description}
	default:
		return 0
	}
	r.onEvent(e)
	return 0
}

func doneCB(op, result, message uintptr) uintptr {
	r := current()
	if r == nil {
		return 0
	}
	code := int32(uint32(result))
	msg := goString(message)
	switch op {
	case opLogin:
		if code != rcOK {
			r.off = true
			r.log.Info("rcheevos: not signed in", "code", code, "error", msg)
			r.onEvent(Event{Kind: "off", Text: msg})
		}
	case opLoadGame:
		switch {
		case code == rcOK:
			r.ready = true
			var total, unlocked, pts, ptsUnlocked uint32
			call("lrc_summary", uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&unlocked)),
				uintptr(unsafe.Pointer(&pts)), uintptr(unsafe.Pointer(&ptsUnlocked)))
			title := make([]byte, 256)
			call("lrc_game", uintptr(unsafe.Pointer(&title[0])), uintptr(len(title)), 0, 0)
			r.onEvent(Event{Kind: "ready", Title: cutNUL(title), Total: total, Unlocked: unlocked})
		case r.off:
			// Already said: the sign-in failed, and the load with it.
		default:
			r.log.Info("rcheevos: no achievements for this game", "code", code, "error", msg)
			r.onEvent(Event{Kind: "none", Text: msg})
		}
		cFree(r.game)
		r.game = nil
	}
	return 0
}

func logCB(message uintptr) uintptr {
	if r := current(); r != nil {
		r.log.Info("rcheevos", "message", goString(message))
	}
	return 0
}

// memoryCB answers rcheevos asking for one of the core's memory regions,
// during lrc_set_memory on the core thread.
func memoryCB(id, data, size uintptr) uintptr {
	r := current()
	if r == nil || r.core == nil {
		return 0
	}
	b := r.core.Memory(uint32(id))
	if len(b) == 0 {
		return 0
	}
	*(*uintptr)(unsafe.Add(unsafe.Pointer(nil), data)) = uintptr(unsafe.Pointer(&b[0]))
	*(*uintptr)(unsafe.Add(unsafe.Pointer(nil), size)) = uintptr(len(b))
	return 0
}
