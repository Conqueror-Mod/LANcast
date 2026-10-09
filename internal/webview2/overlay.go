//go:build windows

package webview2

import (
	"errors"
	"log/slog"
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
	procIsIconic        = user32overlay.NewProc("IsIconic")
	procGetCapture      = user32overlay.NewProc("GetCapture")
	procGetActiveWindow = user32overlay.NewProc("GetActiveWindow")
	procSetTimer        = user32overlay.NewProc("SetTimer")
	procKillTimer       = user32overlay.NewProc("KillTimer")
	procLoadCursorW     = user32overlay.NewProc("LoadCursorW")
	procSetLayeredAttrs = user32overlay.NewProc("SetLayeredWindowAttributes")

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
	wmNCActivate            = 0x0086
	wmDPIChanged            = 0x02E0
	wmLButtonUp             = 0x0202
	wsExLayered             = 0x00080000
	lwaAlpha                = 0x00000002
	idcHand                 = 32649
	wmTimer                 = 0x0113
	waClickActive           = 2
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
	// VideoPiP is the docked picture while a game holds the screen (ADR 0076):
	// the page stays in the overlay over the game, and the picture floats above
	// the overlay at the docked box rather than above the main window.
	VideoPiP
)

/*
 * The two layout questions, as pure functions of what the page asked for and
 * whether a game is on screen (ADR 0076).
 *
 * The page sends "pip" when it knows a game is up and "mini" otherwise, but
 * the game's window comes and goes on the client's own schedule, and the two
 * messages can cross. So the client does not trust the word alone: a docked
 * picture over a game is always PiP, and PiP with no game under it is just
 * docked. Either crossing would otherwise leave the picture under the page
 * (mini with the page in the overlay) or the page torn out from over a game
 * (PiP leaving the overlay), and neither looks like anything but a bug.
 */
func effectiveLayout(asked VideoLayout, gameOn bool) VideoLayout {
	switch {
	case asked == VideoMini && gameOn:
		return VideoPiP
	case asked == VideoPiP && !gameOn:
		return VideoMini
	}
	return asked
}

func layoutName(l VideoLayout) string {
	switch l {
	case VideoFull:
		return "full"
	case VideoMini:
		return "mini"
	case VideoPiP:
		return "pip"
	}
	return "hidden"
}

// overlayWanted is whether the page belongs in the transparent overlay: over
// a film at full size, or over a game.
func overlayWanted(layout VideoLayout, gameOn bool) bool {
	return gameOn || layout == VideoFull
}

// videoRect is a rectangle in the main window's client coordinates, physical
// pixels.
type videoRect struct{ x, y, w, h int32 }

// overlayOf and videoOf mark the extra windows in the window-context map, so
// the shared window procedure can tell them from the main window.
type overlayOf struct{ w *webview }
type videoOf struct{ w *webview }
type shieldOf struct{ w *webview }

/*
 * The shield: a click on the docked picture opens the player.
 *
 * In the docked layout the picture is a window of its own above the page, and
 * inside it is mpv's window, which takes every click on the picture and does
 * nothing with it -- mpv is told to handle no input (mpv/options.go). The
 * page, which knows how to open the player, never heard a thing. Reported as
 * "the only way to maximize a film is to click the title".
 *
 * So a third window lies over the picture while it is docked: layered at an
 * alpha of 1 in 255, which Windows still hit-tests and nobody can see, never
 * activated, with a hand cursor. A click on it calls the page's own handler
 * (window.__lancastNativeClick in PlaybackProvider), the same thing a click
 * on the browser player's picture does. It exists only in the docked layout;
 * full size, the page itself is on top and needs no help.
 */
var shieldClass = sync.OnceValue(func() *uint16 {
	name, err := windows.UTF16PtrFromString("webview-shield")
	if err != nil {
		return nil
	}
	var hinstance windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &hinstance)
	cursor, _, _ := procLoadCursorW.Call(0, idcHand)
	wc := w32.WndClassExW{
		CbSize:        uint32(unsafe.Sizeof(w32.WndClassExW{})),
		HInstance:     hinstance,
		LpszClassName: name,
		LpfnWndProc:   windows.NewCallback(wndproc),
		HCursor:       windows.Handle(cursor),
	}
	if ret, _, _ := w32.User32RegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); ret == 0 {
		return nil
	}
	return name
})

// ensureShield creates the click-catching window over the docked picture on
// first use. A failure leaves the picture unclickable, as it was, rather than
// failing the layout.
func (w *webview) ensureShield() {
	if w.shield != 0 {
		return
	}
	class := shieldClass()
	if class == nil {
		return
	}
	h := w.createOwnedOf(wsExNoActivate|wsExLayered, class)
	if h == 0 {
		return
	}
	_, _, _ = procSetLayeredAttrs.Call(h, 0, 1, lwaAlpha)
	setWindowContext(h, shieldOf{w})
	w.shield = h
}

