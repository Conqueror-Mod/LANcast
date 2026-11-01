package main

import (
	"fmt"
	"sort"
	"strings"

	"lancast/internal/clientwindow"
)

/*
 * The desktop's layout, as far as theater output cares (docs/theater-output-plan.md).
 *
 * Two decisions hang on it, both learned on the first evening with the Roku.
 *
 * Pacing: Windows composes the player on the clock of another active
 * monitor whenever there is one, so pacing frames to the TV can only work
 * with the TV alone (resampleWorthTrying).
 *
 * Restoring: with White Chicks still holding the TV at LANcast's temporary
 * 23.976 Hz, disconnecting a desk monitor in Windows' settings turned the TV
 * off and rotated another screen. A layout that changes mid-film is
 * therefore the moment to put the TV back, not the end of the film
 * (layoutKey, watchLayout).
 */

// resampleWorthTrying reports whether the display-resample trial can win:
// only with one active monitor, since any other sets the pace.
func resampleWorthTrying(active []clientwindow.Monitor) bool { return len(active) == 1 }

// layoutKey names which monitors are on and where, in an order that does not
// depend on enumeration. A refresh-rate switch leaves it unchanged; adding,
// removing, moving or rotating a screen changes it.
func layoutKey(active []clientwindow.Monitor) string {
	parts := make([]string, 0, len(active))
	for _, m := range active {
		w := m.Work
		parts = append(parts, fmt.Sprintf("%s@%d,%d,%d,%d", m.Device, w.Left, w.Top, w.Right, w.Bottom))
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

// monitorList names the active monitors, for a log line.
func monitorList(active []clientwindow.Monitor) string {
	names := make([]string, 0, len(active))
	for _, m := range active {
		n := m.Name
		if n == "" {
			n = m.Device
		}
		names = append(names, n)
	}
	return strings.Join(names, ", ")
}

// uncomposedFix says what would let Windows show the film on device without
// composing it (ADR 0111), or "" when nothing needs changing. The driver on
// the theater PC gives overlay planes to the primary display only, so with
// the TV secondary every frame was composed on another monitor's clock, and
// with it primary, other monitors on, none was. LANcast does not move the
// primary display itself: that is the person's desktop.
func uncomposedFix(active []clientwindow.Monitor, device string) string {
	if len(active) < 2 {
		return ""
	}
	for _, m := range active {
		if m.Device != device {
			continue
		}
		if m.Primary {
			return ""
		}
		n := m.Name
		if n == "" {
			n = m.Device
		}
		return "make " + n + " the main display (Windows Settings, System, Display) for the smoothest picture"
	}
	return ""
}
