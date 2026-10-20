package main

import (
	"encoding/json"
	"strings"
	"sync"

	"lancast/internal/clientwindow"
	"lancast/internal/desktop"
)

/*
 * desk is what the tray and the window share (ADR 0080).
 *
 * They live in one process now, on two threads: the window on main, the tray
 * on a thread of its own (991875f8 proved the arrangement). Everything the
 * tray asks of the window goes through here, because the window may not exist
 * yet — a launch at login starts with the tray alone and creates the window
 * the first time somebody opens it — and may already be on its way out.
 *
 * It replaces the cross-process coordination the two executables needed:
 * whether a tray was alive to bring a hidden window back, and a named event
 * for one process to tell another's window to quit. Both questions have one
 * answer in one process.
 */
type desk struct {
	mu       sync.Mutex
	win      clientwindow.Controller
	quitting bool
	// pending is where to go once the window exists, when the tray asked for
	// a destination before there was a window to send there.
	pending string
	// browser is true when the UI is the default browser rather than the
	// window: -browser, or a machine where the window cannot be shown. The
	// tray's Open then opens the browser.
	browser bool
	base    string

	openReq  chan struct{}
	exitReq  chan struct{}
	exitOnce sync.Once

	// onScanning, set by the tray, enables or greys its Cancel item. The page
	// is the only thing that knows whether a scan is running, so it reports
	// through a binding (trayBindings) and this passes it on.
	onScanning func(bool)
}

func newDesk(base string, browser bool) *desk {
	return &desk{base: base, browser: browser,
		openReq: make(chan struct{}, 1), exitReq: make(chan struct{})}
}

// useBrowser switches to the browser after the fact, for the paths that find
// out at window time that there will be no window.
func (d *desk) useBrowser() {
	d.mu.Lock()
	d.browser = true
	d.mu.Unlock()
}

/*
 * open shows LANcast, at url when one is given.
 *
 * With a window, it navigates and comes forward. With no window yet, it
 * records where to go and wakes the launch that is waiting to create one. In
 * browser mode it opens the browser, which is the only interface there is.
 */
func (d *desk) open(url string) {
	d.mu.Lock()
	browser, base, w := d.browser, d.base, d.win
	if !browser && w == nil && url != "" {
		d.pending = url
	}
	d.mu.Unlock()

	switch {
	case browser:
		if url == "" {
			url = base
		}
		_ = desktop.OpenBrowser(url)
	case w != nil:
		if url != "" {
			goInPage(w, base, url)
		}
		w.Show()
	default:
		select {
		case d.openReq <- struct{}{}:
		default: // already asked; one window is enough
		}
	}
}

/*
 * goInPage sends an open window somewhere without reloading it.
 *
 * Navigate is a full page load. Reported: with LANcast already open, the
 * tray's "Update libraries" reloaded the window rather than going to the
 * pane — whatever was on screen was thrown away, a film included, and the
 * whole client booted again just to change route. The page is a single-page
 * app with a router, so the window asks it to route (TrayBridge's
 * __lancastNavigate) and only falls back to loading the address when there is
 * no router to ask: the sign-in page, or a page still starting.
 *
 * A url outside this server's base is not a route, and is loaded as before.
 */
func goInPage(w clientwindow.Controller, base, url string) {
	if js, ok := inPageScript(base, url); ok {
		w.Eval(js)
		return
	}
	w.Navigate(url)
}

// inPageScript is goInPage's script, separate so it is tested without a window.
func inPageScript(base, url string) (string, bool) {
	path, ok := strings.CutPrefix(url, base)
	if !ok || !strings.HasPrefix(path, "/") {
		return "", false
	}
	// JSON is a JavaScript string literal, quotes and escapes included, so
	// nothing in a path can end the string early.
	p, _ := json.Marshal(path)
	u, _ := json.Marshal(url)
	return `(function(p,u){if(window.__lancastNavigate){window.__lancastNavigate(p)}else{location.assign(u)}})(` +
		string(p) + `,` + string(u) + `)`, true
}

// setScanning is the page telling the tray whether a scan is running.
func (d *desk) setScanning(on bool) {
	d.mu.Lock()
	fn := d.onScanning
	d.mu.Unlock()
	if fn != nil {
		fn(on)
	}
}

/*
 * cancelScan asks the page to stop every running scan.
 *
 * Without showing the window: the Cancel item is only enabled while the page
 * has reported a scan, so the page exists and is signed in, and it can make
 * the call itself (ADR 0080 §3) — no window needs to come forward to cancel
 * something. A fixed action name is the whole message; the page owns what it
 * means.
 */
func (d *desk) cancelScan() {
	d.mu.Lock()
	w := d.win
	d.mu.Unlock()
	if w != nil {
		w.Eval(`window.__lancastTray && window.__lancastTray("cancel-scan")`)
	}
}

// trayBindings are the page's half of the tray: how it reports scanning.
func trayBindings(d *desk) map[string]any {
	return map[string]any{
		"lancastTrayScanning": func(on bool) { d.setScanning(on) },
	}
}

// attach records the window once it exists, and sends it wherever the tray
// asked to go while it was being created.
func (d *desk) attach(c clientwindow.Controller) {
	d.mu.Lock()
	d.win = c
	pending := d.pending
	d.pending = ""
	d.mu.Unlock()
	if pending != "" {
		c.Navigate(pending)
	}
}

/*
 * exit ends LANcast on this desktop: the window, then the tray.
 *
 * It never touches a server this process did not start — the caller's normal
 * shutdown stops one it did, and a service carries on.
 */
func (d *desk) exit() {
	d.mu.Lock()
	d.quitting = true
	w := d.win
	d.mu.Unlock()
	if w != nil {
		w.Close()
	}
	d.exitOnce.Do(func() { close(d.exitReq) })
}

/*
 * closing answers the window's ✕.
 *
 * With a tray it hides: the tray is in this process, so it exists for exactly
 * as long as the hidden window does, and the question the old Close to tray
 * setting had to ask — is there anything to bring it back? — always has the
 * answer yes. Exit is the way to close. Where there is no tray (not Windows),
 * ✕ closes, because a hidden window with nothing to restore it is a process
 * with no way in.
 */
func (d *desk) closing() bool {
	d.mu.Lock()
	quitting, w := d.quitting, d.win
	d.mu.Unlock()
	if quitting || !hasTray || w == nil {
		return true
	}
	w.Hide()
	// A film does not play on in a hidden window; the page stops it and keeps
	// its place, and leaves music playing (TrayBridge.tsx).
	w.Eval(`window.__lancastTray && window.__lancastTray("window-hidden")`)
	return false
}
