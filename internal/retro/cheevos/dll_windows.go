//go:build windows

package cheevos

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"lancast/internal/retro/libretro"
)

/*
 * Callbacks from C carry no context, and the shim keeps one client, so the
 * Go side keeps one too: active is the Client the callbacks reach. Each
 * callback is made once per process (syscall.NewCallback has a fixed budget
 * of about 2,000 for the life of the process), however many times a client
 * is opened and closed.
 *
 * Anything rcheevos may keep after a call returns — the game's bytes and
 * path — goes on the process heap, not in Go memory, for the reason
 * internal/retro/libretro gives: a pointer held in C is one the collector
 * knows nothing about.
 */

var (
	kernel32      = syscall.NewLazyDLL("kernel32.dll")
	procGetHeap   = kernel32.NewProc("GetProcessHeap")
	procHeapAlloc = kernel32.NewProc("HeapAlloc")
	procHeapFree  = kernel32.NewProc("HeapFree")

	active atomic.Pointer[Client]

	callbacks = sync.OnceValue(func() [4]uintptr {
		return [4]uintptr{
			syscall.NewCallback(requestCB),
			syscall.NewCallback(eventCB),
			syscall.NewCallback(logCB),
			syscall.NewCallback(doneCB),
		}
	})
)

var shimFuncs = []string{
	"lc_interface_version", "lc_init", "lc_shutdown", "lc_user_agent_clause",
	"lc_server_response", "lc_login_with_token", "lc_set_memory", "lc_load_game",
	"lc_unload_game", "lc_do_frame", "lc_idle", "lc_reset", "lc_progress_size",
	"lc_serialize_progress", "lc_deserialize_progress", "lc_hash",
	"lc_memory_regions", "lc_read_memory",
	// Interface version 2: the game menu's Achievements page.
	"lc_game", "lc_list_achievements", "lc_achievement_at", "lc_free_achievements",
	"lc_rich_presence",
	// Interface version 3: hardcore (ADR 0112 step 5).
	"lc_set_hardcore", "lc_hardcore",
}

// Client is the one rcheevos client in this process.
type Client struct {
	procs map[string]*syscall.Proc
	h     Handler

	mu       sync.Mutex // guards the game's C copies and closed
	gameData unsafe.Pointer
	gamePath unsafe.Pointer
	closed   bool
}

// Open loads lancast-cheevos.dll by full path and creates the client,
// casual: hardcore stays off until RetroAchievements approves LANcast
// (ADR 0112). Only one Client may be open at a time.
func Open(path string, h Handler) (*Client, error) {
	if h == nil {
		return nil, errors.New("cheevos: no handler")
	}
	dll, err := syscall.LoadDLL(path)
	if err != nil {
		return nil, fmt.Errorf("cheevos: load %s: %w", path, err)
	}
	c := &Client{procs: map[string]*syscall.Proc{}, h: h}
	for _, name := range shimFuncs {
		p, err := dll.FindProc(name)
		if err != nil {
			return nil, fmt.Errorf("cheevos: %s is not LANcast's rcheevos shim: %w", path, err)
		}
		c.procs[name] = p
	}
	if v, _, _ := c.procs["lc_interface_version"].Call(); uint32(v) != InterfaceVersion {
		return nil, fmt.Errorf("cheevos: %s speaks shim version %d, not %d", path, uint32(v), InterfaceVersion)
	}
	if !active.CompareAndSwap(nil, c) {
		return nil, errors.New("cheevos: a client is already open")
	}
	cb := callbacks()
	if r, _, _ := c.procs["lc_init"].Call(cb[0], cb[1], cb[2], cb[3]); int32(r) != 0 {
		active.Store(nil)
		return nil, errors.New("cheevos: the client could not be created")
	}
	return c, nil
}

// Close destroys the client. A Respond for a request still in flight is
// dropped. The DLL stays loaded, as libmpv does: unloading it would gain
// nothing and would leave the callbacks pointing at nothing should it load
// at another address next time.
func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	_, _, _ = c.procs["lc_shutdown"].Call()
	active.CompareAndSwap(c, nil)
	c.mu.Lock()
	c.freeGame()
	c.mu.Unlock()
}

// UserAgentClause is rcheevos' part of the HTTP user agent, "rcheevos/12.5".
// RetroAchievements asks that a client's user agent carry it.
func (c *Client) UserAgentClause() string {
	var buf [64]byte
	n, _, _ := c.procs["lc_user_agent_clause"].Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || n > uintptr(len(buf)) {
		return ""
	}
	return goString(&buf[0])
}

