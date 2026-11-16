//go:build !windows

package dataperm

import (
	"fmt"
	"os"
	"path/filepath"
)

/*
 * Protect makes the data directory its owner's alone: mode 0700.
 *
 * It was a no-op off Windows, on the belief that a package creates
 * /var/lib/lancast with the service account's mode. Nothing does: `service
 * install` writes the unit and the server creates the directory itself, 0755,
 * and SQLite creates lancast.db, -wal and -shm 0644 inside it. The Linux amd64
 * run found the database world-readable (docs/review/linux-amd64-run.md), and
 * secrets.key, the identity key and every account's password hash and sessions
 * sit beside it.
 *
 * The directory's mode is the whole fix: a file whose folder another account
 * cannot enter cannot be read by that account, whatever the file's own mode.
 * There is no desktop app on Linux that needs to look inside, so nothing is
 * shared back the way ShareCert does on Windows. Applied by `service install`
 * and on every start, so an install from before this is closed at its next
 * start.
 */
func Protect(dir string) (Report, error) {
	var r Report
	if !filepath.IsAbs(dir) {
		return r, fmt.Errorf("%q is not an absolute path", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return r, fmt.Errorf("create %s: %w", dir, err)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return r, err
	}
	if fi.Mode().Perm()&0o077 == 0 {
		return r, nil
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return r, fmt.Errorf("restrict %s: %w", dir, err)
	}
	r.Applied = true
	return r, nil
}

// ShareCert does nothing off Windows: no unelevated app reads the certificate
// out of the data directory there.
func ShareCert(string) error { return nil }

// protectedSummary is what LogReport says was done.
const protectedSummary = "data directory protected: its owner only (mode 0700)"
