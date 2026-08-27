package api

import (
	"net/http"
	"testing"
)

/*
 * Library 0 searches every library.
 *
 * The store gained this and the route did not, so "everything this person is
 * in" — the question the widening existed to answer — was unreachable through
 * the API. The handler rejected 0 as no such library before the search ran.
 *
 * The shape of that miss is worth a test rather than a comment: a capability
 * added one layer down and never exposed passes every test at both layers,
 * because each is about the half that exists.
 */
func TestCastSearchAcrossEveryLibraryOverTheAPI(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "GET", "/api/libraries/0/cast?q=", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("library 0 answered %d, want 200 — the all-libraries search is unreachable",
			resp.StatusCode)
	}
	resp.Body.Close()

	// A library that genuinely does not exist is still a 404, or 0 would have
	// turned every typo into a silent search of everything.
	missing := h.do(t, "GET", "/api/libraries/99999/cast?q=", nil)
	if missing.StatusCode != http.StatusNotFound {
		t.Errorf("a nonexistent library answered %d, want 404", missing.StatusCode)
	}
	missing.Body.Close()
}
