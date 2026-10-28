package api

import (
	"net/http"
	"testing"
	"time"

	"lancast/internal/history"
)

// The history is an administrator's view, read in windows of 1 to 48 hours
// (ADR 0106).
func TestDashboardHistoryIsAnAdministratorsWindow(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	now := time.Now()
	h.srvAPI.history.Add(history.Sample{At: now.Add(-3 * time.Hour).Unix(), Watching: 1})
	h.srvAPI.history.Add(history.Sample{At: now.Add(-10 * time.Minute).Unix(), Watching: 2, CPU: 12.5})

	var got struct {
		Every   int              `json:"every_seconds"`
		Samples []history.Sample `json:"samples"`
	}
	decode(t, h.authed(t, "GET", "/api/dashboard/history?hours=1", nil), &got)
	if got.Every != 60 || len(got.Samples) != 1 || got.Samples[0].Watching != 2 {
		t.Fatalf("one hour: %+v", got)
	}
	decode(t, h.authed(t, "GET", "/api/dashboard/history", nil), &got)
	if len(got.Samples) != 2 {
		t.Fatalf("the default six hours: %d samples", len(got.Samples))
	}
	for _, bad := range []string{"0", "49", "x"} {
		resp := h.authed(t, "GET", "/api/dashboard/history?hours="+bad, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("hours=%s: %d", bad, resp.StatusCode)
		}
	}
	m := h.member(t, "sam", "a long enough password")
	resp := h.asUser(t, m, "GET", "/api/dashboard/history", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a member: %d", resp.StatusCode)
	}
}

// A sample reads presence, the request counters and memory as they are.
func TestASampleReadsTheServerAsItIs(t *testing.T) {
	h := newHarness(t)
	h.srvAPI.presence.WatchingHere("u1", "Blade Runner")
	h.srvAPI.presence.Seen("u2") // online, idle
	s := h.srvAPI.sampleNow(time.Now(), 30, 120, 2)
	if s.Watching != 1 || s.Requests != 120 || s.Errors != 2 || s.CPU != 30 || s.MemoryMB <= 0 {
		t.Fatalf("%+v", s)
	}
}
