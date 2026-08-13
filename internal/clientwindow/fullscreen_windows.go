//go:build windows

package clientwindow

import (
	"sync"
	"unsafe"
)

/*
 * Real fullscreen for the desktop window.
 *
 * The page's fullscreen button used to call requestFullscreen() and nothing
 * visible happened: WebView2 tells its host that a page wants fullscreen and
 * the host is what has to do something about it — resize, drop the frame, cover
 * the taskbar. Nothing here listened, so the page believed it was fullscreen
 * inside a window that had not changed size, which is indistinguishable from a
 * button that does not work.
 *
 * Handling it in the host rather than through the WebView2 fullscreen event is
 * the smaller of two jobs: the event needs a COM interface and an event-handler
 * vtable written by hand, while the window it would ask us to resize is a plain
 * HWND we already own. So the page calls a binding, and this does the ordinary
 * Win32 borderless-fullscreen dance.
 *
 * Monitor-aware on purpose. The window is fullscreened to the monitor it is
 * *on*, not to the primary one — dragging the player to a second screen and
 * pressing fullscreen is exactly how this was reported.
 */

var (
	// user32 is declared in clientwindow_windows.go, which this file extends.
	procGetWindowPlacement = user32.NewProc("GetWindowPlacement")
	procSetWindowPlacement = user32.NewProc("SetWindowPlacement")
	procMonitorFromWindow  = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW    = user32.NewProc("GetMonitorInfoW")
	procGetWindowLongPtrW  = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW  = user32.NewProc("SetWindowLongPtrW")
	procSetWindowPos       = user32.NewProc("SetWindowPos")
)

// gwlStyle is GWL_STYLE (-16) as the uintptr the syscall takes. Written as a
// bit pattern because a negative constant does not convert: the call wants the
// two's-complement value, not the number.
var gwlStyle = ^uintptr(0) - 15

const (
	wsOverlappedWindow    = 0x00CF0000
	swpNoOwnerZOrder      = 0x0200
	swpFrameChanged       = 0x0020
	monitorDefaultNearest = 0x00000002
)

type rect struct{ Left, Top, Right, Bottom int32 }

type windowPlacement struct {
	Length         uint32
	Flags          uint32
	ShowCmd        uint32
	PtMinPositionX int32
	PtMinPositionY int32
	PtMaxPositionX int32
	PtMaxPositionY int32
	NormalPosition rect
}

type monitorInfo struct {
	CbSize    uint32
	RcMonitor rect
	RcWork    rect
	DwFlags   uint32
}

// fullscreener remembers what a window looked like before it was fullscreened,
// so leaving puts it back exactly — including which monitor it was on and
// whether it was maximized.
type fullscreener struct {
	mu        sync.Mutex
	on        bool
	style     uintptr
	placement windowPlacement
}

// Toggle switches the window between fullscreen and what it was, and reports
// which state it ended in.
func (f *fullscreener) Toggle(hwnd uintptr) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.on {
		f.restore(hwnd)
		f.on = false
		return false
	}
	f.enter(hwnd)
	f.on = true
	return true
}

func (f *fullscreener) enter(hwnd uintptr) {
	f.placement.Length = uint32(unsafe.Sizeof(f.placement))
	_, _, _ = procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&f.placement)))
	f.style, _, _ = procGetWindowLongPtrW.Call(hwnd, gwlStyle)

	// The monitor the window is on, not the primary one.
	mon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultNearest)
	var mi monitorInfo
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if mon == 0 {
		return
	}
	if ok, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		return
	}

	_, _, _ = procSetWindowLongPtrW.Call(hwnd, gwlStyle, f.style&^wsOverlappedWindow)
	// RcMonitor rather than RcWork: the whole screen, taskbar included, which is
	// what fullscreen means to somebody watching a film.
	_, _, _ = procSetWindowPos.Call(hwnd, 0,
		uintptr(mi.RcMonitor.Left), uintptr(mi.RcMonitor.Top),
		uintptr(mi.RcMonitor.Right-mi.RcMonitor.Left),
		uintptr(mi.RcMonitor.Bottom-mi.RcMonitor.Top),
		swpNoOwnerZOrder|swpFrameChanged)
}

func (f *fullscreener) restore(hwnd uintptr) {
	if f.style != 0 {
		_, _, _ = procSetWindowLongPtrW.Call(hwnd, gwlStyle, f.style)
	}
	if f.placement.Length != 0 {
		_, _, _ = procSetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&f.placement)))
	}
	_, _, _ = procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0,
		swpNoOwnerZOrder|swpFrameChanged|0x0001|0x0002) // SWP_NOSIZE|SWP_NOMOVE
}
