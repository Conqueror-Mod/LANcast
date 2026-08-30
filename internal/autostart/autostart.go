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

/*
 * Two things can start at login, and they are not the same thing.
 *
 * The client window and the server's tray icon are separate programs with
 * separate reasons to be there, and both offer a "start at login" toggle. They
 * used to write **one** run-key value, whichever asked last, because the name
 * was a single constant and the path came from whichever process was calling.
 *
 * The result was two switches wired to one wire. Turning it on in the tray
 * replaced the client's entry with the tray's; turning it off anywhere cleared
 * whatever the other had meant. Both checkboxes then read the same value and
 * both showed "on", describing different programs.
 *
 * Found on a real install as a preferences file saying `open_at_login: true`
 * with no run-key entry at all.
 *
 * So each target owns a value name, and neither can reach the other's.
 */
type Target struct {
	// value is the run-key value name this target owns.
	value string
	// own is this target's executable, used to recognise a legacy entry that
	// was this target's doing under the old shared name.
	own string
	/*
	 * foreign is the *other* target's executable.
	 *
	 * Enabled asks "is this entry the other program's?" rather than "is it
	 * exactly mine?", and the difference matters: the path legitimately varies
	 * — a moved install, a build run from a terminal, a test binary — so
	 * demanding an exact executable would report a perfectly good entry as
	 * absent. What must never be reported as *this* target starting at login is
	 * an entry that plainly starts the other one.
	 */
	foreign string
}

var (
	// Client is the desktop window. It keeps the original value name, because
	// it is the common case and an existing entry pointing at the client should
	// go on working across this change.
	Client = Target{value: "LANcast", own: "LANcast-Client.exe", foreign: "LANcast-Server.exe"}
	// Tray is the server's notification-area icon.
	Tray = Target{value: "LANcast Tray", own: "LANcast-Server.exe", foreign: "LANcast-Client.exe"}
)

// Names lists every run-key value LANcast may have written, for the
// uninstaller. One list rather than a string in two places: an installer that
// forgets one leaves a login-time entry pointing at a deleted executable, which
// is an error dialog every morning with nothing obvious to blame.
func Names() []string { return []string{Client.value, Tray.value} }

// Enabled reports whether this target is set to start at login.
func Enabled(t Target) (bool, error) { return enabled(t) }

// Enable sets this target to start at login, pointing at the running
// executable.
//
// Always rewrites rather than checking first: an entry left by an older install
// at a path that no longer exists is worse than no entry, and rewriting is how a
// moved or reinstalled LANcast repairs itself.
func Enable(t Target, args ...string) error { return enable(t, args...) }

// Disable removes this target's entry. Absent is success — the caller asked for
// it not to be there, and it is not there.
func Disable(t Target) error { return disable(t) }
