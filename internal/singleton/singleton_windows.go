//go:build windows

package singleton

import (
	"golang.org/x/sys/windows"
)

// acquire creates a named mutex. Windows keeps the name alive for as long as any
// handle to it is open, so a second process asking for the same name is told it
// already exists — that is the whole guard.
//
// It tries the Global namespace first so the check spans sessions: the server can
// run as a service in session 0 while a double-click happens in the user's
// session, and a session-local name would not see it. Creating a Global object
// needs a privilege that services and admins have but a standard user may not, so
// it falls back to the session-local name rather than failing.
func acquire(name string) (Release, bool, error) {
	if release, held, err := createMutex(`Global\` + name); err == nil {
		return release, held, nil
	}
	return createMutex(name)
}

func createMutex(name string) (Release, bool, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return func() {}, false, err
	}
	h, err := windows.CreateMutex(nil, false, p)
	// ERROR_ALREADY_EXISTS is returned alongside a valid handle: the mutex was
	// opened, not created, which means another process is holding the name.
	if err == windows.ERROR_ALREADY_EXISTS {
		if h != 0 {
			windows.CloseHandle(h)
		}
		return func() {}, false, nil
	}
	if err != nil {
		return func() {}, false, err
	}
	return func() { windows.CloseHandle(h) }, true, nil
}
