package scan

import (
	"os/exec"
	"testing"
)

// makeUnreadable denies everyone the right to list dir, which is the shape an
// ACL that refuses the service account takes. By SID rather than by name, so
// it does not depend on the machine's language. The deny is removed at
// cleanup, or t.TempDir could not delete the folder.
func makeUnreadable(t *testing.T, dir string) {
	t.Helper()
	if out, err := exec.Command("icacls", dir, "/deny", "*S-1-1-0:(RD)").CombinedOutput(); err != nil {
		t.Skipf("icacls could not deny listing: %v: %s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("icacls", dir, "/remove:d", "*S-1-1-0").Run()
	})
}
