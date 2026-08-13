package main

import (
	"os/exec"
	"runtime"
	"testing"
	"time"
)

/*
 * The relaunch helper's wait.
 *
 * This is the part of finishing an update that is a race if it is written
 * carelessly: start the new server too early and it cannot bind the port the
 * old one still holds; give up too late and the user is watching nothing
 * happen. A fixed sleep is wrong on both ends, which is why the helper waits on
 * the process rather than on the clock — and why the waiting is tested rather
 * than assumed.
 */

func TestProcessAliveSeesARunningProcessAndAnExitedOne(t *testing.T) {
	cmd := sleeper(t, 30)
	pid := cmd.Process.Pid

	if !processAlive(pid) {
		t.Fatalf("pid %d is running and processAlive says otherwise", pid)
	}

	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()

	// The Windows implementation has to check the exit code, not merely whether
	// a handle opens: a handle to an exited process stays valid. Without that,
	// this waits the full timeout every time and never restarts anything.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("pid %d exited and processAlive still reports it running", pid)
}

func TestWaitForExitReturnsOnceTheProcessIsGone(t *testing.T) {
	cmd := sleeper(t, 30)
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	start := time.Now()
	if err := waitForExit(cmd.Process.Pid, 10*time.Second); err != nil {
		t.Fatalf("waitForExit: %v", err)
	}
	took := time.Since(start)
	// It must not return early — the point is that the old server is gone —
	// and it must not sit on a fixed sleep either.
	if took < 300*time.Millisecond {
		t.Errorf("returned after %s, before the process had exited", took)
	}
	if took > 5*time.Second {
		t.Errorf("took %s to notice an exit after 300ms", took)
	}
}

// A server that will not stop must not be started over. Two servers on one port
// and one database is a worse outcome than an update that stayed staged.
func TestWaitForExitGivesUpRatherThanStartingASecondServer(t *testing.T) {
	cmd := sleeper(t, 30)
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	err := waitForExit(cmd.Process.Pid, 400*time.Millisecond)
	if err == nil {
		t.Fatal("waitForExit succeeded while the process was still running")
	}
}

func TestRelaunchNeedsAPid(t *testing.T) {
	if err := runRelaunch(nil); err == nil {
		t.Error("runRelaunch with no arguments succeeded")
	}
	if err := runRelaunch([]string{"not-a-pid"}); err == nil {
		t.Error("runRelaunch with a non-numeric pid succeeded")
	}
}

// sleeper starts a long-running child process to watch.
func sleeper(t *testing.T, seconds int) *exec.Cmd {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// ping loops for roughly one second per count and exists on every
		// Windows install, which `sleep` does not.
		cmd = exec.Command("cmd", "/c", "ping", "-n", "30", "127.0.0.1")
	} else {
		cmd = exec.Command("sleep", "30")
	}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start a helper process here: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return cmd
}
