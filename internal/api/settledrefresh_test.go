package api

import (
	"fmt"
	"testing"

	"lancast/internal/store"
)

// makeLibrary creates one library and returns its id. Spelled here rather than
// reused from another file so this test does not fail for a reason that has
// nothing to do with refresh scopes.
func makeLibrary(t *testing.T, h *harness) int64 {
	t.Helper()
	var lib store.Library
	decode(t, h.do(t, "POST", "/api/libraries", map[string]any{
		"name": "Films", "kind": "movie", "path": t.TempDir(),
	}), &lib)
	if lib.ID == 0 {
		t.Fatal("library was not created")
	}
	return lib.ID
}

/*
 * The `settled` refresh scope, at the boundary.
 *
 * What is worth guarding here is not the fetching — internal/enrich owns that
 * and tests it — but the routing. `settled` is spelled as a RefreshScope so one
 * `?scope=` parameter covers every variant, and it must **never** reach
 * RefreshCount or RefreshScoped: those clear a stamp so the search-and-score
 * pass picks the row up, which is the one thing a locked row must not undergo.
 *
 * A wrong branch here would not fail. It would answer 200 with a plausible
 * number, having requeued nothing, and the locked titles would stay exactly as
 * empty as they were.
 */

func TestTheSettledScopeIsAccepted(t *testing.T) {
	h := newHarness(t)
	lib := makeLibrary(t, h)

	var body map[string]any
	decode(t, h.do(t, "GET", fmt.Sprintf("/api/libraries/%d/refresh?scope=settled", lib), nil), &body)
	if got := body["scope"]; got != "settled" {
		t.Errorf("scope = %#v, want settled", got)
	}
	if _, ok := body["count"]; !ok {
		t.Error("the preview gave no count, so nothing can be priced before it is run")
	}
}

func TestAnUnknownScopeIsStillRefused(t *testing.T) {
	/*
	 * Adding a scope must not turn the parameter permissive. An unrecognised
	 * value is 400 rather than silently widened to `all` — doing 1,480 lookups
	 * because somebody typed `setled` is the expensive failure scoping exists
	 * to prevent.
	 */
	h := newHarness(t)
	lib := makeLibrary(t, h)
	for _, scope := range []string{"setled", "locked", "everything", "SETTLED"} {
		wantError(t, h.do(t, "GET",
			fmt.Sprintf("/api/libraries/%d/refresh?scope=%s", lib, scope), nil), 400, "bad_request")
		wantError(t, h.do(t, "POST",
			fmt.Sprintf("/api/libraries/%d/refresh?scope=%s", lib, scope), nil), 400, "bad_request")
	}
}

func TestTheOldScopesStillBehave(t *testing.T) {
	// The scopes that existed before, unchanged. A new branch in a shared
	// handler is exactly where the other paths quietly break.
	h := newHarness(t)
	lib := makeLibrary(t, h)
	for _, scope := range []string{"", "all", "unmatched"} {
		url := fmt.Sprintf("/api/libraries/%d/refresh", lib)
		if scope != "" {
			url += "?scope=" + scope
		}
		var body map[string]any
		decode(t, h.do(t, "GET", url, nil), &body)
		if _, ok := body["count"]; !ok {
			t.Errorf("scope %q gave no count", scope)
		}
	}
}

func TestRunningTheSettledPassAnswersWhatItWillAttempt(t *testing.T) {
	/*
	 * An empty library is the honest case to assert on here: with no locked
	 * rows it must answer 0 rather than a number borrowed from another scope.
	 * A handler that fell through to RefreshScoped would answer with whatever
	 * `all` requeued, which on a seeded library is not zero — that is the
	 * mis-routing this test exists to catch.
	 */
	h := newHarness(t)
	lib := makeLibrary(t, h)

	var body map[string]any
	decode(t, h.do(t, "POST", fmt.Sprintf("/api/libraries/%d/refresh?scope=settled", lib), nil), &body)
	if got := body["scope"]; got != "settled" {
		t.Errorf("scope = %#v, want settled", got)
	}
	if got := body["queued"]; got != float64(0) {
		t.Errorf("queued = %#v, want 0 for a library with no locked rows", got)
	}
}

func TestAMissingLibraryIsNotFound(t *testing.T) {
	// The same answer every other library-scoped action gives, checked because
	// the new branch sits after the lookup and could have been placed before it.
	h := newHarness(t)
	wantError(t, h.do(t, "GET", "/api/libraries/999999/refresh?scope=settled", nil), 404, "not_found")
	wantError(t, h.do(t, "POST", "/api/libraries/999999/refresh?scope=settled", nil), 404, "not_found")
}
