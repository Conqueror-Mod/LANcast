package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lancast/internal/crashlog"
)

// panicking exercises the middleware directly rather than through the router.
// A route that panics on request would be a route that ships, and the thing
// under test is the wrapper, not any particular handler.
func (h *harness) panicking(t *testing.T, v any) *http.Response {
	t.Helper()
	handler := h.srvAPI.recoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(v)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/items/7", nil))
	return rec.Result()
}

// The caller gets an error it can render, instead of a dropped connection that
// reads as "the network is down".
func TestPanicBecomesAnError(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.panicking(t, "nil map write"),
		http.StatusInternalServerError, "internal")
}

// And it is countable afterwards, which is the whole feature: a stack in the
// log is only found by somebody who already suspects it is there.
func TestPanicIsRecorded(t *testing.T) {
	h := newHarness(t)
	h.panicking(t, "nil map write").Body.Close()

	var body struct {
		Crashes []crashlog.Report `json:"crashes"`
	}
	decode(t, h.do(t, "GET", "/api/crashes", nil), &body)
	if len(body.Crashes) != 1 {
		t.Fatalf("crashes = %d, want 1", len(body.Crashes))
	}
	got := body.Crashes[0]
	if got.Value != "nil map write" {
		t.Errorf("value = %q, want the panic value", got.Value)
	}
	if !strings.Contains(got.Stack, "crashes_test.go") {
		t.Errorf("stack does not name the panicking frame:\n%s", got.Stack)
	}
	// The route, not the URL: `/api/items/7` invites the belief that item 7 is
	// special. There is no pattern on a hand-built request, so the fallback
	// shape is what is asserted here.
	if !strings.Contains(got.Where, "/api/items/7") {
		t.Errorf("where = %q, want the request it happened on", got.Where)
	}
	if got.Version == "" {
		t.Error("version is empty; a crash report that cannot say which build it came from is half a report")
	}
}

// http.ErrAbortHandler is how a handler drops a connection deliberately.
// Recording it would fill the list with things that are not crashes.
func TestDeliberateAbortIsNotACrash(t *testing.T) {
	h := newHarness(t)
	func() {
		defer func() { _ = recover() }()
		h.panicking(t, http.ErrAbortHandler)
	}()

	var body struct {
		Crashes []crashlog.Report `json:"crashes"`
	}
	decode(t, h.do(t, "GET", "/api/crashes", nil), &body)
	if len(body.Crashes) != 0 {
		t.Errorf("crashes = %d, want 0 — an abort is not a fault", len(body.Crashes))
	}
}

// A server that has never crashed answers with an empty array, not a 404 and
// not a null — one shape for the client to render.
func TestCrashesEmpty(t *testing.T) {
	h := newHarness(t)
	var body struct {
		Crashes []crashlog.Report `json:"crashes"`
	}
	decode(t, h.do(t, "GET", "/api/crashes", nil), &body)
	if body.Crashes == nil || len(body.Crashes) != 0 {
		t.Errorf("crashes = %v, want an empty array", body.Crashes)
	}
}

func TestCrashesClear(t *testing.T) {
	h := newHarness(t)
	h.panicking(t, "boom").Body.Close()

	resp := h.do(t, "DELETE", "/api/crashes", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	var body struct {
		Crashes []crashlog.Report `json:"crashes"`
	}
	decode(t, h.do(t, "GET", "/api/crashes", nil), &body)
	if len(body.Crashes) != 0 {
		t.Errorf("crashes = %d after clearing, want 0", len(body.Crashes))
	}
}
