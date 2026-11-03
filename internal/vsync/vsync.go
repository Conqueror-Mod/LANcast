// Package vsync waits for one monitor's vertical blank.
//
// The page's frame clock is not the screen's. Chromium ticks
// requestAnimationFrame at one rate for the whole desktop, and with displays
// at different rates that is not the rate of the one the window is on: a TV
// at 30 Hz beside a 72 Hz monitor was shown 2, then 3, then 2 of the page's
// frames per picture, so a visualizer's motion lurched by 28 ms, then 42 ms,
// on every picture (ADR 0113). Only the native side can see the TV's own
// refresh, so it waits for it here and tells the page.
//
// Deliberately one call wide: open a monitor by its GDI name, wait, close.
// Which monitor, how often to look again, and what to tell the page are the
// caller's (cmd/lancast/displayclock.go).
package vsync

import "errors"

// ErrUnsupported is Open's answer where there is no way to wait for a blank.
var ErrUnsupported = errors.New("vsync: not supported on this platform")
