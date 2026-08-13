package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"lancast/internal/childproc"
)

/*
 * Finishing a staged update on an install that is not a service.
 *
 * The service path restarts through the service manager, which can stop and
 * start a thing that is not itself. Nothing else can: a process cannot outlive
 * its own exit to start its replacement. So the work is handed to a detached
 * copy of the same binary running in `relaunch` mode, which waits for the
 * server to disappear and then starts it again.
 *
 * WHY THIS IS SAFE ON WINDOWS
 *
 * The staged files are applied by the server on the way *down*
 * (applyStagedUpdate), so by the time the parent has exited the swap has
 * already happened and the helper is starting a binary that is the new version.
 * Renaming a running executable is permitted, which is what lets the helper
 * keep executing out of a file that is being replaced underneath it.
 *
 * WHY IT WAITS ON THE PID RATHER THAN SLEEPING
 *
 * A fixed sleep is a race with a shutdown whose length depends on how many
 * background workers have to stop, how big the library is, and whether a
 * transcode is draining. Too short and the new server cannot bind the port; too
 * long and the user is staring at nothing. Waiting for the process to actually
 * be gone is the only version of this that is correct on a slow machine and
 * fast on a quick one.
 */

// relaunchArg is the hidden subcommand. Hidden because it is never something a
// person types: it is a step in an update, and offering it in help would
// invite someone to run it with a pid that means nothing.
const relaunchArg = "relaunch"

// relaunchWait bounds the wait for the old process. Generous, because the thing
// being waited for is a graceful shutdown that stops workers and closes a
// database — and the cost of giving up early is two servers fighting for a
// port, which is worse than a slow update.
const relaunchWait = 90 * time.Second

// scheduleRelaunch spawns the helper that will restart this server.
//
// Called before the shutdown begins, so the helper is already waiting when the
// process goes away. It is deliberately not waited on: its first act is to
// outlive us.
func scheduleRelaunch(log *slog.Logger) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}

	// The arguments this server was started with, so the restart is the same
	// server: a tray launch comes back as a tray, a `-data X -addr Y` launch
	// comes back on the same directory and port. Reconstructing them from
	// config instead would silently "fix" a deliberately odd invocation.
	args := append([]string{relaunchArg, strconv.Itoa(os.Getpid())}, os.Args[1:]...)

	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	childproc.Hide(cmd)
	childproc.Detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start the relaunch helper: %w", err)
	}
	log.Info("relaunch helper started; the server will restart to finish the update",
		"helper", cmd.Process.Pid, "args", os.Args[1:])
	return nil
}

// runRelaunch is the helper: wait for the old server, then start the new one.
func runRelaunch(argv []string) error {
	if len(argv) < 1 {
		return errors.New("relaunch: no pid to wait for")
	}
	pid, err := strconv.Atoi(argv[0])
	if err != nil {
		return fmt.Errorf("relaunch: bad pid %q: %w", argv[0], err)
	}

	if err := waitForExit(pid, relaunchWait); err != nil {
		// Starting anyway would put two servers on one port and one database.
		// The staged update is still staged and the running server still works,
		// which is a worse update experience and not a broken install.
		return fmt.Errorf("relaunch: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("relaunch: locate executable: %w", err)
	}
	cmd := exec.Command(exe, argv[1:]...)
	cmd.Dir = filepath.Dir(exe)
	childproc.Hide(cmd)
	childproc.Detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("relaunch: start the server: %w", err)
	}
	return nil
}

// waitForExit blocks until the process is gone, or the timeout elapses.
//
// Polling rather than a handle wait, because this has to behave the same on
// every platform the server runs on and the cost is a signal check every 200ms
// for a few seconds.
func waitForExit(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			// A moment for the operating system to release the executable and
			// the listening socket. Without it the new server can lose a race
			// it did not know it was in, on exactly the machines that are
			// slowest to notice.
			time.Sleep(400 * time.Millisecond)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("the old server (pid %d) did not exit within %s", pid, timeout)
}
