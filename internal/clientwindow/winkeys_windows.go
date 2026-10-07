package clientwindow

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"lancast/internal/webview2"

	"golang.org/x/sys/windows"
)

/*
 * The keyboard the window answers to without having focus of its own: Win+arrow
 * while the page sits in the video overlay or the window is fullscreen
 * (winkeys.go), and the media keys while something is playing.
 *
 * One low-level keyboard hook carries both, installed on the window's own
 * thread, which is the thread whose message loop Windows calls it from. It
 * decides fast and does the work later, through Dispatch: a low-level hook
 * that takes too long is removed by Windows without a word.
 *
 * Both halves are narrow on purpose. Win+arrow is taken only in the two states
 * Windows cannot handle, and left alone everywhere else. A media key is taken
 * only while the page says something is loaded in the player — paused counts,
 * since Play is what resumes it — and otherwise passes to whatever else on the
 * machine wants it, a music app or the browser.
 *
 * Why a hook rather than RegisterHotKey for the media keys: a registered hotkey
 * belongs to the process for as long as it is registered, and registering and
 * unregistering as playback starts and stops is a race with every other
 * application doing the same. The hook decides per press.
 */

var (
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetAsyncKeyState    = user32.NewProc("GetAsyncKeyState")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procGetWindow           = user32.NewProc("GetWindow")
	procSendInput           = user32.NewProc("SendInput")
	procMonitorFromRect     = user32.NewProc("MonitorFromRect")
)

const (
	whKeyboardLL  = 13
	wmKeyDown     = 0x0100
	wmKeyUp       = 0x0101
	wmSysKeyDown  = 0x0104
	wmSysKeyUp    = 0x0105
	llkhfInjected = 0x10
	gwOwner       = 4

	vkShift  = 0x10
	vkLWin   = 0x5B
	vkRWin   = 0x5C
	vkLeft   = 0x25
	vkUp     = 0x26
	vkRight  = 0x27
	vkDown   = 0x28
	vkDummy  = 0xE8 // unassigned: pressed to stop a lone Win release opening Start
	swMin    = 6
	inputKey = 1
	keyUp    = 0x0002
)

type kbdLLHook struct {
	VkCode, ScanCode, Flags, Time uint32
	ExtraInfo                     uintptr
}

type keyInput struct {
	Type      uint32
	_         uint32
	Vk, Scan  uint16
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
	_         [8]byte // INPUT is sized by its largest member, MOUSEINPUT
}

// keyHook is the one hook a process installs. A low-level hook is global to
// the desktop and called back on a plain function, so its state is too.
type keyHook struct {
	handle uintptr
	w      webview2.WebView
	fs     *fullscreener
	// media is whether the page has something in the player.
	media atomic.Bool
}

var (
	hookMu   sync.Mutex
	theHook  *keyHook
	hookProc = windows.NewCallback(lowLevelKey)
)

// installKeyHook installs the hook for this window. Call it on the window's
// thread; remove it before the window goes.
func installKeyHook(w webview2.WebView, fs *fullscreener) *keyHook {
	h := &keyHook{w: w, fs: fs}
	hookMu.Lock()
	theHook = h
	hookMu.Unlock()
	h.handle, _, _ = procSetWindowsHookExW.Call(whKeyboardLL, hookProc, 0, 0)
	return h
}

func (h *keyHook) remove() {
	if h == nil {
		return
	}
	if h.handle != 0 {
		_, _, _ = procUnhookWindowsHookEx.Call(h.handle)
	}
	hookMu.Lock()
	if theHook == h {
		theHook = nil
	}
	hookMu.Unlock()
}

// SetMediaActive is what the page reports: something is loaded in the player.
func (h *keyHook) SetMediaActive(on bool) { h.media.Store(on) }