// Respond answers request id, from any goroutine, once: with the HTTP status
// and body, or with StatusRetry or StatusFail when there was no answer.
func (c *Client) Respond(id uint32, body []byte, status int) {
	var p unsafe.Pointer
	if len(body) > 0 {
		p = unsafe.Pointer(&body[0])
	}
	_, _, _ = c.procs["lc_server_response"].Call(uintptr(id), uintptr(p), uintptr(len(body)), uintptr(int32(status)))
}

// Login signs in with the user's RetroAchievements token. Done(OpLogin, …)
// follows, once the request it makes has been answered.
func (c *Client) Login(username, token string) error {
	u, err := syscall.BytePtrFromString(username)
	if err != nil {
		return err
	}
	t, err := syscall.BytePtrFromString(token)
	if err != nil {
		return err
	}
	_, _, _ = c.procs["lc_login_with_token"].Call(uintptr(unsafe.Pointer(u)), uintptr(unsafe.Pointer(t)))
	return nil
}

// retroMemoryDescriptor and retroMemoryMap mirror libretro's structs on
// 64-bit Windows, as rcheevos reads them.
type retroMemoryDescriptor struct {
	flags                                  uint64
	ptr                                    uintptr
	offset, start, sel, disconnect, length uintptr
	addrspace                              *byte
}

type retroMemoryMap struct {
	descriptors *retroMemoryDescriptor
	num         uint32
}

// SetMemory registers the game's memory, before LoadGame: the core's memory
// map, if it sent one, and its system and save RAM blocks for consoles it
// describes without one. Every region must be memory the core owns, valid
// while the game is loaded — rcheevos keeps the pointers, not copies. It
// reports whether any memory was found at all; a game with none can load,
// but no achievement will ever see a byte change.
func (c *Client) SetMemory(console Console, mmap []libretro.MemoryDescriptor, system, save []byte) bool {
	var mp unsafe.Pointer
	var held []unsafe.Pointer
	defer func() {
		for _, p := range held {
			cFree(p)
		}
	}()
	if len(mmap) > 0 {
		size := unsafe.Sizeof(retroMemoryDescriptor{})
		arr := cAlloc(size * uintptr(len(mmap)))
		held = append(held, arr)
		descs := unsafe.Slice((*retroMemoryDescriptor)(arr), len(mmap))
		for i, d := range mmap {
			descs[i] = retroMemoryDescriptor{
				flags: d.Flags, ptr: d.Ptr, offset: uintptr(d.Offset), start: uintptr(d.Start),
				sel: uintptr(d.Select), disconnect: uintptr(d.Disconnect), length: uintptr(d.Len),
			}
			if d.AddrSpace != "" {
				s := cString(d.AddrSpace)
				held = append(held, s)
				descs[i].addrspace = (*byte)(s)
			}
		}
		m := (*retroMemoryMap)(cAlloc(unsafe.Sizeof(retroMemoryMap{})))
		held = append(held, unsafe.Pointer(m))
		m.descriptors = &descs[0]
		m.num = uint32(len(mmap))
		mp = unsafe.Pointer(m)
	}
	r, _, _ := c.procs["lc_set_memory"].Call(uintptr(console), uintptr(mp),
		bytesPtr(system), uintptr(len(system)), bytesPtr(save), uintptr(len(save)))
	return int32(r) != 0
}

// LoadGame identifies the game by its RetroAchievements hash and loads its
// achievements. Pass the bytes, or a path and nil for a game the core reads
// from disk itself (a disc image). Done(OpLoadGame, …) follows.
func (c *Client) LoadGame(console Console, path string, data []byte) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.freeGame()
	if path != "" {
		c.gamePath = cString(path)
	}
	if len(data) > 0 {
		c.gameData = cAlloc(uintptr(len(data)))
		copy(unsafe.Slice((*byte)(c.gameData), len(data)), data)
	}
	gp, gd := c.gamePath, c.gameData
	c.mu.Unlock()
	_, _, _ = c.procs["lc_load_game"].Call(uintptr(console), uintptr(gp), uintptr(gd), uintptr(len(data)))
}

// UnloadGame ends the game. Unlocks still queued are kept by rcheevos and
// sent when it can.
func (c *Client) UnloadGame() {
	_, _, _ = c.procs["lc_unload_game"].Call()
	c.mu.Lock()
	c.freeGame()
	c.mu.Unlock()
}

