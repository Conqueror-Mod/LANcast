//go:build windows

package main

import "golang.org/x/sys/windows"

// processAlive reports whether a pid still names a running process.
//
// OpenProcess with SYNCHRONIZE is enough to answer the question and is the
// narrowest right the check can ask for — the helper does not need to read the
// old server's memory or terminate it, only to notice that it is gone.
//
// A pid that cannot be opened is treated as gone. The alternative reading — a
// process this account may not touch — cannot arise here: the helper is a child
// of the server it is waiting for, started by the same user.
//
// STILL_ACTIVE is checked rather than trusting the handle, because a handle to
// an exited process stays valid until it is closed. Without that check this
// would wait the full timeout every time and never restart anything.
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	const stillActive = 259
	return code == stillActive
}
