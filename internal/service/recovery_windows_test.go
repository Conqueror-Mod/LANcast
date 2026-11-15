//go:build windows

package service

import (
	"os"
	"testing"

	"golang.org/x/sys/windows/svc/mgr"
)

/*
 * The recovery policy must include the failure-actions flag (R10-6).
 *
 * Without it Windows restarts the service only when its process dies without
 * reporting STOPPED. A start that fails reports STOPPED with exit code 1, which
 * is a non-crash failure, and nothing happened: the server stayed down.
 *
 * Registering a service needs an administrator, so this runs only in an
 * elevated shell and skips everywhere else, CI included. It registers a
 * throwaway service under its own name, never starts it, and deletes it. The
 * real service is checked on the machine with `sc qfailureflag lancastd`.
 */
func TestRecoveryActionsIncludeNonCrashFailures(t *testing.T) {
	m, err := mgr.Connect()
	if err != nil {
		t.Skipf("needs an elevated shell to register a service: %v", err)
	}
	defer m.Disconnect()

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const name = "lancast-test-recovery"
	if old, err := m.OpenService(name); err == nil { // a leftover from a killed run
		old.Delete()
		old.Close()
	}
	s, err := m.CreateService(name, exe, mgr.Config{StartType: mgr.StartManual})
	if err != nil {
		t.Skipf("cannot register a test service: %v", err)
	}
	defer func() {
		s.Delete()
		s.Close()
	}()

	if err := setRecoveryActions(s); err != nil {
		t.Fatalf("setRecoveryActions: %v", err)
	}
	flag, err := s.RecoveryActionsOnNonCrashFailures()
	if err != nil {
		t.Fatal(err)
	}
	if !flag {
		t.Error("FAILURE_ACTIONS_ON_NONCRASH_FAILURES is FALSE: a failed start will never be retried")
	}
	actions, err := s.RecoveryActions()
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 3 {
		t.Errorf("%d recovery actions, want 3", len(actions))
	}
	for i, a := range actions {
		if a.Type != mgr.ServiceRestart {
			t.Errorf("action %d is type %d, want a restart", i, a.Type)
		}
	}
}
