//go:build !windows

package main

import "log/slog"

// hasTray is false: outside Windows the client has no notification-area icon
// (ADR 0022), so the window's ✕ closes it.
const hasTray = false

func startDesktopTray(*desk, *slog.Logger) (stop func()) { return func() {} }

// waitForExit has no tray to wait on. The browser is already open; if this
// launch started the server, it lives as long as the launch, and stops with
// it. If the server was already running, there is nothing to wait for.
func waitForExit(_ *desk, l *launcher) {
	if l.started == nil {
		return
	}
	_ = l.started.Wait()
}
