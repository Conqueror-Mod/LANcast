package main

import (
	"fmt"
	"os"
	"os/exec"

	"lancast/internal/childproc"
	"lancast/internal/desktopprefs"
)

/*
 * Switching servers means a new process, because a window's pin is fixed for
 * its lifetime. connect.go has the reasoning; this is the mechanics.
 *
 * The choice is written to disk *before* the new process starts, and nothing
 * is passed on the command line about which server to open. That is
 * deliberate: the preference is the single statement of where this client
 * points, so a relaunch that is interrupted -- the new process failing to
 * start, the machine losing power between the two -- leaves the client
 * pointing at the server the person chose rather than at whichever one the
 * argument list happened to name. The next launch, by any route, agrees with
 * the one this would have produced.
 */

// switchTo records a new server choice and restarts the client onto it.
//
// The caller closes the window after this returns; the single-instance lock is
// released on the way out, and the new process acquires it. That ordering is
// why this starts the successor and returns rather than replacing the process
// in place: Windows has no exec, and a second client holding nothing would
// race the first one's lock.
func switchTo(address string) error {
	dir := clientDataDir()
	/*
	 * Re-read before writing, for the reason OnPlacement re-reads: the
	 * settings page writes this same file through a binding while the window
	 * is open, so the copy loaded at startup may be stale and writing it back
	 * would undo a tickbox somebody changed this session.
	 */
	prefs, err := desktopprefs.Load(dir)
	if err != nil {
		// A preferences file that will not parse is not a reason to refuse to
		// switch: the defaults plus this choice is a coherent state, and the
		// alternative is being stuck on a server with no way to leave it.
		prefs = desktopprefs.Prefs{}
	}
	prefs.Server = address
	if err := desktopprefs.Save(dir, prefs); err != nil {
		// Fatal *to the switch*, and it must be, because the new process reads
		// its destination from this file. Starting it now would relaunch onto
		// the server we are already on and look like nothing happened.
		return fmt.Errorf("remember the chosen server: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find this program: %w", err)
	}
	cmd := exec.Command(exe, relaunchArgs()...)
	childproc.Hide(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("restart LANcast: %w", err)
	}
	// Not waited on. It is a sibling, not a child that this process owns -- and
	// this process is about to exit, which is the whole point.
	_ = cmd.Process.Release()
	return nil
}

// relaunchArgs reproduces the launch mode and nothing about the destination.
//
// -addr is deliberately not carried across. It is the operator's instruction
// to open the server on *this* machine (see resolve), so passing it on would
// make every relaunch ignore the choice that caused it.
func relaunchArgs() []string {
	if browserMode {
		return []string{"-browser"}
	}
	return nil
}
