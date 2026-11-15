//go:build windows

package faces

import (
	"context"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"lancast/internal/mediatools"
)

// The real guard: a worker beside a binary in a folder this unelevated test
// account owns is the shape of a planted one, and the service must refuse it
// and say so.
func TestTheServiceGuardRefusesAWorkerAnOrdinaryAccountOwns(t *testing.T) {
	if windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("elevated, the files this test writes are owned by Administrators")
	}
	besideTheServer(t)
	svc := &Tool{Guard: mediatools.ServiceGuard()}
	if p, ok := svc.Path(); ok {
		t.Fatalf("the service would run %s", p)
	}
	c := svc.probe(context.Background())
	if !strings.Contains(c.Reason, "refused") {
		t.Errorf("reason = %q", c.Reason)
	}
	t.Logf("reason: %s", c.Reason)
}
