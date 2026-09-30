//go:build windows

package main

import (
	"log/slog"

	"lancast/internal/clientwindow"
)

// Per-monitor DPI awareness has to be in place before the process creates any
// window, the tray's included, so it is set here: an init function runs before
// main and therefore before anything that could make one. See
// clientwindow/dpi_windows.go for why the client needs it at all.
//
// The error is held rather than logged here: init runs before startLogging,
// and a warning written then would go nowhere anybody reads.
func init() {
	dpiAwareErr = clientwindow.SetDPIAware()
}

var dpiAwareErr error

// logDPIAwareness reports a failure from init, once logging exists.
func logDPIAwareness() {
	if dpiAwareErr != nil {
		slog.Warn("could not make the window DPI aware; it will be stretched on scaled monitors",
			"error", dpiAwareErr)
	}
}
