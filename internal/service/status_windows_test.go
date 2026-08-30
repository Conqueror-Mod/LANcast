//go:build windows

package service

import (
	"strings"
	"testing"
)

/*
 * Reading a service's state must not need an administrator.
 *
 * `mgr.Connect` asks for `SC_MANAGER_ALL_ACCESS`, which the service control
 * manager grants to administrators only. Installing, starting and deleting are
 * administrator acts and paying a consent prompt for them is honest. Reading a
 * state is not, and routing it through the same door made Status fail with
 * "Access is denied" on every unelevated launch.
 *
 * The consequence was invisible and total: `installedService()` reads Status and
 * treats an error as *not installed*, so an ordinary double-click concluded
 * there was no service, fell through to the "somebody else holds the name"
 * branch, opened a browser and exited. The whole service-aware launch path was
 * dead for every non-elevated user — which is every ordinary one — and it was
 * found only because a tray icon that should have appeared did not.
 */

func TestStatusDoesNotNeedElevation(t *testing.T) {
	m, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	st, err := m.Status()

	/*
	 * Two outcomes are correct here and "access is denied" is neither.
	 *
	 * A machine with the service installed answers a state; one without answers
	 * that it is not installed. This test runs on both — CI has no LANcast
	 * service — so it asserts on the *kind* of failure rather than requiring a
	 * particular machine.
	 */
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "access is denied") {
			t.Errorf("reading the service state demanded elevation: %v", err)
		}
		if !strings.Contains(err.Error(), "not installed") {
			t.Logf("status unavailable for another reason, which is allowed: %v", err)
		}
		return
	}
	if st == "" {
		t.Error("a successful status returned no state")
	}
}

/*
 * The states the launcher branches on have to survive being renamed.
 *
 * `installedService` calls a service running when the string is "running" or
 * "start pending", and a typo in either would make a running service look
 * stopped — which prompts to start something that is already going.
 */
func TestTheStateNamesTheLauncherReadsAreStable(t *testing.T) {
	for _, want := range []string{"running", "start pending", "stopped"} {
		found := false
		for _, s := range []string{
			svcStateName(1), svcStateName(2), svcStateName(3),
			svcStateName(4), svcStateName(5), svcStateName(6), svcStateName(7),
		} {
			if s == want {
				found = true
			}
		}
		if !found {
			t.Errorf("no service state maps to %q, which the launcher branches on", want)
		}
	}
}