// DoFrame evaluates achievements, once per emulated frame, on the core's
// thread: it reads the game's memory.
func (c *Client) DoFrame() { _, _, _ = c.procs["lc_do_frame"].Call() }

// Idle keeps the session alive while paused; at least once a second.
func (c *Client) Idle() { _, _, _ = c.procs["lc_idle"].Call() }

// Reset tells rcheevos the game was reset, so progress starts over.
func (c *Client) Reset() { _, _, _ = c.procs["lc_reset"].Call() }

// Progress is achievement progress, to keep inside a save state (ADR 0112
// section 7). Empty when there is none.
func (c *Client) Progress() ([]byte, error) {
	n, _, _ := c.procs["lc_progress_size"].Call()
	if n == 0 {
		return nil, nil
	}
	buf := make([]byte, n)
	if r, _, _ := c.procs["lc_serialize_progress"].Call(uintptr(unsafe.Pointer(&buf[0])), n); int32(r) != OK {
		return nil, fmt.Errorf("cheevos: progress not saved: %d", int32(r))
	}
	return buf, nil
}

// RestoreProgress restores progress from a save state; nil for a state saved
// without any, which resets progress rather than leaving the current one.
func (c *Client) RestoreProgress(b []byte) error {
	if r, _, _ := c.procs["lc_deserialize_progress"].Call(bytesPtr(b), uintptr(len(b))); int32(r) != OK {
		return fmt.Errorf("cheevos: progress not restored: %d", int32(r))
	}
	return nil
}

// Hash is RetroAchievements' hash of a game: from its bytes, or from a path
// with data nil. Each console has its own rule (an NES dump loses its
// header first, for one), which is why this asks rcheevos rather than
// computing an MD5 here.
func (c *Client) Hash(console Console, path string, data []byte) (string, bool) {
	var out [33]byte
	var pp unsafe.Pointer
	if path != "" && len(data) == 0 {
		pp = cString(path)
		defer cFree(pp)
	}
	r, _, _ := c.procs["lc_hash"].Call(uintptr(console), uintptr(pp), bytesPtr(data), uintptr(len(data)),
		uintptr(unsafe.Pointer(&out[0])))
	if int32(r) == 0 {
		return "", false
	}
	return goString(&out[0]), true
}

// MemoryRegions is how many regions SetMemory found.
func (c *Client) MemoryRegions() int {
	n, _, _ := c.procs["lc_memory_regions"].Call()
	return int(uint32(n))
}

// ReadMemory reads n bytes at a RetroAchievements address, through the
// regions SetMemory registered: the read rcheevos itself makes. It returns
// what it could read, short where an address is not backed by memory.
func (c *Client) ReadMemory(address uint32, n int) []byte {
	if n <= 0 {
		return nil
	}
	buf := make([]byte, n)
	got, _, _ := c.procs["lc_read_memory"].Call(uintptr(address), uintptr(unsafe.Pointer(&buf[0])), uintptr(n))
	return buf[:uint32(got)]
}

// lcSummary mirrors lc_summary_t.
type lcSummary struct{ total, unlocked, points, pointsUnlocked uint32 }

// Game is the loaded game and its counts; false when none is loaded.
func (c *Client) Game() (GameInfo, bool) {
	var title [256]byte
	var badge [64]byte
	var s lcSummary
	id, _, _ := c.procs["lc_game"].Call(uintptr(unsafe.Pointer(&title[0])), uintptr(len(title)),
		uintptr(unsafe.Pointer(&badge[0])), uintptr(len(badge)), uintptr(unsafe.Pointer(&s)))
	if uint32(id) == 0 {
		return GameInfo{}, false
	}
	return GameInfo{
		ID: uint32(id), Title: goString(&title[0]), Badge: goString(&badge[0]),
		Total: s.total, Unlocked: s.unlocked, Points: s.points, PointsUnlocked: s.pointsUnlocked,
	}, true
}

// lcAchievement mirrors lc_achievement_t.
type lcAchievement struct {
	id, points, state                   uint32
	percent                             float32
	title, description, badge, progress *byte
}

