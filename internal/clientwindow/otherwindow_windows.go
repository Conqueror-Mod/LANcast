package clientwindow

import (
	"syscall"
	"unsafe"
)

/*
 * Moving somebody else's window: a game's, onto the screen chosen for it.
 *
 * The facts and the motion only; what to do is decided in games.PlanMove, where
 * it is tested without a screen. These sit here beside the code that remembers
 * LANcast's own window, because they are the same few Win32 calls.
 */

const (
	swpNoSizeFlag  = 0x0001
	swpNoZOrderFlg = 0x0004
)

// WindowState is what deciding a move needs: where the window is, whether it
// is maximized, and its restored size (from the placement Windows keeps, since
// a maximized window's rectangle is the whole screen).
func WindowState(hwnd uintptr) (win Rect, maximized bool, normal Rect, ok bool) {
	if win, ok = WindowRect(hwnd); !ok {
		return Rect{}, false, Rect{}, false
	}
	maximized = windowMaximized(hwnd)
	var wp windowPlacement
	wp.Length = uint32(unsafe.Sizeof(wp))
	if r, _, _ := procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&wp))); r != 0 {
		normal = toRect(wp.NormalPosition)
	}
	return win, maximized, normal, true
}

// MonitorOfWindow is the device name of the screen Windows considers a window
// to be on: the one holding most of it. Asked of Windows rather than worked out
// from the window's centre, which for a window larger than its screen can sit
// on the next one.
func MonitorOfWindow(hwnd uintptr) string {
	const monitorDefaultToNearest = 2
	mon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	if mon == 0 {
		return ""
	}
	var mi monitorInfoEx
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if ok, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		return ""
	}
	return syscall.UTF16ToString(mi.SzDevice[:])
}

/*
 * PlaceWindow carries out a move.
 *
 * Restore first for a maximized window, since a maximized window moved by
 * position keeps the size of the screen it was maximized on. Then position,
 * and size when one is given. Then maximize, which Windows does on the screen
 * the window is now on. No activation for the move itself: a game still
 * loading is not pulled in front. Maximizing does activate it, which is the
 * game the person just pressed Play on.
 */
func PlaceWindow(hwnd uintptr, x, y, w, h int, restore, maximize bool) bool {
	if restore {
		_, _, _ = procShowWindow.Call(hwnd, swRestore)
	}
	flags := uintptr(swpNoZOrderFlg | swpNoActivate)
	if w <= 0 || h <= 0 {
		flags |= swpNoSizeFlag
		w, h = 0, 0
	}
	ok, _, _ := procSetWindowPos.Call(hwnd, 0,
		uintptr(int32(x)), uintptr(int32(y)), uintptr(int32(w)), uintptr(int32(h)), flags)
	if ok == 0 {
		return false
	}
	if maximize {
		_, _, _ = procShowWindow.Call(hwnd, swMaximize)
	}
	return true
}
