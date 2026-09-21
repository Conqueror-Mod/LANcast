//go:build windows

package webview2

import (
	"errors"
	"sync"
	"unsafe"

	"lancast/internal/webview2/edge"
	"lancast/internal/webview2/w32"

	"golang.org/x/sys/windows"
)

/*
 * Native video layout. LOCAL ADDITION — see PROVENANCE.md and ADR 0067.
 *
 * A native renderer (libmpv) draws a GPU swapchain. The Phase 0 spike proved a
 * transparent WebView2 in the same window never shows one through, so video
 * gets windows of its own. Two, both owned by the main window (so they minimise
 * and stack with it) and both kept out of the taskbar:
 *
 *   - the **video window**, which the renderer draws into. It is placed where
 *     the picture belongs.
 *   - the **page overlay**, a WS_EX_NOREDIRECTIONBITMAP popup the page moves
 *     into so DWM composites it, transparent, over the video window.
 *
 * Three layouts:
 *
 *   full   video window over the whole client area, the page overlay above it:
 *          the React chrome draws on the picture.
 *   mini   the page is back in the main window, opaque, and the video window
 *          floats above it at the docked rectangle. A hole in a transparent
 *          page cannot do this — everything beneath the hole is the page's own
 *          opaque content — so the picture goes on top instead.
 *   hidden video window hidden, page in the main window: browsing as ever.
 *
 * Keeping these in step with the main window is the cost, and all of it is in
 * this file: position and size (WM_MOVE, WM_SIZE), visibility (WM_SHOWWINDOW,
 * since hiding an owner does not hide what it owns), and focus (WM_ACTIVATE,
 * since Alt-Tab lands on the main window and the keyboard belongs to the page).
 */

var (
	user32overlay       = windows.NewLazySystemDLL("user32.dll")
	procClientToScreen  = user32overlay.NewProc("ClientToScreen")
	procSetActiveWindow = user32overlay.NewProc("SetActiveWindow")
	procIsWindowVisible = user32overlay.NewProc("IsWindowVisible")

	gdi32overlay       = windows.NewLazySystemDLL("gdi32.dll")
	procGetStockObject = gdi32overlay.NewProc("GetStockObject")
)

// blackBrush is GetStockObject(BLACK_BRUSH). A stock object is owned by the
// system and never freed.
const stockBlackBrush = 4

/*
 * videoClass is a window class of its own for the picture, and it exists for
 * one field: a background brush.
 *
 * The class everything else here uses leaves HbrBackground unset, which means
 * a null brush, which means nothing erases the window. For the main window
 * that is right -- the web view paints every pixel of it -- but the picture
 * window is empty until mpv presents its first frame, and what shows in the
 * meantime is whatever the compositor had. In practice, white.
 *
 * That was reported twice. First as a black-and-white pattern on every start
 * and stop, which was this on top of a white page backdrop; the backdrop was
 * fixed and what remained was a plain white rectangle over the whole picture
 * area, longer on starting than on stopping because opening a file takes
 * longer than closing one.
 *
 * Black rather than the page's near-black: this is the surface a video sits
 * on, it is what every player letterboxes to, and it is what the picture
 * itself fades from.
 */
var videoClass = sync.OnceValue(func() *uint16 {
	name, err := windows.UTF16PtrFromString("webview-video")
	if err != nil {
		return nil
	}
	var hinstance windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &hinstance)

	brush, _, _ := procGetStockObject.Call(stockBlackBrush)
	if brush == 0 {
		// No brush is the behaviour that was already there, not a reason to
		// fail to create the window the picture goes in.
		return nil
	}

	wc := w32.WndClassExW{
		CbSize:        uint32(unsafe.Sizeof(w32.WndClassExW{})),
		HInstance:     hinstance,
		LpszClassName: name,
		LpfnWndProc:   windows.NewCallback(wndproc),
		HbrBackground: windows.Handle(brush),
	}
	if ret, _, _ := w32.User32RegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); ret == 0 {
		return nil
	}
	return name
})

