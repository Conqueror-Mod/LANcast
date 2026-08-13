//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"

	"lancast/internal/service"
)

/*
 * Starting the installed service from a double-click.
 *
 * The hole this closes: with LANcast installed as a service and stopped,
 * double-clicking LANcast-Server.exe used to start its *own* server as the
 * logged-in user, against the machine-wide data directory the service account
 * owns — which fails with `attempt to write a readonly database (8)`, an error
 * whose text explains nothing to anyone who has not read the SQLite source. The
 * only way through was an elevated `Start-Service`, which is a terminal, which
 * is exactly what a double-click is supposed to avoid.
 *
 * WHY ELEVATION IS ASKED FOR HERE AND NOWHERE ELSE
 *
 * Starting a service requires rights a normal user session does not have, and
 * there is no way around that — it is the operating system's boundary, not
 * LANcast's. So it is asked for the ordinary Windows way: ShellExecute with the
 * `runas` verb, which is the UAC prompt every installer uses. One prompt, one
 * click.
 *
 * This is the *only* thing LANcast will elevate for. A media server that
 * silently acquires administrator rights to do its ordinary work is a different
 * and much worse product; the elevated child here runs `service start` and
 * exits.
 */

// serviceState is what a launch needs to know about the installed service:
// whether there is one, and whether it is already up.
type serviceState struct {
	Installed bool
	Running   bool
	// Status is the raw state name for a diagnostic, "" when unknown.
	Status string
}

// installedService inspects the service without needing elevation.
//
// Querying a service is permitted to an ordinary user; only controlling one is
// not. An error is read as "not installed" rather than propagated: a launch that
// cannot ask is a launch that should behave like a machine with no service,
// which is the pre-existing behaviour and is never wrong in a damaging way.
func installedService() serviceState {
	m, err := service.NewManager()
	if err != nil {
		return serviceState{}
	}
	st, err := m.Status()
	if err != nil {
		return serviceState{}
	}
	return serviceState{
		Installed: true,
		Running:   st == "running" || st == "start pending",
		Status:    st,
	}
}

// startInstalledService starts the service, elevating only if it has to.
//
// Tried unelevated first, because a machine where the user already has the
// right — an administrator with UAC off, a service whose ACL was widened
// deliberately — should not be shown a consent prompt for no reason.
func startInstalledService() error {
	m, err := service.NewManager()
	if err != nil {
		return err
	}
	if err := m.Start(); err == nil {
		return nil
	} else if !accessDenied(err) {
		// A real failure — a missing binary, a bad service configuration. A UAC
		// prompt would not fix it and asking for one would blame the user for
		// something that is not about permission.
		return err
	}
	return elevateSelf("service", "start")
}

// accessDenied reports whether an error is Windows refusing on rights grounds.
func accessDenied(err error) bool {
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return true
	}
	// The manager wraps its errors with context, and the wrapped value is not
	// always a syscall.Errno by the time it arrives here.
	return strings.Contains(strings.ToLower(err.Error()), "access is denied")
}

// elevateSelf re-runs this executable with the given arguments, elevated.
//
// The child is not waited on: ShellExecute returns as soon as the prompt is
// answered, and what the caller actually wants to know is whether the server
// came up — which it learns by watching the port, not by watching a process it
// cannot see the exit code of.
//
// A refused prompt is reported as such rather than as a failure to start. "You
// said no" and "it did not work" are different sentences and the user knows
// which one they caused.
func elevateSelf(args ...string) error {
	exe, err := windows.UTF16PtrFromString(mustExe())
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	params, err := windows.UTF16PtrFromString(strings.Join(args, " "))
	if err != nil {
		return err
	}

	err = windows.ShellExecute(0, verb, exe, params, nil, windows.SW_HIDE)
	if err == nil {
		return nil
	}
	if errors.Is(err, windows.ERROR_CANCELLED) {
		return errCancelled
	}
	return fmt.Errorf("ask for permission to start the LANcast service: %w", err)
}

// errCancelled is a refused UAC prompt, which is a decision rather than a fault.
var errCancelled = errors.New("permission was not granted, so the LANcast service was not started")

// mustExe is this executable's path, or the empty string.
//
// Empty rather than an error because the only caller is asking Windows to run
// us again: an empty path fails the ShellExecute with a message about the path,
// which is the same information one level closer to where it is useful.
func mustExe() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}
