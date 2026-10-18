package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A route the client's router owns gets the shell; a build file that is not
// there is a 404, never the shell. Answering a script request with HTML left
// a page from an older build blank with no way to notice (index.html reloads
// on the 404).
func TestAMissingBuildFileIsNotTheShell(t *testing.T) {
	h := Handler()
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec
	}
	if rec := get("/library/1"); rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("a client route = %d %s, want the shell", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := get("/assets/index-FromAnOlderBuild.js"); rec.Code != http.StatusNotFound {
		t.Errorf("a missing build file = %d, want 404", rec.Code)
	}
}
