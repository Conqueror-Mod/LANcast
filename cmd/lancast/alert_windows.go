//go:build windows

package main

import "golang.org/x/sys/windows"

// alert shows a message box. The launcher is linked with -H=windowsgui, so
// stderr goes nowhere — without this a failure to find or start the server would
// be a double-click that silently does nothing.
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