const (
	wmShowWindow            = 0x0018
	wmMouseActivate         = 0x0021
	maNoActivate            = 3
	wsPopup                 = 0x80000000
	wsExToolWindow          = 0x00000080
	wsExNoActivate          = 0x08000000
	wsExNoRedirectionBitmap = 0x00200000
	swpNoMove               = 0x0002
	swpNoSize               = 0x0001
	swpNoZOrder             = 0x0004
	swpNoActivate           = 0x0010
	swpShowWindow           = 0x0040
	swpHideWindow           = 0x0080
	swHide                  = 0
	swShowNA                = 8
)

// VideoLayout is where native video goes.
type VideoLayout int

const (
	VideoHidden VideoLayout = iota
	VideoFull
	VideoMini
)

// videoRect is a rectangle in the main window's client coordinates, physical
// pixels.
type videoRect struct{ x, y, w, h int32 }

// overlayOf and videoOf mark the extra windows in the window-context map, so
// the shared window procedure can tell them from the main window.
type overlayOf struct{ w *webview }
type videoOf struct{ w *webview }

/*
 * The backdrop the web view paints when it is not the transparent overlay.
 *
 * It used to be opaque **white**, which is WebView2's own default and was
 * simply what "put it back" meant. The cost showed up as a white rectangle
 * flashing across the window every time a film started, was skipped, or
 * stopped: leaving the overlay reparents the Chromium control back to the main
 * window, and for the moment between the background turning opaque and the
 * page painting over it, the backdrop is all there is to see. At a stale size,
 * mid-reparent, on a black screen.
 *
 * `--space-void` is what the page paints there anyway, so matching it makes
 * the gap invisible rather than merely shorter. The client has no light theme
 * (`web/src/styles/tokens.css`), so there is no case where a pale backdrop is
 * the right one.
 */
var opaqueBackdrop = edge.COREWEBVIEW2_COLOR{
	A: 255, R: backdropR, G: backdropG, B: backdropB,
}

func (w *webview) createOwned(exStyle uintptr) uintptr {
	return w.createOwnedOf(exStyle, nil)
}

// createOwnedOf creates an owned popup of a given class, or of the shared one
// when class is nil -- which is also what a failed registration falls back to,
// since a window with the wrong background is better than no picture at all.
func (w *webview) createOwnedOf(exStyle uintptr, class *uint16) uintptr {
	var hinstance windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &hinstance)
	className := class
	if className == nil {
		className, _ = windows.UTF16PtrFromString("webview")
	}
	h, _, _ := w32.User32CreateWindowExW.Call(
		exStyle|wsExToolWindow,
		uintptr(unsafe.Pointer(className)), 0, wsPopup,
		0, 0, 1, 1, w.hwnd, 0, uintptr(hinstance), 0,
	)
	return h
}

// VideoWindow returns the window a native renderer draws into, creating it
// hidden on first use.
func (w *webview) VideoWindow() (uintptr, error) {
	if w.video != 0 {
		return w.video, nil
	}
	// Never activated: focus stays with the page whichever layout is showing.
	// Its own class, for a background that is black rather than whatever the
	// compositor left there -- see videoClass.
	h := w.createOwnedOf(wsExNoActivate, videoClass())
	if h == 0 {
		return 0, errors.New("webview2: could not create the video window")
	}
	setWindowContext(h, videoOf{w})
	w.video = h
	return h, nil
}

// SetVideoLayout places native video. x, y, width and height are the docked
// rectangle in client pixels and are read only for VideoMini.
func (w *webview) SetVideoLayout(layout VideoLayout, x, y, width, height int) error {
	w.layout = layout
	w.mini = videoRect{int32(x), int32(y), int32(width), int32(height)}
	switch layout {
	case VideoFull:
		if _, err := w.VideoWindow(); err != nil {
			return err
		}
		if err := w.enterOverlay(); err != nil {
			return err
		}
	case VideoMini:
		if _, err := w.VideoWindow(); err != nil {
			return err
		}
		w.leaveOverlay()
	default:
		w.leaveOverlay()
	}
	w.syncVideo()
	return nil
}

