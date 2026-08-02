//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

// bareLaunchUsesTray: on Windows the binary is linked with -H=windowsgui, so a
// double-click has no console. Running the server foreground there would start an
// invisible process with no way to see it or stop it, so a bare launch shows the
// tray instead (ADR 0022).
const bareLaunchUsesTray = true

// alert shows a message box. In windowsgui mode stderr goes nowhere, so a fatal
// startup error would otherwise be completely silent — the user double-clicks and
// nothing happens at all.
func alert(title, msg string) {
	t, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	m, err := windows.UTF16PtrFromString(msg)
	if err != nil {
		return
	}
	windows.MessageBox(0, m, t, windows.MB_OK|windows.MB_ICONWARNING)
}
