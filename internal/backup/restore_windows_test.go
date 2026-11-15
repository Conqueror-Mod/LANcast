//go:build windows

package backup

import (
	"context"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"lancast/internal/mediatools"
)

/*
 * The real guard on real files. A test running unelevated is the planting
 * account of R10-2: the marker and the backup it writes are owned by it, not
 * by SYSTEM or Administrators, and the service must not act on them. Elevated
 * (as an administrator runs it) the files are Administrators', which is the
 * owner a restore made from an elevated shell has, so the test is skipped.
 */
func TestTheServiceGuardRefusesARestoreAnOrdinaryAccountStaged(t *testing.T) {
	if windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("elevated, the files this test writes are owned by Administrators")
	}
	d, db, _ := stagedRestore(t)
	out := d.WithGuard(mediatools.ServiceGuard()).ApplyPending(context.Background(), db, time.Now())
	assertNotRestored(t, d, db, out)
	t.Logf("refused: %s", out.Error)
}