// lowLevelKey is the hook. Its third argument is a KBDLLHOOKSTRUCT, taken as
// a pointer outright: the callback trampoline passes it through unchanged, and
// a uintptr turned back into a pointer is what vet rightly distrusts.
func lowLevelKey(code int, wp uintptr, k *kbdLLHook) uintptr {
	if code >= 0 && k != nil {
		hookMu.Lock()
		h := theHook
		hookMu.Unlock()
		if h != nil && h.handle != 0 && k.Flags&llkhfInjected == 0 && h.take(uint32(wp), k.VkCode) {
			return 1
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wp, uintptr(unsafe.Pointer(k)))
	return r
}

// take decides whether this window answers a key, starts the answer, and
// reports whether the key should go no further.
func (h *keyHook) take(msg, vk uint32) bool {
	down := msg == wmKeyDown || msg == wmSysKeyDown
	up := msg == wmKeyUp || msg == wmSysKeyUp

	if cmd := MediaCommand(vk); cmd != "" {
		if !h.media.Load() {
			return false
		}
		if down {
			h.w.Dispatch(func() {
				h.w.Eval(`window.__lancastMediaKey && window.__lancastMediaKey("` + cmd + `")`)
			})
		}
		// The release goes no further either: a key half-delivered to another
		// application is one it may act on.
		return true
	}

	key := arrowKey(vk)
	if key == 0 || !(down || up) || !keyHeld(vkLWin) && !keyHeld(vkRWin) {
		return false
	}
	if !h.answersWinKeys() {
		return false
	}
	if down {
		shift := keyHeld(vkShift)
		h.w.Dispatch(func() { h.applyWinKey(key, shift) })
		// Windows saw the Win key go down and will see it come up. With
		// nothing between, it opens Start; a key it ignores stops that.
		tapDummy()
	}
	return true
}

// answersWinKeys is true in the two states Windows cannot snap: the page in the
// video overlay (the foreground window is then a popup this window owns), and
// fullscreen (no frame).
func (h *keyHook) answersWinKeys() bool {
	main := uintptr(h.w.Window())
	fg, _, _ := procGetForegroundWindow.Call()
	if fg == 0 {
		return false
	}
	if fg == main {
		return h.fs.IsOn()
	}
	owner, _, _ := procGetWindow.Call(fg, gwOwner)
	return owner == main
}

func (h *keyHook) applyWinKey(key WinKey, shift bool) {
	main := uintptr(h.w.Window())
	win, maximized, normal, ok := WindowState(main)
	if !ok {
		return
	}
	m := PlanWinKey(key, shift, WinState{
		Window: win, Normal: normal, Maximized: maximized,
		Fullscreen: h.fs.IsOn(), Monitors: Monitors(),
	})
	switch m.Kind {
	case MoveMaximize:
		_, _, _ = procShowWindow.Call(main, swMaximize)
	case MoveRestore:
		_, _, _ = procShowWindow.Call(main, swRestore)
	case MoveMinimize:
		_, _, _ = procShowWindow.Call(main, swMin)
	case MovePlace:
		PlaceWindow(main, m.Rect.Left, m.Rect.Top, m.Rect.Width(), m.Rect.Height(), maximized, m.Maximize)
	case MoveFullscreenTo:
		h.fs.MoveTo(main, m.Monitor)
	}
}

func arrowKey(vk uint32) WinKey {
	switch vk {
	case vkUp:
		return WinUp
	case vkDown:
		return WinDown
	case vkLeft:
		return WinLeft
	case vkRight:
		return WinRight
	}
	return 0
}

func keyHeld(vk uintptr) bool {
	s, _, _ := procGetAsyncKeyState.Call(vk)
	return s&0x8000 != 0
}

func tapDummy() {
	in := [2]keyInput{
		{Type: inputKey, Vk: vkDummy},
		{Type: inputKey, Vk: vkDummy, Flags: keyUp},
	}
	_, _, _ = procSendInput.Call(2, uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
}

// IsOn reports whether the window is fullscreen.
func (f *fullscreener) IsOn() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.on
}

/*
 * MoveTo carries a fullscreen window onto another screen, still fullscreen.
 *
 * The rectangle it will be restored to goes with it, so leaving fullscreen
 * afterwards lands on the screen the film was moved to rather than jumping
 * back to the one it started on.
 */
func (f *fullscreener) MoveTo(hwnd uintptr, to Monitor) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.on {
		return
	}
	const monitorDefaultToNearest = 2
	r := rect{Left: int32(to.Work.Left), Top: int32(to.Work.Top), Right: int32(to.Work.Right), Bottom: int32(to.Work.Bottom)}
	mon, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), monitorDefaultToNearest)
	var mi monitorInfo
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if mon == 0 {
		return
	}
	if ok, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		return
	}
	if from, ok := monitorHolding(toRect(f.placement.NormalPosition), screens(Monitors())); ok {
		n := carry(toRect(f.placement.NormalPosition), from.Work, to.Work)
		f.placement.NormalPosition = rect{Left: int32(n.Left), Top: int32(n.Top), Right: int32(n.Right), Bottom: int32(n.Bottom)}
	}
	_, _, _ = procSetWindowPos.Call(hwnd, 0,
		uintptr(mi.RcMonitor.Left), uintptr(mi.RcMonitor.Top),
		uintptr(mi.RcMonitor.Right-mi.RcMonitor.Left),
		uintptr(mi.RcMonitor.Bottom-mi.RcMonitor.Top),
		swpNoOwnerZOrder|swpFrameChanged)
}
