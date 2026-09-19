//go:build windows

package webview2

import (
	"errors"
	"unsafe"

	"lancast/internal/webview2/edge"
	"lancast/internal/webview2/w32"

	"golang.org/x/sys/windows"
)

/*
 * The video overlay. LOCAL ADDITION — see PROVENANCE.md and ADR 0067.
 *
 * A native video renderer (libmpv) draws a GPU swapchain into the main window.
 * A transparent WebView2 *in the same window* does not show that swapchain
 * through — the Phase 0 spike proved it composites sibling GDI painting and
 * nothing else — so during video playback the page moves into a second window:
 *
 *   - WS_POPUP, owned by the main window, so it minimises and stacks with it;
 *   - WS_EX_NOREDIRECTIONBITMAP, so DWM composites the transparent page
 *     directly over whatever is beneath it, swapchains included;
 *   - WS_EX_TOOLWINDOW, so it never becomes a second taskbar button.
 *
 * It exists only while a video plays. Browsing never sees it: the page lives
 * in the main window as it always has, and leaving the overlay puts it back.
 *
 * Keeping two top-level windows in step is the cost, and every piece of it is
 * in this file or the window procedure: position and size (WM_MOVE, WM_SIZE),
 * visibility (WM_SHOWWINDOW, since hiding an owner does not hide what it owns),
 * and focus (WM_ACTIVATE — Alt-Tab lands on the main window, and the keyboard
 * has to follow to the page, which was the rough edge the spike reported).
 */

var (
	user32overlay       = windows.NewLazySystemDLL("user32.dll")
	procClientToScreen  = user32overlay.NewProc("ClientToScreen")
	procSetActiveWindow = user32overlay.NewProc("SetActiveWindow")
	procIsWindowVisible = user32overlay.NewProc("IsWindowVisible")
)

const (
	wmShowWindow           = 0x0018
	wsPopup                = 0x80000000
	wsVisible              = 0x10000000
	wsExToolWindow         = 0x00000080
	wsExNoRedirectionBitmp = 0x00200000
	swpNoZOrder            = 0x0004
	swpNoActivate          = 0x0010
	swHide                 = 0
	swShowNA               = 8
)

// overlayOf marks an overlay window in the window-context map, so the shared
// window procedure can tell it from the main window.
type overlayOf struct{ w *webview }

var opaqueWhite = edge.COREWEBVIEW2_COLOR{A: 255, R: 255, G: 255, B: 255}

// EnterVideoOverlay moves the page into a transparent overlay above the main
// window and returns the main window's handle, for the renderer to draw into.
// Calling it while already in the overlay returns the same handle.
func (w *webview) EnterVideoOverlay() (uintptr, error) {
	ch, ok := w.browser.(*edge.Chromium)
	if !ok {
		return 0, errors.New("webview2: overlay needs the Chromium browser")
	}
	if w.overlay != 0 {
		return w.hwnd, nil
	}
	var hinstance windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &hinstance)
	className, _ := windows.UTF16PtrFromString("webview")
	style := uintptr(wsPopup)
	if v, _, _ := procIsWindowVisible.Call(w.hwnd); v != 0 {
		style |= wsVisible
	}
	popup, _, _ := w32.User32CreateWindowExW.Call(
		wsExNoRedirectionBitmp|wsExToolWindow,
		uintptr(unsafe.Pointer(className)), 0, style,
		0, 0, 1, 1, w.hwnd, 0, uintptr(hinstance), 0,
	)
	if popup == 0 {
		return 0, errors.New("webview2: could not create the overlay window")
	}
	if err := ch.SetBackground(edge.COREWEBVIEW2_COLOR{}); err != nil {
		_, _, _ = w32.User32DestroyWindow.Call(popup)
		return 0, err
	}
	setWindowContext(popup, overlayOf{w})
	w.overlay = popup
	w.syncOverlay()
	ch.Reparent(popup)
	_, _, _ = procSetActiveWindow.Call(popup)
	ch.Focus()
	return w.hwnd, nil
}

// LeaveVideoOverlay puts the page back in the main window and destroys the
// overlay. Safe to call when not in the overlay.
func (w *webview) LeaveVideoOverlay() {
	if w.overlay == 0 {
		return
	}
	ch, ok := w.browser.(*edge.Chromium)
	if ok {
		ch.Reparent(w.hwnd)
		_ = ch.SetBackground(opaqueWhite)
	}
	popup := w.overlay
	w.overlay = 0
	_, _, _ = w32.User32DestroyWindow.Call(popup)
	setWindowContext(popup, nil)
	_, _, _ = procSetActiveWindow.Call(w.hwnd)
	if ok {
		ch.Focus()
	}
}

// syncOverlay lays the overlay exactly over the main window's client area.
func (w *webview) syncOverlay() {
	if w.overlay == 0 {
		return
	}
	var rc w32.Rect
	_, _, _ = w32.User32GetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&rc)))
	pt := w32.Point{}
	_, _, _ = procClientToScreen.Call(w.hwnd, uintptr(unsafe.Pointer(&pt)))
	_, _, _ = w32.User32SetWindowPos.Call(w.overlay, 0,
		uintptr(pt.X), uintptr(pt.Y), uintptr(rc.Right-rc.Left), uintptr(rc.Bottom-rc.Top),
		swpNoZOrder|swpNoActivate)
	w.browser.Resize()
}

// overlayMessage handles the main window's messages that the overlay has to
// follow. It reports whether it consumed the message.
func (w *webview) overlayMessage(msg, wp uintptr) bool {
	if w.overlay == 0 {
		return false
	}
	switch msg {
	case w32.WMMove, w32.WMSize:
		w.syncOverlay()
		return msg == w32.WMSize // the browser is not in this window to resize
	case wmShowWindow:
		if wp == 0 {
			_, _, _ = w32.User32ShowWindow.Call(w.overlay, swHide)
		} else {
			_, _, _ = w32.User32ShowWindow.Call(w.overlay, swShowNA)
		}
	case w32.WMActivate:
		// The low word is the state; the high word says whether it is minimised.
		if wp&0xffff != w32.WAInactive {
			// Alt-Tab and taskbar clicks land here, on the main window; the
			// keyboard belongs to the page in the overlay.
			_, _, _ = procSetActiveWindow.Call(w.overlay)
			w.browser.Focus()
			return true
		}
	}
	return false
}
