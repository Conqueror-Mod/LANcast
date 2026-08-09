//go:build !windows

package main

import "lancast/internal/clientwindow"

// runLauncherTray on non-Windows has no system tray — the launcher is a
// desktop convenience, primarily Windows (ADR 0022). The browser is already
// open; if this launcher started the server, block so it lives as long as the
// launcher, and stop it on exit. If the server was already running, return.
func runLauncherTray(l *launcher) {
	if l.started == nil {
		return
	}
	defer l.stopStartedServer()
	_ = l.started.Wait()
}

// runWindowTray is a no-op where there is no window to sit beside. The window
// itself is Windows-only (clientwindow returns ErrUnsupported elsewhere), so
// this exists to keep the caller free of build tags rather than to do anything:
// the close-to-tray preference is read on every platform and simply never has a
// tray to honour it here.
func runWindowTray(c clientwindow.Controller, stopping *bool) {}
