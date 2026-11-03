//go:build windows

package main

import (
	"time"

	"lancast/internal/clientwindow"
	"lancast/internal/displaymode"
	"lancast/internal/vsync"
)

// displayClockBindings are the screen-clock functions (displayclock.go).
func displayClockBindings(win func() clientwindow.Controller) map[string]any {
	c := &displayClock{
		win:     win,
		monitor: displaymode.MonitorOf,
		rate:    clientwindow.RefreshRate,
		open:    func(d string) (blankWaiter, error) { return vsync.Open(d) },
		now:     time.Now,
	}
	return c.bindings()
}
