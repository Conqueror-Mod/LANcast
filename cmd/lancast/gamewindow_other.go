//go:build !windows

package main

import "lancast/internal/games"

// No displays, and nothing to move: the window this belongs to is Windows-only.
//
// Present so the package still builds on CI's Linux, which is what keeps the
// rules in internal/games — the geometry, the labels, the remembered answers —
// under test on every platform rather than only on the one desk that has three
// monitors.
func availableDisplays() []games.Display { return nil }

func moveGameToDisplay(device, name string) {}