// shieldMessage handles the shield's own messages and reports whether it
// consumed one.
func (w *webview) shieldMessage(msg uintptr) (uintptr, bool) {
	switch msg {
	case wmMouseActivate:
		// The keyboard stays with the page.
		return maNoActivate, true
	case wmLButtonUp:
		// Out of the window procedure before touching the browser.
		w.Dispatch(func() {
			w.Eval("window.__lancastNativeClick && window.__lancastNativeClick()")
		})
		return 0, true
	}
	return 0, false
}

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

// GameWindow returns the window a game draws into (ADR 0076), creating it
// hidden on first use. A game has a window of its own so that starting one no
// longer has to stop a film: libmpv keeps the video window, and a docked film
// can go on playing above the game.
func (w *webview) GameWindow() (uintptr, error) {
	if w.game != 0 {
		return w.game, nil
	}
	// The video window's class and rules: black behind the first frame, never
	// activated, so the keyboard stays with the page.
	h := w.createOwnedOf(wsExNoActivate, videoClass())
	if h == 0 {
		return 0, errors.New("webview2: could not create the game window")
	}
	setWindowContext(h, videoOf{w})
	w.game = h
	return h, nil
}

// SetGameLayout shows the game window over the whole client area, beneath the
// page overlay, or hides it.
func (w *webview) SetGameLayout(on bool) error {
	if on {
		if _, err := w.GameWindow(); err != nil {
			return err
		}
	}
	w.gameOn = on
	return w.applyLayout()
}

// SetVideoLayout places native video. x, y, width and height are the docked
// rectangle in client pixels and are read only for VideoMini and VideoPiP.
func (w *webview) SetVideoLayout(layout VideoLayout, x, y, width, height int) error {
	w.asked = layout
	w.mini = videoRect{int32(x), int32(y), int32(width), int32(height)}
	if layout != VideoHidden {
		if _, err := w.VideoWindow(); err != nil {
			return err
		}
	}
	return w.applyLayout()
}

