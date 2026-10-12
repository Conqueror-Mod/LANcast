//go:build windows

package webview2

import (
	"log/slog"
	"sync/atomic"
	"time"
)

/*
 * A trace of the close button, while a film plays full. LOCAL ADDITION.
 *
 * Reported twice: with the player full, the window's ✕ does nothing, and with
 * the film docked in the corner it closes. The difference between the two is
 * the page overlay (overlay.go), which takes activation from the main window
 * and is handed it back on activation — the first fix assumed the hand-off
 * swallowed the click and deferred it, and the report outlived the fix. So
 * this records what actually happens between pressing ✕ and the window
 * hiding, rather than another theory about it: the press, the system command,
 * the close, and anything that interrupts them, each with how long after the
 * press it came. Quiet unless a close is in flight, so it costs nothing.
 */

const (
	htClose        = 20
	wmNCLButtonUp  = 0x00A2
	scClose        = 0xF060
	wmCancelMode   = 0x001F
	wmCaptureChang = 0x0215
	wmNCActivateTr = 0x0086
	wmActivateTr   = 0x0006
)

// closeArmed is when ✕ was last pressed, in UnixNano, or zero.
var closeArmed atomic.Int64

// traceClose is called first for every message to the main window.
func (w *webview) traceClose(msg, wp uintptr) {
	switch {
	case msg == 0x00A1 && wp == htClose: // WM_NCLBUTTONDOWN on ✕
		closeArmed.Store(time.Now().UnixNano())
		slog.Info("close trace: ✕ pressed", "overlay", w.overlay != 0, "layout", layoutName(w.layout))
		return
	}
	at := closeArmed.Load()
	if at == 0 {
		return
	}
	since := time.Since(time.Unix(0, at))
	if since > 3*time.Second {
		closeArmed.Store(0)
		return
	}
	ms := since.Milliseconds()
	switch msg {
	case wmNCLButtonUp:
		slog.Info("close trace: button released", "ms", ms, "on_close", wp == htClose)
	case wmSysCommand:
		if wp&0xFFF0 == scClose {
			slog.Info("close trace: close command", "ms", ms)
		}
	case 0x0010: // WM_CLOSE
		slog.Info("close trace: close message", "ms", ms)
	case wmCancelMode:
		slog.Info("close trace: cancelled", "ms", ms)
	case wmCaptureChang:
		slog.Info("close trace: capture lost", "ms", ms)
	case wmActivateTr:
		slog.Info("close trace: activation", "ms", ms, "state", wp&0xffff)
	case wmNCActivateTr:
		slog.Info("close trace: frame activation", "ms", ms, "active", wp != 0)
	}
}

// traceHandOff notes the overlay taking activation while a close is in flight.
func traceHandOff(why string) {
	at := closeArmed.Load()
	if at == 0 {
		return
	}
	if since := time.Since(time.Unix(0, at)); since <= 3*time.Second {
		slog.Info("close trace: page overlay activated", "ms", since.Milliseconds(), "by", why)
	}
}
