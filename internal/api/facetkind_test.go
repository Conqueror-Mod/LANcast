package api

import (
	"net/http"
	"testing"
)

// The facets of one level of a music library (ADR 0114). The level is spelled
// into SQL, so anything outside the closed set must be refused, not passed on.
func TestFacetsKindIsAClosedSet(t *testing.T) {
	h := newHarness(t)
	base := "/api/libraries/" + itoa(h.lib.ID) + "/facets"
	for _, kind := range []string{"", "album", "track"} {
		if rr := h.do(t, "GET", base+"?kind="+kind, nil); rr.StatusCode != http.StatusOK {
			t.Errorf("kind=%q: %d, want 200", kind, rr.StatusCode)
		}
	}
	for _, kind := range []string{"artist", "movie", "album'--"} {
		if rr := h.do(t, "GET", base+"?kind="+kind, nil); rr.StatusCode != http.StatusBadRequest {
			t.Errorf("kind=%q: %d, want 400", kind, rr.StatusCode)
		}
	}
}
