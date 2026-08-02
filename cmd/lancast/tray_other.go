//go:build !windows

package main

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
