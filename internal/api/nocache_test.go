package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

/*
 * Status that changes every second must say it is not cacheable.
 *
 * The reported symptom: a progress bar frozen at 0% while the ffmpeg install
 * ran to completion underneath it. Every poll is a GET of the same URL with no
 * cache-buster, and with no cache headers a browser may heuristically reuse the
 * first response — so the client asked once, was told "0 bytes", and was handed
 * that answer for the rest of the download.
 *
 * Cheap to assert and impossible to notice by reading the handler, which is
 * exactly the kind of thing worth a test.
 */
func TestPollingEndpointsForbidCaching(t *testing.T) {
	h := newHarness(t)

	for _, path := range []string{"/api/media-tools"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		h.srvAPI.mediaToolsStatus(rec, req)

		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s Cache-Control = %q, want no-store — a frozen progress bar is what a cached poll looks like", path, got)
		}
	}
}

/*
 * Continue-watching answers the same way, for the same reason with higher
 * stakes: a cached answer sends somebody back to an episode they have already
 * watched, which is the failure that feature exists to prevent.
 */
func TestContinueForbidsCaching(t *testing.T) {
	h := newHarness(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/items/1/continue", nil)
	req.SetPathValue("id", "1")
	h.srvAPI.continueShow(rec, req)

	// Asserted even on the not-found path: the header has to be set before the
	// answer is known, or the one response a proxy is most likely to keep is the
	// one that carries no header at all.
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("continue Cache-Control = %q, want no-store", got)
	}
}
