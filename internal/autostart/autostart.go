// Package autostart turns "open when Windows starts" on and off.
//
// It is the per-user run key, not a service and not a scheduled task. That is
// the right mechanism for this specific thing: the client is one person's app on
// one machine, it starts at *login* rather than at boot, and it needs no
// elevation to set. The server's own auto-start is a separate mechanism with a
// separate lifetime — the installed service, machine-wide, delayed-auto-start —
// and the two are deliberately independent (docs/desktop-lifecycle-plan.md).
//
// The failure this is written to avoid: a run-key entry pointing at an
// executable that no longer exists is a login-time error dialog every single
// morning, forever, with nothing obvious to blame. So Enable always writes the
// current executable path, Disable is safe to call when nothing is set, and the
// uninstaller clears it.
package autostart

// Name is the run-key value name. It is also what the uninstaller deletes, so
// the two must not drift — hence one exported constant rather than a string in
// two places.
const Name = "LANcast"

// Enabled reports whether LANcast is set to start at login.
func Enabled() (bool, error) { return enabled() }

// Enable sets LANcast to start at login, pointing at the running executable.
//
// Always rewrites rather than checking first: an entry left by an older install
// at a path that no longer exists is worse than no entry, and rewriting is how a
// moved or reinstalled LANcast repairs itself.
func Enable(args ...string) error { return enable(args...) }

// Disable removes the entry. Absent is success — the caller asked for it not to
// be there, and it is not there.
func Disable() error { return disable() }
