//go:build windows

package clientwindow

import (
	"sync"
	"syscall"
	"unsafe"
)

/*
 * The Win32 half of remembering which screen the window was on.
 *
 * Deliberately thin. Everything decidable lives in placement.go, which is why
 * the rules about unplugged monitors and windows larger than their new screen
 * are tested with no display attached — this file only fetches facts and moves
 * a window.
 */

var (
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procIsZoomed            = user32.NewProc("IsZoomed")
)

// monitorInfoEx is monitorInfo plus the device name.
//
// The name is the whole reason this exists rather than reusing monitorInfo:
// coordinates are not identity, and a monitor has to be recognisable after the
// desk has been rearranged.
type monitorInfoEx struct {
	CbSize    uint32
	RcMonitor rect
	RcWork    rect
	DwFlags   uint32
	SzDevice  [32]uint16
}

const (
	monitorInfoPrimary = 0x00000001
	// SW_MAXIMIZE, and SWP_NOACTIVATE so restoring a position does not steal
	// focus from whatever the person is already looking at.
	swMaximize    = 3
	swpNoActivate = 0x0010
)

// Monitors lists the displays currently attached.
func Monitors() []Monitor {
	var out []Monitor
	cb := syscall.NewCallback(func(hMonitor, hdc, lprc, lparam uintptr) uintptr {
		var mi monitorInfoEx
		mi.CbSize = uint32(unsafe.Sizeof(mi))
		if ok, _, _ := procGetMonitorInfoW.Call(hMonitor,
			uintptr(unsafe.Pointer(&mi))); ok == 0 {
			// Skip it rather than abandoning the enumeration: one monitor that
			// will not describe itself should not hide the others.
			return 1
		}
		out = append(out, Monitor{
			Device:  syscall.UTF16ToString(mi.SzDevice[:]),
			Work:    toRect(mi.RcWork),
			Primary: mi.DwFlags&monitorInfoPrimary != 0,
		})
		return 1 // keep enumerating
	})
	_, _, _ = procEnumDisplayMonitors.Call(0, 0, cb, 0)
	return out
}

// WindowRect is where a window currently sits, in desktop coordinates.
func WindowRect(hwnd uintptr) (Rect, bool) {
	var r rect
	if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return Rect{}, false
	}
	return toRect(r), true
}

// windowMaximized reports whether the window is currently maximized.
func windowMaximized(hwnd uintptr) bool {
	z, _, _ := procIsZoomed.Call(hwnd)
	return z != 0
}

/*
 * applyPlacement moves a window to where it was last time.
 *
 * A maximized window is restored by maximizing rather than by size: its
 * remembered rectangle is the *restored* one, so setting those bounds and then
 * maximizing gives the right window in the right place on the right screen,
 * and un-maximizing later lands where it should.
 */
func applyPlacement(hwnd uintptr, p Placement) {
	target, ok := Resolve(p, Monitors())
	if !ok {
		return
	}
	_, _, _ = procSetWindowPos.Call(hwnd, 0,
		uintptr(int32(target.Left)), uintptr(int32(target.Top)),
		uintptr(int32(target.Width())), uintptr(int32(target.Height())),
		swpNoOwnerZOrder|swpNoActivate)
	if p.Maximized {
		_, _, _ = procShowWindow.Call(hwnd, swMaximize)
	}
}

/*
 * capturePlacement reads where a window is, for saving.
 *
 * A maximized window reports the whole screen as its rectangle, which is not
 * what should be remembered: restoring that as a *size* gives a window exactly
 * covering the screen but not actually maximized, and un-maximizing it does
 * nothing visible. The restored bounds come from the placement Windows keeps
 * for exactly this purpose.
 */
func capturePlacement(hwnd uintptr) (Placement, bool) {
	maxed := windowMaximized(hwnd)

	var win Rect
	if maxed {
		var wp windowPlacement
		wp.Length = uint32(unsafe.Sizeof(wp))
		if ok, _, _ := procGetWindowPlacement.Call(hwnd,
			uintptr(unsafe.Pointer(&wp))); ok == 0 {
			return Placement{}, false
		}
		win = toRect(wp.NormalPosition)
	} else {
		var ok bool
		if win, ok = WindowRect(hwnd); !ok {
			return Placement{}, false
		}
	}
	if win.empty() {
		return Placement{}, false
	}
	return Capture(win, Monitors(), maxed), true
}

func toRect(r rect) Rect {
	return Rect{Left: int(r.Left), Top: int(r.Top),
		Right: int(r.Right), Bottom: int(r.Bottom)}
}

/*
 * placement records where the window was, while it still exists.
 *
 * A window's handle stops answering once the message loop has ended, so the
 * rectangle has to be read at the moment somebody decides to close rather than
 * after the fact — and a plausible rectangle read from a destroyed window is
 * worse than none, because it would be saved.
 */
type placement struct {
	mu  sync.Mutex
	p   Placement
	got bool
}

func (r *placement) capture(hwnd uintptr) {
	p, ok := capturePlacement(hwnd)
	if !ok {
		return
	}
	r.mu.Lock()
	r.p, r.got = p, true
	r.mu.Unlock()
}

func (r *placement) get() (Placement, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.p, r.got
}
