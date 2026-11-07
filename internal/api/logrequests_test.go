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
		// Positive controls: what somebody did is still there.
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
