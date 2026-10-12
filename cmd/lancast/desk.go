package main

import (
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
			w.Navigate(url)
		}
		w.Show()
	default:
		select {
		case d.openReq <- struct{}{}:
		default: // already asked; one window is enough
		}
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
	return false
}
