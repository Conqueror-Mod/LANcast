//go:build windows

package webview2

import (
	"encoding/json"
	"log/slog"
	"unsafe"

	"lancast/internal/webview2/w32"
)

/*
 * The page leaves the screen while nothing is over a full-size film (ADR 0111).
 *
 * Anything drawn over mpv's swapchain makes DWM compose the two, on a clock
 * that on a desk with other monitors on is not the TV's: up to 281 of 2,878
 * frames lost in two minutes, each one a visible hitch. With the controls
 * faded out the overlay draws nothing, and its only effect is that
 * composition. Hidden, with the TV the primary display and the window
 * fullscreen, Windows flips the film straight to the screen: measured at
 * 0 lost and 2,873 of 2,873 frames shown for exactly one refresh.
 *
 * While the overlay is hidden the main window holds activation, so a key
 * arrives here; it wakes the page and is then handed to it, so Space pauses
 * rather than only waking. The pointer is over mpv's window, which reports
 * nothing, so a timer watches it, and only a deliberate move over the
 * picture counts (pointerWakes).
 */

var (
	procGetCursorPos = user32overlay.NewProc("GetCursorPos")
	procGetKeyState  = user32overlay.NewProc("GetKeyState")
	procGetWindowRc  = user32overlay.NewProc("GetWindowRect")
)

const (
	idleTimer    = 0x4c44
	idleInterval = 100
	wmKeyDown    = 0x0100
	wmSysKeyDown = 0x0104
	vkShift      = 0x10
	vkControl    = 0x11
	vkMenu       = 0x12 // Alt
)

// SetOverlayIdle records whether the page has anything to show over the film,
// and hides or restores the overlay to match.
func (w *webview) SetOverlayIdle(idle bool) error {
	w.idleAsked = idle
	w.applyIdle()
	return nil
}

// idleAllowed is when hiding the overlay is safe: a full-size film, no game
// under the page, and the page actually in the overlay.
func (w *webview) idleAllowed() bool {
	return w.idleAsked && w.layout == VideoFull && !w.gameOn && w.overlay != 0
}

func (w *webview) applyIdle() {
	switch want := w.idleAllowed(); {
	case want && !w.idleHidden:
		w.hideOverlayForIdle()
	case !want && w.idleHidden:
		w.showOverlayAfterIdle(nil, false)
	}
}

func (w *webview) hideOverlayForIdle() {
	w.idleHidden = true
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&w.idleCursor)))
	// Activation to the main window first, so the keyboard has somewhere to go
	// once the overlay is gone. idleHidden is already set, so the WM_ACTIVATE
	// this causes does not hand activation straight back to the overlay.
	_, _, _ = procSetActiveWindow.Call(w.hwnd)
	_, _, _ = w32.User32SetFocus.Call(w.hwnd)
	_, _, _ = w32.User32ShowWindow.Call(w.overlay, swHide)
	_, _, _ = procSetTimer.Call(w.hwnd, idleTimer, idleInterval, 0)
	slog.Debug("native video: page overlay hidden while nothing is over the film")
}

// showOverlayAfterIdle puts the overlay back. woken says the person did it,
// so the page is told to show its controls, and key is what they pressed, if
// anything the page should act on.
func (w *webview) showOverlayAfterIdle(key *wakeKey, woken bool) {
	w.idleHidden = false
	_, _, _ = procKillTimer.Call(w.hwnd, idleTimer)
	if w.overlay == 0 {
		return
	}
	if woken {
		// The page's own answer arrives as SetOverlayIdle(false); until it
		// does, a second wake must not hide the overlay again.
		w.idleAsked = false
	}
	_, _, _ = w32.User32ShowWindow.Call(w.overlay, swShowNA)
	w.syncVideo()
	w.activateOverlay()
	slog.Debug("native video: page overlay shown", "woken", woken)
	if !woken {
		return
	}
	arg := "null"
	if key != nil {
		if b, err := json.Marshal(key); err == nil {
			arg = string(b)
		}
	}
	// Out of the window procedure before touching the browser, as the shield
	// does with its clicks.
	w.Dispatch(func() {
		w.Eval("window.__lancastNativeWake && window.__lancastNativeWake(" + arg + ")")
	})
}

// idleMessage handles the main window's messages while the overlay is hidden.
func (w *webview) idleMessage(msg, wp uintptr) (uintptr, bool) {
	if msg == wmTimer && wp == idleTimer {
		if !w.idleHidden {
			_, _, _ = procKillTimer.Call(w.hwnd, idleTimer)
			return 0, true
		}
		var p w32.Point
		_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
		var picture w32.Rect
		_, _, _ = procGetWindowRc.Call(w.video, uintptr(unsafe.Pointer(&picture)))
		wake, rest := pointerWakes(w.idleCursor, p, picture)
		w.idleCursor = rest
		if wake {
			w.showOverlayAfterIdle(nil, true)
		}
		return 0, true
	}
	if !w.idleHidden {
		return 0, false
	}
	switch msg {
	case wmKeyDown, wmSysKeyDown:
		var key *wakeKey
		if k, ok := keyFor(uint32(wp), keyHeld(vkShift), keyHeld(vkControl) || keyHeld(vkMenu)); ok {
			key = &k
		}
		w.showOverlayAfterIdle(key, true)
		return 0, true
	}
	return 0, false
}

// keyHeld reads a modifier's state as of the message being handled.
func keyHeld(vk uintptr) bool {
	s, _, _ := procGetKeyState.Call(vk)
	return int16(s) < 0
}
