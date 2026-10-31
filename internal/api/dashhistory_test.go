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

// The minute in progress is read live, and its counts are scaled to a whole
// minute so they sit in the same units as the samples beside them.
func TestTheMinuteInProgressIsLive(t *testing.T) {
	h := newHarness(t)
	s := h.srvAPI
	if s.liveSample(time.Now()) != nil {
		t.Fatal("a live reading before the recorder ran")
	}
	start := time.Now()
	s.history.Add(history.Sample{At: start.Unix(), Requests: 40, MediaBytes: 9_000})
	s.markHistory(start, 100, 3, 1_000_000)
	s.counters.requests.Store(130)
	s.counters.serverErrors.Store(3)
	s.counters.mediaBytes.Store(1_000_000 + 7_500_000)
	s.presence.WatchingHere("u1", "Blade Runner")

	// Thirty seconds in: 30 requests and 7.5 MB, which is 60 and 15 MB a minute.
	live := s.liveSample(start.Add(30 * time.Second))
	if live == nil || live.Requests != 60 || live.Errors != 0 || live.MediaBytes != 15_000_000 || live.Watching != 1 {
		t.Fatalf("thirty seconds in: %+v", live)
	}
	// Three seconds in, scaling is noise: the last whole minute stands in,
	// and the states are still read now.
	live = s.liveSample(start.Add(3 * time.Second))
	if live.Requests != 40 || live.MediaBytes != 9_000 || live.Watching != 1 {
		t.Fatalf("three seconds in: %+v", live)
	}

	// And the endpoint carries it.
	var got struct {
		Now *history.Sample `json:"now"`
	}
	decode(t, h.authed(t, "GET", "/api/dashboard/history", nil), &got)
	if got.Now == nil || got.Now.Watching != 1 {
		t.Fatalf("now: %+v", got.Now)
	}
}
