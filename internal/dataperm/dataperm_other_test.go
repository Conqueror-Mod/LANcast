//go:build !windows

package dataperm

import (
	"os"
	"path/filepath"
	"testing"
)

/*
 * Off Windows the data directory is closed to every other account (found by
 * the Linux amd64 run: lancast.db was 0644 in a 0755 directory, readable by any
 * local user). The directory's mode is what closes it, so a database SQLite
 * created 0644 inside is still unreadable to anyone else.
 */
func TestProtectClosesAnOpenDataDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lancast")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	r, err := Protect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Applied {
		t.Error("an open directory was reported as already protected")
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o700 {
		t.Errorf("mode = %o, want 700", got)
	}

	// Already right: left alone, and said so.
	if r, err := Protect(dir); err != nil || r.Applied {
		t.Errorf("second Protect = %+v, %v; want nothing applied", r, err)
	}
}

// A directory that does not exist yet is created closed.
func TestProtectCreatesTheDirectoryClosed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "lancast")
	if _, err := Protect(dir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got&0o077 != 0 {
		t.Errorf("mode = %o, want nothing for group or others", got)
	}
}

func TestProtectRefusesARelativePath(t *testing.T) {
	if _, err := Protect("lancast"); err == nil {
		t.Error("a relative path was accepted")
	}
}
