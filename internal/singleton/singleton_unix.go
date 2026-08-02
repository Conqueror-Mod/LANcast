//go:build !windows

package singleton

import (
	"os"
	"path/filepath"
	"syscall"
)

// acquire takes an exclusive advisory lock on a file in the temp dir. The lock is
// released when the process exits — including a crash — so a stale lock file
// never wedges the next start, which a plain "does the pid file exist" check
// would.
func acquire(name string) (Release, bool, error) {
	path := filepath.Join(os.TempDir(), name+".lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		// EWOULDBLOCK means someone else holds it — the guard working, not a
		// failure to evaluate it.
		if err == syscall.EWOULDBLOCK {
			return func() {}, false, nil
		}
		return func() {}, false, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, true, nil
}
