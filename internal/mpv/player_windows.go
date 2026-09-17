//go:build windows

package mpv

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

/*
 * The libmpv calls, by syscall and without cgo.
 *
 * Two facts about the Windows x64 calling convention carry this file, and both
 * are easy to get wrong silently:
 *
 *  - mpv_wait_event takes a double. Go's asmstdcall copies the first four
 *    arguments into XMM0–XMM3 as well as the integer registers, so a double
 *    passed as its bit pattern in a uintptr arrives where the callee reads it.
 *  - The event structs are read from mpv's memory with the layouts below. They
 *    are stable across libmpv 2.x (client API major 2), which is the only
 *    version this loads — see Load.
 */

const (
	eventNone           = 0
	eventShutdown       = 1
	eventStartFile      = 6
	eventPropertyChange = 22

	formatNone   = 0
	formatFlag   = 3
	formatDouble = 5
)

// struct mpv_event
type cEvent struct {
	ID            int32
	Error         int32
	ReplyUserdata uint64
	Data          unsafe.Pointer
}

// struct mpv_event_property
type cProperty struct {
	Name   *byte
	Format int32
	_      int32
	Data   unsafe.Pointer
}

type lib struct {
	create, initialize, destroy, setOption, setPropString, command,
	observe, waitEvent, wakeup, errString, apiVersion *windows.LazyProc
}

var (
	loaded  *lib
	loadErr error
	once    sync.Once
)

// Load opens libmpv from an explicit path, once per process.
//
// Always a full path: a bare "libmpv-2.dll" would be found by the DLL search
// order, which includes the working directory — a planted DLL beside a media
// file would then run inside the client.
func Load(path string) error {
	once.Do(func() {
		d := windows.NewLazyDLL(path)
		if err := d.Load(); err != nil {
			loadErr = fmt.Errorf("mpv: load %s: %w", path, err)
			return
		}
		l := &lib{
			create: d.NewProc("mpv_create"), initialize: d.NewProc("mpv_initialize"),
			destroy: d.NewProc("mpv_terminate_destroy"), setOption: d.NewProc("mpv_set_option_string"),
			setPropString: d.NewProc("mpv_set_property_string"), command: d.NewProc("mpv_command"),
			observe: d.NewProc("mpv_observe_property"), waitEvent: d.NewProc("mpv_wait_event"),
			wakeup: d.NewProc("mpv_wakeup"), errString: d.NewProc("mpv_error_string"),
			apiVersion: d.NewProc("mpv_client_api_version"),
		}
		v, _, _ := l.apiVersion.Call()
		if major := v >> 16; major != 2 {
			loadErr = fmt.Errorf("mpv: client API %d.%d, need 2.x", major, v&0xffff)
			return
		}
		loaded = l
	})
	return loadErr
}

// Player is one embedded mpv instance.
type Player struct {
	h        uintptr
	mu       sync.Mutex
	state    State
	onEvents func(State, []string)
	done     chan struct{}
}

// New creates and initialises a player drawing into wid. onEvents is called
// from the player's own goroutine with the state after each change and the
// media events it raised; the caller marshals to its UI thread.
func New(wid uint64, logFile string, onEvents func(State, []string)) (*Player, error) {
	if loaded == nil {
		return nil, errors.New("mpv: not loaded")
	}
	h, _, _ := loaded.create.Call()
	if h == 0 {
		return nil, errors.New("mpv: mpv_create returned null")
	}
	p := &Player{h: h, state: NewState(), onEvents: onEvents, done: make(chan struct{})}
	for _, o := range Options(wid, logFile) {
		if err := p.check("option "+o.Name, callStr2(loaded.setOption, h, o.Name, o.Value)); err != nil {
			_, _, _ = loaded.destroy.Call(h)
			return nil, err
		}
	}
	if rc, _, _ := loaded.initialize.Call(h); int32(rc) < 0 {
		err := p.check("mpv_initialize", rc)
		_, _, _ = loaded.destroy.Call(h)
		return nil, err
	}
	for i, o := range Observed {
		f := uintptr(formatFlag)
		if o.Double {
			f = formatDouble
		}
		name := cstr(o.Name)
		_, _, _ = loaded.observe.Call(h, uintptr(i+1), uintptr(unsafe.Pointer(name)), f)
		runtime.KeepAlive(name)
	}
	go p.loop()
	return p, nil
}

