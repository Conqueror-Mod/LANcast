package contain

import (
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
)

// junction makes a Windows directory junction, which needs no privilege.
func junction(t *testing.T, target, link string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("cannot create a junction here: %v: %s", err, out)
	}
}

/*
 * Measured, not assumed: since Go 1.23 filepath.EvalSymlinks fails with "path
 * not found" for every path beneath a junction. A library under a junctioned
 * folder is an ordinary Windows setup, so its plain files must still be
 * contained — and a link found under one must still be judged by where it
 * points, not refused as dangling or waved through by name.
 */
func TestWithinBeneathAJunction(t *testing.T) {
	lib, secret := layout(t)
	via := filepath.Join(t.TempDir(), "via")
	junction(t, lib, via)

	if _, err := Within(via, filepath.Join(via, "Films", "real.mkv")); err != nil {
		t.Fatalf("plain file under a junctioned root: %v, want nil", err)
	}

	link := filepath.Join(lib, "Films", "film.mkv")
	symlink(t, secret, link)
	if _, err := Within(via, filepath.Join(via, "Films", "film.mkv")); !errors.Is(err, ErrOutside) {
		t.Fatalf("link out, under a junctioned root: %v, want ErrOutside", err)
	}

	inside := filepath.Join(lib, "Films", "again.mkv")
	symlink(t, filepath.Join(lib, "Films", "real.mkv"), inside)
	if _, err := Within(via, filepath.Join(via, "Films", "again.mkv")); err != nil {
		t.Fatalf("link in, under a junctioned root: %v, want nil", err)
	}
}
