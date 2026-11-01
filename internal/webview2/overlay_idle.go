//go:build windows

package webview2

import (
	"log/slog"
	"unsafe"

	"lancast/internal/webview2/w32"
)

/*
 * SPIKE (docs/uncomposed-video-plan.md, phase 1): the page overlay leaves the
 * screen while a full-size film has nothing to show over it.
 *
 * Anything drawn over mpv's swapchain makes DWM compose the two on the desktop's
 * clock, which on a desk with other monitors on is never the TV's. The baseline
 * measured 7% of a 24p film's frames never reaching the TV that way. With the
 * controls faded out the overlay draws nothing, so hiding it costs nothing
 * visible and may let Windows flip the video straight to the screen.
 *
 * While it is hidden the main window holds activation, so a key lands on it,
 * and a timer watches the cursor, because the mouse is over mpv's own window,
 * which reports nothing. Either one brings the overlay back and tells the page
 * (window.__lancastNativeWake), which shows its controls as a mouse move would.
 * The key that wakes it is spent on waking, as many players do.
 */

var procGetCursorPos = user32overlay.NewProc("GetCursorPos")

const (
	idleTimer    = 0x4c44
	idleInterval = 100
	wmKeyDown    = 0x0100
	wmSysKeyDown = 0x0104
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
		w.showOverlayAfterIdle(false)
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
	slog.Info("native video: page overlay hidden while idle")
}

// showOverlayAfterIdle puts the overlay back. woken says the person did it,
// so the page is told to show its controls.
func (w *webview) showOverlayAfterIdle(woken bool) {
	w.idleHidden = false
	_, _, _ = procKillTimer.Call(w.hwnd, idleTimer)
	if w.overlay == 0 {
		return
	}
	if woken {
		// The page's own answer will arrive as SetOverlayIdle(false); until it
		// does, a second wake must not hide the overlay again.
		w.idleAsked = false
	}
	_, _, _ = w32.User32ShowWindow.Call(w.overlay, swShowNA)
	w.syncVideo()
	w.activateOverlay()
	slog.Info("native video: page overlay shown", "woken", woken)
	if woken {
		w.Eval("window.__lancastNativeWake && window.__lancastNativeWake()")
	}
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
		if p != w.idleCursor {
			w.showOverlayAfterIdle(true)
		}
		return 0, true
	}
	if !w.idleHidden {
		return 0, false
	}
	switch msg {
	case wmKeyDown, wmSysKeyDown:
		w.showOverlayAfterIdle(true)
		return 0, true
	}
	return 0, false
}