// Achievements is the loaded game's core achievements, unlocked first.
func (c *Client) Achievements() []Listed {
	n, _, _ := c.procs["lc_list_achievements"].Call()
	defer c.procs["lc_free_achievements"].Call()
	out := make([]Listed, 0, uint32(n))
	for i := uint32(0); i < uint32(n); i++ {
		var a lcAchievement
		if ok, _, _ := c.procs["lc_achievement_at"].Call(uintptr(i), uintptr(unsafe.Pointer(&a))); int32(ok) == 0 {
			break
		}
		out = append(out, Listed{
			ID: a.id, Points: a.points, State: ListedState(a.state), Percent: a.percent,
			Title: goString(a.title), Description: goString(a.description),
			Badge: goString(a.badge), Progress: goString(a.progress),
		})
	}
	return out
}

// SetHardcore turns hardcore on or off. On, with a game loaded, rcheevos
// raises EventReset: the game has to be reset before it counts.
func (c *Client) SetHardcore(on bool) {
	var v uintptr
	if on {
		v = 1
	}
	_, _, _ = c.procs["lc_set_hardcore"].Call(v)
}

// Hardcore is whether rcheevos has hardcore on.
func (c *Client) Hardcore() bool {
	r, _, _ := c.procs["lc_hardcore"].Call()
	return int32(r) != 0
}

// RichPresence is the game's rich presence line, "" when it has none.
func (c *Client) RichPresence() string {
	var buf [256]byte
	if n, _, _ := c.procs["lc_rich_presence"].Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); n == 0 {
		return ""
	}
	return goString(&buf[0])
}

func (c *Client) freeGame() {
	cFree(c.gameData)
	cFree(c.gamePath)
	c.gameData, c.gamePath = nil, nil
}

// lcEvent mirrors lc_event_t.
type lcEvent struct {
	typ, id, points           uint32
	percent                   float32
	title, description, badge *byte
	text                      *byte
}

func requestCB(id, url, post, contentType uintptr) uintptr {
	if c := active.Load(); c != nil {
		c.h.Request(Request{
			ID:          uint32(id),
			URL:         cStr(url),
			PostData:    cStr(post),
			ContentType: cStr(contentType),
		})
	}
	return 0
}

func eventCB(ev uintptr) uintptr {
	c := active.Load()
	if c == nil || ev == 0 {
		return 0
	}
	e := (*lcEvent)(cPointer(ev))
	c.h.Event(Event{
		Type:        EventType(e.typ),
		ID:          e.id,
		Points:      e.points,
		Percent:     e.percent,
		Title:       goString(e.title),
		Description: goString(e.description),
		Badge:       goString(e.badge),
		Text:        goString(e.text),
	})
	return 0
}

func logCB(msg uintptr) uintptr {
	if c := active.Load(); c != nil {
		c.h.Log(cStr(msg))
	}
	return 0
}

func doneCB(op, result, msg uintptr) uintptr {
	if c := active.Load(); c != nil {
		c.h.Done(Op(uint32(op)), int(int32(result)), cStr(msg))
	}
	return 0
}

// cPointer turns an address from C back into a pointer; see the same helper
// in internal/retro/libretro for why it is written this way.
func cPointer(addr uintptr) unsafe.Pointer { return unsafe.Add(unsafe.Pointer(nil), addr) }

func bytesPtr(b []byte) uintptr {
	if len(b) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&b[0]))
}

// cStr copies the C string at addr, "" for NULL. The address is tested
// before it becomes a pointer: the compiler takes unsafe.Add's result to be
// non-nil, so a nil check after the conversion is optimised away. That
// crashed the first run, on the NULL error of a login that succeeded.
func cStr(addr uintptr) string {
	if addr == 0 {
		return ""
	}
	return goString((*byte)(cPointer(addr)))
}

// goString copies a NUL-terminated C string, which may be nil.
func goString(p *byte) string {
	if p == nil {
		return ""
	}
	var n int
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
		if n > 1<<20 {
			break
		}
	}
	return string(unsafe.Slice(p, n))
}

const heapZeroMemory = 0x8

func cAlloc(n uintptr) unsafe.Pointer {
	if n == 0 {
		n = 1
	}
	h, _, _ := procGetHeap.Call()
	p, _, _ := procHeapAlloc.Call(h, heapZeroMemory, n)
	if p == 0 {
		panic("cheevos: out of memory")
	}
	return cPointer(p)
}

func cFree(p unsafe.Pointer) {
	if p == nil {
		return
	}
	h, _, _ := procGetHeap.Call()
	_, _, _ = procHeapFree.Call(h, 0, uintptr(p))
}

func cString(s string) unsafe.Pointer {
	p := cAlloc(uintptr(len(s) + 1))
	copy(unsafe.Slice((*byte)(p), len(s)), s)
	return p
}
