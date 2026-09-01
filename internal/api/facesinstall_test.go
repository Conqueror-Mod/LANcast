package api

import (
	"context"
	"testing"
	"time"

	"lancast/internal/faceinstall"
)

/*
 * Starting the face-model download must not destroy the lock that guards it.
 *
 * The reported fault was that pressing "Download the face models" killed the
 * server. It did, every time: the handler reset the job with
 * `*j = faceJob{...}` while holding `j.mu`, which replaced the held mutex with
 * the literal's zero-value one, and the following Unlock released a lock nobody
 * held — `fatal error: sync: unlock of unlocked mutex`.
 *
 * That is a runtime fatal, not a panic. recoverPanics cannot catch it, nothing
 * is written to the crash list, and the stack goes to stderr, which a Windows
 * service discards. From the outside the process simply vanished and every
 * library said "failed to fetch", while the home page — already rendered —
 * carried on looking healthy.
 *
 * These tests cannot use `defer recover()` for the same reason the server could
 * not: a fatal error is unrecoverable and takes the test binary with it. They
 * pass by completing at all, which is exactly the property that was missing.
 */

// The reported fault, at its narrowest: lock, reset, unlock. Before the fix
// this aborted the test process on the Unlock.
func TestStartingAJobKeepsItsLockIntact(t *testing.T) {
	j := &faceJob{}

	j.mu.Lock()
	j.reset(1234, func() {})
	j.mu.Unlock()

	// Reaching here at all is the assertion. Taking the lock again proves it is
	// a working mutex rather than one left in a state that happens not to have
	// tripped yet.
	j.mu.Lock()
	j.mu.Unlock()
}

// And it survives being started repeatedly — two presses of one button is a
// person, not a fault, and each press runs the same reset.
func TestRepeatedStartsDoNotCorruptTheLock(t *testing.T) {
	j := &faceJob{}
	for i := 0; i < 50; i++ {
		j.mu.Lock()
		j.reset(int64(i), func() {})
		j.mu.Unlock()
		_ = j.snapshot()
	}
}

/*
 * reset means "just started", so every field a previous run left behind has to
 * go — not only the ones a fresh struct literal would have zeroed by accident.
 *
 * The error and the finish time are the two that matter: a job that failed and
 * was retried would otherwise report the *old* failure while running, and the
 * UI would show a finished install that never happened.
 */
func TestResetClearsTheOutcomeOfThePreviousRun(t *testing.T) {
	j := &faceJob{
		running:  false,
		stage:    faceinstall.StageVerifying,
		asset:    "left-over.onnx",
		done:     999,
		err:      "the previous attempt failed",
		finished: time.Unix(1, 0),
	}

	j.mu.Lock()
	j.reset(500, func() {})
	j.mu.Unlock()

	snap := j.snapshot()
	if snap["running"] != true {
		t.Error("a started job does not report itself as running")
	}
	if _, ok := snap["error"]; ok {
		t.Errorf("the previous run's error survived the restart: %v", snap["error"])
	}
	if _, ok := snap["finished_at"]; ok {
		t.Error("a running job reports a finish time from the previous attempt")
	}
	if snap["asset"] != "" {
		t.Errorf("asset carried over: %v", snap["asset"])
	}
	if snap["bytes_done"] != int64(0) {
		t.Errorf("progress carried over: %v", snap["bytes_done"])
	}
	if snap["bytes_total"] != int64(500) {
		t.Errorf("bytes_total is %v, want 500", snap["bytes_total"])
	}
	if snap["stage"] != string(faceinstall.StageDownloading) {
		t.Errorf("stage is %v, want downloading", snap["stage"])
	}
}

// The cancel function is what the cancel endpoint reaches for, so a reset that
// dropped it would leave a 113MB download with no way to stop it.
func TestResetInstallsTheCancelFunction(t *testing.T) {
	j := &faceJob{}
	_, cancel := context.WithCancel(context.Background())

	j.mu.Lock()
	j.reset(1, cancel)
	j.mu.Unlock()

	if j.cancel == nil {
		t.Fatal("a running job has no way to be cancelled")
	}
}
