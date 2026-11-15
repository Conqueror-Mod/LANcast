//go:build !windows

package scan

import (
	"os"
	"testing"
)

// makeUnreadable takes away every permission on dir, so listing it fails. The
// mode is put back at cleanup, or t.TempDir could not delete the folder.
func makeUnreadable(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0); err != nil {
		t.Skipf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}
