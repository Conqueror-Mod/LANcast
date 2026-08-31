package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

/*
 * A caller that goes away is not a server error.
 *
 * Measured on a live server rather than imagined: of 474 ERROR lines in
 * lancastd.log, 421 were `context canceled` — 236 "list libraries", 98 "count
 * users", 55 "review queue". Every one of them is somebody navigating away, or
 * typing the next letter into the search box while the previous request is
 * still in flight. Nothing failed, and each was answered with a 500.
 *
 * The cost is not the wasted status code, which reaches nobody. It is that the
 * log cried wolf on every navigation, so the 53 genuine faults were buried in
 * it — and this project has twice paid for a fault that was sitting in the log
 * the whole time.
 */

func TestACancelledRequestIsNotAServerError(t *testing.T) {
	h := newHarness(t)

	rec := httptest.NewRecorder()
	h.srvAPI.writeInternal(rec, fmt.Errorf("list libraries: %w", context.Canceled), "list libraries")

	if rec.Code == http.StatusInternalServerError {
		t.Errorf("a cancelled request answered 500; it is not a failure of this server")
	}
	if rec.Code != statusClientClosed {
		t.Errorf("status = %d, want %d", rec.Code, statusClientClosed)
	}
}

// A real failure still is one. This is the half that stops the fix from
// silencing the thing the log exists for.
func TestARealFailureStillReportsFiveHundred(t *testing.T) {
	h := newHarness(t)

	rec := httptest.NewRecorder()
	h.srvAPI.writeInternal(rec, errors.New("disk on fire"), "list libraries")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for a genuine error", rec.Code)
	}
}

/*
 * And a deadline this server set and then missed is still an error.
 *
 * Deliberately not folded in with cancellation: DeadlineExceeded means we
 * promised something and did not deliver it, which is exactly the kind of event
 * the log is for. Only cancellation means the other end left.
 */
func TestADeadlineIsStillAnError(t *testing.T) {
	h := newHarness(t)

	rec := httptest.NewRecorder()
	h.srvAPI.writeInternal(rec, fmt.Errorf("probe: %w", context.DeadlineExceeded), "probe")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 — a missed deadline is this server's fault",
			rec.Code)
	}
}
