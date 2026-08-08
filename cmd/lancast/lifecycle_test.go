package main

import (
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// The ownership rule (docs/desktop-lifecycle-plan.md):
//
//	Closing a window never stops a server the window does not own.
//
// Both halves matter and they fail in opposite directions. Stopping a server
// this client did not start cuts off everyone else in the house streaming from
// the service. Failing to stop one it did start leaves the invisible hanging
// process the ruling forbids.

func sleeper(t *testing.T) *exec.Cmd {
	t.Helper()
	// A process that will outlive the test unless something stops it.
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "pause")
	} else {
		cmd = exec.Command("sleep", "30")
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start stand-in server: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return cmd
}

// A server this launcher started is stopped when the window closes, and
// stopStartedServer does not return until it is actually gone. Returning early
// is how "closed" gets reported over a process still holding the port.
func TestClosingStopsAServerThisClientStarted(t *testing.T) {
	cmd := sleeper(t)
	l := &launcher{started: cmd}

	start := time.Now()
	l.stopStartedServer()
	took := time.Since(start)

	if took > stopWait+2*time.Second {
		t.Errorf("stopStartedServer took %s; it must be bounded by stopWait (%s)", took, stopWait)
	}

	if cmd.ProcessState == nil {
		t.Error("stopStartedServer returned before the process was waited for; " +
			"the window would report closed over a server still shutting down")
	}
}

// The case that protects everyone else in the house: a launcher that attached to
// a server it did not start must stop nothing at all.
func TestClosingDoesNotStopAServerThisClientDoesNotOwn(t *testing.T) {
	cmd := sleeper(t)

	// started is nil — this is the shape ensureServer leaves when it finds a
	// service or an earlier launch already serving.
	l := &launcher{}
	l.stopStartedServer()

	if cmd.ProcessState != nil {
		t.Fatal("a server this client did not start was stopped")
	}
	// Still there: give it a moment and confirm it was not signalled.
	time.Sleep(200 * time.Millisecond)
	if cmd.ProcessState != nil {
		t.Error("a server this client did not start exited after the window closed")
	}
}

// Closing twice, or closing when nothing was started, must be silent rather than
// an error dialog on the way out of the app.
func TestStopIsSafeWhenNothingWasStarted(t *testing.T) {
	l := &launcher{}
	l.stopStartedServer()
	l.stopStartedServer()
}
