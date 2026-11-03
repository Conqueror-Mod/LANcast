//go:build !windows

package main

import "lancast/internal/clientwindow"

// displayClockBindings: no screen clock here; the page keeps its own.
func displayClockBindings(func() clientwindow.Controller) map[string]any { return nil }
