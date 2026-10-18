package main

import (
	"log/slog"
	"time"

	"lancast/internal/clientwindow"
	"lancast/internal/desktop"
	"lancast/internal/service"
)

/*
 * Bringing the window back when the server returns.
 *
 * Nothing in the web view retries. A window handed a URL that does not answer
 * sits on its own background colour for ever, and there are two ordinary ways
 * to get one: a client that opened while the service was still starting, and a
 * server that restarted underneath a window that was already open — which is
 * exactly what an in-app update does.
 *
 * A NavigationCompleted handler would be the direct way to notice a failed
 * load, and internal/webview2 has no COM event plumbing at all. Hand-writing a
 * callback vtable for one signal is a great deal of delicate code; polling the
 * health endpoint from out here answers the same question and can be read by
 * anybody.
 */

// recoveryPoll is how often to ask whether the server is back. Slow enough to
// be invisible, fast enough that nobody is left looking at an empty window
// wondering whether to restart the app.
const recoveryPoll = 2 * time.Second

/*
 * watchForRecovery reloads the window when the server becomes reachable again.
 *
 * Only on the edge — not while the server is up — because a reload of a page
 * somebody is reading is a worse bug than the one being fixed. Mid-film,
 * mid-search, mid-anything: a window that refreshes itself for no visible
 * reason is the sort of thing people describe as the app "doing something
 * weird", and they would be right.
 */
func (l *launcher) watchForRecovery(c clientwindow.Controller, url, pinAtLaunch string) {
	up := desktop.ServerRunning(l.addr)
	moved := false
	for {
		time.Sleep(recoveryPoll)

		/*
		 * The local service moved to another port (ADR 0089).
		 *
		 * Its old address will never answer again, so the edge below would
		 * never fire and the window would stay blank until the app was
		 * reopened. Following the service's own command line keeps watching
		 * the right place; the page itself also moves to the new port when
		 * it was the one that asked.
		 */
		if l.target.local {
			if a, ok := service.ConfiguredAddr(); ok && a != l.addr {
				slog.Info("the server's port changed; following it", "from", l.addr, "to", a)
				l.addr, l.localAddr = a, a
				up, moved = false, true
			}
		}

		now := desktop.ServerRunning(l.addr)
		if now && !up {
			/*
			 * A certificate the window cannot trust is not fixed by reloading.
			 *
			 * The pin is a Chromium command-line switch, fixed when this
			 * process created the web view environment, so a server that came
			 * back with a different key needs a new window rather than a new
			 * navigation. Saying so beats what happens otherwise: a security
			 * warning inside a native window, and a blank page after clicking
			 * through it, with nothing anywhere connecting the two.
			 */
			if pinAtLaunch != "" {
				if pin := l.serverCertPin(); pin != "" && pin != pinAtLaunch {
					slog.Info("the server came back with a different certificate; " +
						"this window pinned the old key and cannot be reloaded into it")
					alert("LANcast", "The server came back with a new certificate, "+
						"which this window cannot trust: it pinned the old key when "+
						"it opened, and that cannot be changed while it is running.\n\n"+
						"Close LANcast and open it again.")
					return
				}
			}
			/*
			 * Every navigation this loop performs is recorded.
			 *
			 * A window that reloads itself is indistinguishable, from the
			 * outside, from a window that has gone blank on its own — and one
			 * blank window has already cost an evening with this loop as the
			 * leading suspect and no way to confirm it. A line here settles
			 * that question in one reading instead.
			 */
			if moved {
				// Asked now rather than when the move was noticed: then the
				// server was still restarting, and nothing answered to say
				// whether it speaks https.
				url, moved = l.windowURL(l.addr), false
			}
			slog.Info("the server is reachable again; reloading the window", "url", url)
			c.Navigate(url)
		}
		up = now
	}
}
