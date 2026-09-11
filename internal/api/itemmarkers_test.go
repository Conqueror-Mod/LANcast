package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

/*
 * Re-asking about one film's credits.
 *
 * The library-wide refresh decodes every film's tail to repair one row, and was
 * the only way back for a film a shutdown had retired as "unreadable". These hold
 * the parts that do not need ffmpeg: the route exists, refuses honestly when
 * detection cannot run, and rejects an id it cannot read.
 */

func TestItemMarkerRefreshIsRouted(t *testing.T) {
	h := newHarness(t)
	req := httptest.NewRequest("POST", "/api/items/1/markers/refresh", nil)
	rec := httptest.NewRecorder()
	h.srvAPI.Handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
		t.Errorf("status = %d; the route is not registered", rec.Code)
	}
}

// With no detector there is nothing to queue for, and the answer says why
// rather than queueing work that will never run.
func TestItemMarkerRefreshRefusesWithoutADetector(t *testing.T) {
	h := newHarness(t)
	h.srvAPI.markers = nil
	req := httptest.NewRequest("POST", "/api/items/1/markers/refresh", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	h.srvAPI.refreshItemMarkers(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestItemMarkerRefreshRejectsABadID(t *testing.T) {
	h := newHarness(t)
	req := httptest.NewRequest("POST", "/api/items/x/markers/refresh", nil)
	req.SetPathValue("id", "x")
	rec := httptest.NewRecorder()
	h.srvAPI.refreshItemMarkers(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
