//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// attachParentProcess is AttachConsole's "use my caller's console" argument:
// DWORD(-1). Not exported by x/sys/windows, so it is spelled out here.
const attachParentProcess = ^uintptr(0)

// attachConsole reattaches this process's standard streams to the console that
// launched it.
//
// Release builds link with -H=windowsgui so a double-click shows no console
// window (ADR 0022). The side effect is that a build invoked *from* a terminal
// has no attached console either: os.Stdout and os.Stderr point at nothing, and
// a console subcommand prints into the void. `LANcast-Server.exe reset-auth`
// would appear to do nothing at all — the same class of silent failure the
// tray's alert() exists to prevent.
//
// Borrowing the caller's console fails harmlessly when there is not one (a
// double-click, or the service control manager), so this is safe to call
// unconditionally from a console entry point.
func attachConsole() {
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	if err := proc.Find(); err != nil {
		return
	}
	if ret, _, _ := proc.Call(attachParentProcess); ret == 0 {
		// No parent console — nothing to attach to, and nothing to fix.
		return
	}
	// Attaching gives the process a console but does not repoint the file
	// descriptors Go already opened, so they have to be rebound by hand.
	for _, s := range []struct {
		std  uint32
		file **os.File
		name string
	}{
		{windows.STD_OUTPUT_HANDLE, &os.Stdout, "CONOUT$"},
		{windows.STD_ERROR_HANDLE, &os.Stderr, "CONERR$"},
	} {
		h, err := windows.GetStdHandle(s.std)
		if err != nil || h == 0 {
			continue
		}
		*s.file = os.NewFile(uintptr(h), s.name)
	}
}
