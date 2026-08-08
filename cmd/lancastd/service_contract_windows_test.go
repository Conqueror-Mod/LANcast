//go:build windows

package main

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"golang.org/x/sys/windows/svc"
)

// What the service tells the Service Control Manager on the way down.
//
// This is the half of the 7031 fix that cannot be proven by measuring shutdown:
// the SCM judges a service by the status it reports, and a StopPending with
// WaitHint zero means "I am already gone". A stop that then takes seconds is
// read as a hang, the process is killed, and the recovery policy restarts it —
// which is why Stop-Service did not keep LANcast stopped.
//
// Driving Execute directly is as close as a test can get without installing a
// service. It does not prove Windows accepts the hint; it proves we send one,
// that it is long enough to cover our own shutdown, and that the final answer is
// a clean exit rather than an error code.
func TestServiceReportsAWaitHintOnStop(t *testing.T) {
	h := &handler{
		addr:    "127.0.0.1:0",
		dataDir: t.TempDir(),
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	requests := make(chan svc.ChangeRequest, 1)
	changes := make(chan svc.Status, 16)

	type result struct {
		svcSpecific bool
		exitCode    uint32
	}
	done := make(chan result, 1)
	go func() {
		s, code := h.Execute(nil, requests, changes)
		done <- result{s, code}
	}()

	// Wait for Running before asking it to stop, the way the SCM would.
	deadline := time.After(30 * time.Second)
	var accepted svc.Accepted
	for running := false; !running; {
		select {
		case st := <-changes:
			if st.State == svc.Running {
				running = true
				accepted = st.Accepts
			}
		case <-deadline:
			t.Fatal("service never reported Running")
		}
	}
	if accepted&svc.AcceptStop == 0 {
		t.Error("service does not accept Stop")
	}

	requests <- svc.ChangeRequest{Cmd: svc.Stop}

	var sawStopPending bool
	var hint uint32
	for !sawStopPending {
		select {
		case st := <-changes:
			if st.State == svc.StopPending {
				sawStopPending = true
				hint = st.WaitHint
			}
		case <-deadline:
			t.Fatal("service never reported StopPending")
		}
	}

	if hint == 0 {
		t.Error("StopPending carries WaitHint 0 — the SCM reads that as " +
			"'already gone', and a stop that takes any time at all is judged a hang")
	}
	// The hint has to cover the shutdown this server actually performs, or it is
	// a promise broken on the same schedule as before.
	if time.Duration(hint)*time.Millisecond < shutdownGrace {
		t.Errorf("WaitHint %dms is shorter than shutdownGrace %s; it must cover our own shutdown",
			hint, shutdownGrace)
	}

	select {
	case r := <-done:
		if r.exitCode != 0 {
			t.Errorf("exit code = %d, want 0; a non-zero exit is a failure to the SCM "+
				"and triggers the recovery policy", r.exitCode)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Execute did not return after Stop")
	}
}
