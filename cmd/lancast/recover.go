package main

import (
	"time"

	"lancast/internal/clientwindow"
	"lancast/internal/desktop"
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
	for {
		time.Sleep(recoveryPoll)

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
					alert("LANcast", "The server came back with a new certificate, "+
						"which this window cannot trust: it pinned the old key when "+
						"it opened, and that cannot be changed while it is running.\n\n"+
						"Close LANcast and open it again.")
					return
				}
			}
			c.Navigate(url)
		}
		up = now
	}
}