// applyLayout settles the overlay and every extra window for the film's asked
// layout and whether a game is on screen.
func (w *webview) applyLayout() error {
	before, beforeGame := w.layout, w.loggedGame
	w.layout = effectiveLayout(w.asked, w.gameOn)
	// Which picture goes where, each time it changes — not each move, which a
	// drag sends at the frame rate. With the activation line below, enough to
	// tell from the log whether a film in the corner was ever left under the
	// page.
	if w.layout != before || w.gameOn != beforeGame {
		slog.Info("native video layout", "layout", layoutName(w.layout), "game", w.gameOn)
		w.loggedGame = w.gameOn
	}
	if overlayWanted(w.layout, w.gameOn) {
		if err := w.enterOverlay(); err != nil {
			return err
		}
	} else {
		w.leaveOverlay()
	}
	if w.layout == VideoMini {
		w.ensureShield()
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
	setWindowContext(popup, overlayOf{w})
	w.overlay = popup
	w.syncVideo()
	ch.Reparent(popup)
	/*
	 * Transparent **after** the reparent, not before.
	 *
	 * Reparenting re-creates the controller's visual, and the new one starts
	 * at WebView2's default background, which is white. A colour set before
	 * the move is therefore discarded by the very next line -- which is what
	 * made starting a film flash white while stopping one did not, because
	 * leaveOverlay below happened to set its background the other way round.
	 *
	 * Found by asymmetry rather than by reading: the two halves were changed
	 * together and only the half that sets after reparenting came out clean.
	 */
	if err := ch.SetBackground(edge.COREWEBVIEW2_COLOR{}); err != nil {
		ch.Reparent(w.hwnd)
		w.overlay = 0
		_, _, _ = w32.User32DestroyWindow.Call(popup)
		return err
	}
	w.activateOverlay()
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
	_, _, _ = procKillTimer.Call(w.hwnd, refocusTimer)
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

	// The game, directly beneath the page overlay. Placed before the film so
	// that a film placed beneath the overlay afterwards lands above it.
	if w.game != 0 {
		if visible != 0 && w.gameOn && w.overlay != 0 {
			_, _, _ = w32.User32SetWindowPos.Call(w.game, w.overlay,
				uintptr(full.x), uintptr(full.y), uintptr(full.w), uintptr(full.h),
				swpNoActivate|swpShowWindow)
		} else {
			_, _, _ = w32.User32SetWindowPos.Call(w.game, 0, 0, 0, 0, 0,
				swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpHideWindow)
		}
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
		x, y := uintptr(origin.X+w.mini.x), uintptr(origin.Y+w.mini.y)
		cw, ch := uintptr(w.mini.w), uintptr(w.mini.h)
		if w.shield != 0 {
			// The shield on top of the owned windows, the picture directly
			// beneath it: SetWindowPos places a window after the one named.
			_, _, _ = w32.User32SetWindowPos.Call(w.shield, 0, x, y, cw, ch,
				swpNoActivate|swpShowWindow)
			_, _, _ = w32.User32SetWindowPos.Call(w.video, w.shield, x, y, cw, ch,
				swpNoActivate|swpShowWindow)
		} else {
			// Owned windows already stack above their owner; no z-order needed.
			_, _, _ = w32.User32SetWindowPos.Call(w.video, 0, x, y, cw, ch,
				swpNoZOrder|swpNoActivate|swpShowWindow)
		}
	case w.layout == VideoPiP:
		w.raisePiP(origin)
	}
	// The shield is for the docked picture only.
	if w.shield != 0 && (visible == 0 || w.layout != VideoMini) {
		_, _, _ = w32.User32SetWindowPos.Call(w.shield, 0, 0, 0, 0, 0,
			swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpHideWindow)
	}
}

/*
 * The main window's caption while the page is in the overlay.
 *
 * With a film playing full, the window that is *active* is the overlay — it
 * has to be, the keyboard belongs to the page — and the main window, which is
 * the only one with a frame, is by Windows' reckoning inactive. It drew its
 * title bar that way: white instead of the accent colour, for as long as
 * anything played. Reported as "the title bar goes white".
 *
 * Worse, its caption buttons stopped working. Pressing minimise activates the
 * main window before the press is handled, and WM_ACTIVATE below handed
 * activation straight back to the overlay — in the middle of the click, so the
 * button never saw its own press. Minimise, maximise and close all did
 * nothing until playback stopped and the overlay went away.
 *
 * So two things, both here:
 *
 *   - The caption is drawn by who is *really* active. The overlay is part of
 *     this window as far as a person can tell, so while it holds activation
 *     the frame is drawn active, and when activation leaves the application
 *     altogether it is drawn inactive. WM_ACTIVATE's lParam names the window
 *     on the other side of the change, which is what makes that decidable.
 *   - A click on the frame keeps activation where the click put it until the
 *     click is over. Only then does the keyboard go back to the page — on a
 *     timer that waits while the mouse is captured, since a caption button,
 *     a drag and a resize all hold capture for exactly as long as they last.
 *
 * With the overlay covering the whole client area, a click that activates the
 * main window can only have landed on its frame, so WA_CLICKACTIVE is that
 * case precisely.
 */

// refocusTimer is the id of the timer that returns the keyboard to the page
// after a click on the frame. Any value works; timers are per window.
const refocusTimer = 0x4c43

// refocusInterval is short enough that typing straight after a click on the
// title bar reaches the page, long enough to cost nothing while a drag lasts.
const refocusInterval = 50

// ours reports whether h is one of the extra windows this file owns.
func (w *webview) ours(h uintptr) bool {
	return h != 0 && (h == w.overlay || h == w.video || h == w.game)
}

// drawCaption repaints the main window's frame as active or not, without
// changing which window is active.
func (w *webview) drawCaption(active bool) {
	var a uintptr
	if active {
		a = 1
	}
	_, _, _ = w32.User32DefWindowProcW.Call(w.hwnd, wmNCActivate, a, 0)
}

// refocusPage returns activation and the keyboard to the page in the overlay,
// once no click on the frame is in progress. It reports whether it is finished,
// either because it did so or because there is no longer anything to do.
func (w *webview) refocusPage() bool {
	if w.overlay == 0 {
		return true
	}
	if iconic, _, _ := procIsIconic.Call(w.hwnd); iconic != 0 {
		// Minimised by the click. Restoring activates the window again, and
		// that path hands focus over by itself.
		return true
	}
	if active, _, _ := procGetActiveWindow.Call(); active != w.hwnd {
		// Somebody clicked into the picture or another window first.
		return true
	}
	if capture, _, _ := procGetCapture.Call(); capture != 0 {
		return false
	}
	w.activateOverlay()
	return true
}

// overlayWindowMessage handles the messages the overlay itself must act on. It
// runs before the default procedure and never consumes the message.
func (w *webview) overlayWindowMessage(msg, wp, lp uintptr) {
	if msg != w32.WMActivate {
		return
	}
	if wp&0xffff != w32.WAInactive {
		w.drawCaption(true)
		/*
		 * Activating a window brings it to the top of its owner's windows, and
		 * a film in the corner over a game sits *above* this one (VideoPiP).
		 * activateOverlay re-raises the film when activation arrives through
		 * the main window — Alt-Tab, the taskbar — but a click straight onto
		 * the page activates this window itself and went past it. The film
		 * was then under the page, behind the card's black box: reported as
		 * the corner player going black after moving it, which needs exactly
		 * such a click (grabbing the grip after a screenshot tool had focus).
		 * Not reproduced on demand, so the log says when it happens.
		 */
		if w.layout == VideoPiP && w.video != 0 {
			origin := w32.Point{}
			_, _, _ = procClientToScreen.Call(w.hwnd, uintptr(unsafe.Pointer(&origin)))
			w.raisePiP(origin)
			slog.Info("native video: corner picture raised over the page after the page was activated")
		}
		return
	}
	// Leaving for the main window keeps the frame active; leaving for
	// anything else, including another application (lParam zero), does not.
	if lp != w.hwnd && !w.ours(lp) {
		w.drawCaption(false)
	}
}

// overlayMessage handles the main window's messages the extra windows follow.
// It reports whether it consumed the message and, if so, what to return.
func (w *webview) overlayMessage(msg, wp, lp uintptr) (uintptr, bool) {
	if msg == wmTimer && wp == refocusTimer {
		if w.refocusPage() {
			_, _, _ = procKillTimer.Call(w.hwnd, refocusTimer)
		}
		return 0, true
	}
	if w.overlay == 0 && w.video == 0 && w.game == 0 {
		return 0, false
	}
	switch msg {
	case w32.WMMove, w32.WMSize:
		w.syncVideo()
		// With the page in the overlay there is no browser in this window to
		// resize; otherwise the ordinary handling still has to run.
		return 0, msg == w32.WMSize && w.overlay != 0
	case wmNCActivate:
		// Deactivating in favour of the overlay: stay drawn active. The
		// return value must be TRUE or the deactivation is refused.
		if wp == 0 && w.overlay != 0 && w.handingOff {
			r, _, _ := w32.User32DefWindowProcW.Call(w.hwnd, msg, 1, lp)
			return r, true
		}
	case wmShowWindow:
		if wp == 0 {
			if w.overlay != 0 {
				_, _, _ = w32.User32ShowWindow.Call(w.overlay, swHide)
			}
			if w.video != 0 {
				_, _, _ = w32.User32ShowWindow.Call(w.video, swHide)
			}
			if w.game != 0 {
				_, _, _ = w32.User32ShowWindow.Call(w.game, swHide)
			}
			if w.shield != 0 {
				_, _, _ = w32.User32ShowWindow.Call(w.shield, swHide)
			}
			return 0, false
		}
		if w.overlay != 0 {
			_, _, _ = w32.User32ShowWindow.Call(w.overlay, swShowNA)
		}
		w.syncVideo()
	case w32.WMActivate:
		if w.overlay == 0 {
			break
		}
		// The low word is the state; the high word says whether it is minimised.
		switch wp & 0xffff {
		case w32.WAInactive:
			// The overlay taking over is not this window going to the
			// background; any other window taking over is.
			if w.ours(lp) {
				w.drawCaption(true)
			}
		case waClickActive:
			// A click on the frame. Handing activation away now is what
			// swallowed the caption buttons; hand it over once the click ends.
			_, _, _ = procSetTimer.Call(w.hwnd, refocusTimer, refocusInterval, 0)
			return 0, true
		default:
			// Alt-Tab and taskbar clicks land here, on the main window; the
			// keyboard belongs to the page in the overlay.
			w.activateOverlay()
			return 0, true
		}
	}
	return 0, false
}

// activateOverlay gives the overlay activation and the page the keyboard,
// keeping the frame drawn active across the change.
func (w *webview) activateOverlay() {
	w.handingOff = true
	_, _, _ = procSetActiveWindow.Call(w.overlay)
	w.handingOff = false
	w.browser.Focus()
	// Activation brings the overlay to the top of the owned windows, which
	// would bury a picture-in-picture film beneath the page it floats above.
	if w.layout == VideoPiP && w.video != 0 {
		origin := w32.Point{}
		_, _, _ = procClientToScreen.Call(w.hwnd, uintptr(unsafe.Pointer(&origin)))
		w.raisePiP(origin)
	}
}

/*
 * raisePiP floats the film above the page overlay at the docked box.
 *
 * HWND_TOP among the main window's owned windows. Owned windows stack above
 * their owner already, so this only decides the order among the overlay, the
 * game and the film: the film last, on top. The shield is not used here; a
 * click on the film over a game does nothing rather than opening the full
 * player out from under the game.
 */
func (w *webview) raisePiP(origin w32.Point) {
	const hwndTop = 0
	_, _, _ = w32.User32SetWindowPos.Call(w.video, hwndTop,
		uintptr(origin.X+w.mini.x), uintptr(origin.Y+w.mini.y), uintptr(w.mini.w), uintptr(w.mini.h),
		swpNoActivate|swpShowWindow)
}
