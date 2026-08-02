// Package singleton enforces "only one of me at a time" for the LANcast
// executables: one server, one client, no duplicate processes.
//
// The mechanism is per-platform (a named mutex on Windows, an advisory file lock
// elsewhere) but the contract is the same: Acquire either takes the name or
// reports that someone else already holds it. The caller decides what to do about
// it — the LANcast exes open the UI and exit rather than starting a second copy.
package singleton

// Names are the identities the two executables lock on. They are distinct, so a
// running server never blocks the client or vice versa.
const (
	Server = "LANcast-Server"
	Client = "LANcast-Client"
)

// Release frees a held name. Calling it when the name was not held is safe.
type Release func()

// Acquire takes the named lock. held is false when another process already owns
// it — the caller should not start a second instance. The returned Release is
// non-nil in both cases so a deferred call is always safe.
//
// An error means the lock could not be evaluated at all; a caller that cannot
// tell should proceed rather than refuse to start over a broken guard.
func Acquire(name string) (release Release, held bool, err error) {
	return acquire(name)
}