func (w *webview) enterOverlay() error {
	if w.overlay != 0 {
		return nil
	}
	ch, ok := w.browser.(*edge.Chromium)
	if !ok {
		return errors.New("webview2: overlay needs the Chromium browser")
	}
	popup := w.createOwned(wsExNoRedirectionBitmap)
	if popup == 0 {
		return errors.New("webview2: could not create the overlay window")
	}
	if err := ch.SetBackground(edge.COREWEBVIEW2_COLOR{}); err != nil {
		_, _, _ = w32.User32DestroyWindow.Call(popup)
		return err
	}
	setWindowContext(popup, overlayOf{w})
	w.overlay = popup
	w.syncVideo()
	ch.Reparent(popup)
	_, _, _ = procSetActiveWindow.Call(popup)
	ch.Focus()
	return nil
}

func (w *webview) leaveOverlay() {
	if w.overlay == 0 {
		return
	}
	ch, ok := w.browser.(*edge.Chromium)
	if ok {
		ch.Reparent(w.hwnd)
		_ = ch.SetBackground(opaqueBackdrop)
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

// syncVideo lays both extra windows out for the current layout.
func (w *webview) syncVideo() {
	var rc w32.Rect
	_, _, _ = w32.User32GetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&rc)))
	origin := w32.Point{}
	_, _, _ = procClientToScreen.Call(w.hwnd, uintptr(unsafe.Pointer(&origin)))
	visible, _, _ := procIsWindowVisible.Call(w.hwnd)
	full := videoRect{origin.X, origin.Y, rc.Right - rc.Left, rc.Bottom - rc.Top}

	if w.overlay != 0 {
		flags := uintptr(swpNoZOrder | swpNoActivate)
		if visible != 0 {
			flags |= swpShowWindow
		}
		_, _, _ = w32.User32SetWindowPos.Call(w.overlay, 0,
			uintptr(full.x), uintptr(full.y), uintptr(full.w), uintptr(full.h), flags)
		w.browser.Resize()
	}

	if w.video == 0 {
		return
	}
	switch {
	case visible == 0 || w.layout == VideoHidden:
		_, _, _ = w32.User32SetWindowPos.Call(w.video, 0, 0, 0, 0, 0,
			swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpHideWindow)
	case w.layout == VideoFull:
		// Directly beneath the page overlay: SetWindowPos places a window
		// *after* the one named, which in z-order means under it.
		_, _, _ = w32.User32SetWindowPos.Call(w.video, w.overlay,
			uintptr(full.x), uintptr(full.y), uintptr(full.w), uintptr(full.h),
			swpNoActivate|swpShowWindow)
	case w.layout == VideoMini:
		// Owned windows already stack above their owner; no z-order needed.
		_, _, _ = w32.User32SetWindowPos.Call(w.video, 0,
			uintptr(origin.X+w.mini.x), uintptr(origin.Y+w.mini.y), uintptr(w.mini.w), uintptr(w.mini.h),
			swpNoZOrder|swpNoActivate|swpShowWindow)
	}
}

// overlayMessage handles the main window's messages the extra windows follow.
// It reports whether it consumed the message.
func (w *webview) overlayMessage(msg, wp uintptr) bool {
	if w.overlay == 0 && w.video == 0 {
		return false
	}
	switch msg {
	case w32.WMMove, w32.WMSize:
		w.syncVideo()
		// With the page in the overlay there is no browser in this window to
		// resize; otherwise the ordinary handling still has to run.
		return msg == w32.WMSize && w.overlay != 0
	case wmShowWindow:
		if wp == 0 {
			if w.overlay != 0 {
				_, _, _ = w32.User32ShowWindow.Call(w.overlay, swHide)
			}
			if w.video != 0 {
				_, _, _ = w32.User32ShowWindow.Call(w.video, swHide)
			}
			return false
		}
		if w.overlay != 0 {
			_, _, _ = w32.User32ShowWindow.Call(w.overlay, swShowNA)
		}
		w.syncVideo()
	case w32.WMActivate:
		// The low word is the state; the high word says whether it is minimised.
		if w.overlay != 0 && wp&0xffff != w32.WAInactive {
			// Alt-Tab and taskbar clicks land here, on the main window; the
			// keyboard belongs to the page in the overlay.
			_, _, _ = procSetActiveWindow.Call(w.overlay)
			w.browser.Focus()
			return true
		}
	}
	return false
}