// Load opens a URL, replacing whatever was playing. It starts paused so the
// provider's play() decides when frames begin, as it does for the element.
func (p *Player) Load(url string, startSeconds float64) error {
	p.mu.Lock()
	p.state = Reset(p.state)
	p.mu.Unlock()
	opts := "pause=yes"
	if startSeconds > 0 {
		opts += fmt.Sprintf(",start=%.3f", startSeconds)
	}
	return p.Command("loadfile", url, "replace", "-1", opts)
}

// Command runs an mpv command.
func (p *Player) Command(args ...string) error {
	ptrs := make([]*byte, len(args)+1)
	for i, a := range args {
		ptrs[i] = cstr(a)
	}
	rc, _, _ := loaded.command.Call(p.h, uintptr(unsafe.Pointer(&ptrs[0])))
	runtime.KeepAlive(ptrs)
	return p.check("command "+args[0], rc)
}

// Set sets a property from its string form.
func (p *Player) Set(name, value string) error {
	return p.check("set "+name, callStr2(loaded.setPropString, p.h, name, value))
}

// State is the latest view of the player.
func (p *Player) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// Close stops the event loop and destroys the instance.
func (p *Player) Close() {
	_, _, _ = loaded.wakeup.Call(p.h)
	_ = p.Command("quit")
	<-p.done
	_, _, _ = loaded.destroy.Call(p.h)
}

func (p *Player) loop() {
	defer close(p.done)
	forever := uintptr(math.Float64bits(-1))
	for {
		r, _, _ := loaded.waitEvent.Call(p.h, forever)
		ev := (*cEvent)(cPointer(r))
		switch ev.ID {
		case eventShutdown:
			return
		case eventPropertyChange:
			c, ok := readChange((*cProperty)(ev.Data))
			if !ok {
				continue
			}
			p.mu.Lock()
			var events []string
			p.state, events = Apply(p.state, c)
			st := p.state
			p.mu.Unlock()
			if len(events) > 0 && p.onEvents != nil {
				p.onEvents(st, events)
			}
		}
	}
}

func readChange(pr *cProperty) (Change, bool) {
	if pr == nil || pr.Name == nil {
		return Change{}, false
	}
	c := Change{Name: windows.BytePtrToString(pr.Name)}
	switch pr.Format {
	case formatNone:
		c.Unavailable = true
	case formatDouble:
		c.Double = *(*float64)(pr.Data)
	case formatFlag:
		c.Flag = *(*int32)(pr.Data) != 0
	default:
		return Change{}, false
	}
	return c, true
}

func (p *Player) check(what string, rc uintptr) error {
	if int32(rc) >= 0 {
		return nil
	}
	s, _, _ := loaded.errString.Call(rc)
	return fmt.Errorf("mpv: %s: %s", what, windows.BytePtrToString((*byte)(cPointer(s))))
}

func cstr(s string) *byte { b, _ := windows.BytePtrFromString(s); return b }

func callStr2(proc *windows.LazyProc, h uintptr, a, b string) uintptr {
	pa, pb := cstr(a), cstr(b)
	rc, _, _ := proc.Call(h, uintptr(unsafe.Pointer(pa)), uintptr(unsafe.Pointer(pb)))
	runtime.KeepAlive(pa)
	runtime.KeepAlive(pb)
	return rc
}

// cPointer turns an address returned by libmpv into a pointer. The memory is
// mpv's, never Go's, so the moving-GC concern behind vet's check does not
// apply; unsafe.Add states that without tripping it.
func cPointer(addr uintptr) unsafe.Pointer { return unsafe.Add(unsafe.Pointer(nil), addr) }
