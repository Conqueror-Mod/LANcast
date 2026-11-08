package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/*
 * Debug logging keeps a line per request, except the routes every client
 * polls on a timer. Those were 79% of the log and pushed the evidence for two
 * reports out of it before anybody looked (polledPaths).
 */
func TestPolledRoutesAreLeftOutOfTheRequestLog(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	served := 0
	h := logRequests(log, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { served++ }))

	cases := []struct {
		method, path string
		logged       bool
	}{
		{http.MethodGet, "/api/health", false},
		{http.MethodGet, "/api/together/requests", false},
		{http.MethodGet, "/api/activity", false},
		// Added 2026-11-08: the dashboard's polls, a peer's presence check,
		// a Prometheus scrape, and a window reporting in for ADR 0117.
		{http.MethodGet, "/api/transcodes", false},
		{http.MethodGet, "/api/dashboard/history", false},
		{http.MethodGet, "/api/federation/presence", false},
		{http.MethodGet, "/api/metrics", false},
		{http.MethodGet, "/api/remote/players", false},
		{http.MethodPut, "/api/remote/players/rzeoe3we-mw6tst52", false},
		// Positive controls: what somebody did is still there.
		{http.MethodPost, "/api/remote/players/rzeoe3we-mw6tst52/commands", true},
		{http.MethodDelete, "/api/remote/players/rzeoe3we-mw6tst52", true},
		{http.MethodPut, "/api/remote/players/", true},
		{http.MethodDelete, "/api/transcodes/7", true},
		{http.MethodGet, "/api/stream/42", true},
		{http.MethodPut, "/api/items/42/progress", true},
		{http.MethodGet, "/api/together", true},
		{http.MethodPost, "/api/together/requests", true},
	}
	for _, c := range cases {
		buf.Reset()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(c.method, c.path, nil))
		got := strings.Contains(buf.String(), "path="+c.path)
		if got != c.logged {
			t.Errorf("%s %s: logged %v, want %v (%q)", c.method, c.path, got, c.logged, buf.String())
		}
	}
	if served != len(cases) {
		t.Fatalf("served %d of %d requests; leaving a line out must not drop the request", served, len(cases))
	}
}
