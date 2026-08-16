package api

import (
	"net/http"
	"testing"
)

/*
 * The version contract, enforced rather than merely written down.
 *
 * ADR 0018 has stated the policy since M3 and nothing checked it. The valuable
 * half is the refusal: without it, a client built against a contract this
 * server does not speak discovers the mismatch as a field that is mysteriously
 * absent three screens later, and the report that arrives is "the library page
 * is blank".
 */

func TestEveryAPIResponseStatesItsVersion(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "GET", "/api/health", nil)
	defer resp.Body.Close()
	if got := resp.Header.Get(APIVersionHeader); got != "1" {
		t.Errorf("%s = %q, want \"1\"", APIVersionHeader, got)
	}
}

// An absent header means "whatever you have". Every existing client sends
// nothing, and all of them must keep working — this is an opt-in assertion.
func TestNoVersionHeaderIsFine(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "GET", "/api/libraries", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 for a request that asserted nothing", resp.StatusCode)
	}
}

func (h *harness) withVersion(t *testing.T, value string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("GET", h.srv.URL+"/api/libraries", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(APIVersionHeader, value)
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestMatchingVersionIsServed(t *testing.T) {
	h := newHarness(t)
	resp := h.withVersion(t, "1")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 when the client asked for the version we speak", resp.StatusCode)
	}
}

// The whole point: a mismatch is named at the door.
func TestFutureVersionIsRefusedByName(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.withVersion(t, "2"),
		http.StatusBadRequest, "unsupported_api_version")
}

// A header that is not a number is a client bug worth naming. Serving it
// anyway would hide the fault at the moment somebody is looking for it.
func TestMalformedVersionIsRefused(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.withVersion(t, "one"), http.StatusBadRequest, "bad_request")
}

/*
 * Version negotiation sits outside authentication.
 *
 * A client asking for a contract this build cannot serve should be told that,
 * not told to sign in first — otherwise the version fault presents as an auth
 * fault, and the two have entirely different fixes.
 */
func TestVersionIsCheckedBeforeAuth(t *testing.T) {
	h := newHarness(t)
	req, err := http.NewRequest("GET", h.srv.URL+"/api/settings", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(APIVersionHeader, "99")
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	wantError(t, resp, http.StatusBadRequest, "unsupported_api_version")
}

// The static client is served from the same origin and has no contract of its
// own, so the header has no business being on it.
func TestNonAPIResponsesCarryNoVersionHeader(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "GET", "/", nil)
	defer resp.Body.Close()
	if got := resp.Header.Get(APIVersionHeader); got != "" {
		t.Errorf("%s = %q on a non-API path, want none", APIVersionHeader, got)
	}
}

// health lists every revision this build can serve, which is a different
// question from which one this response is.
func TestHealthListsSupportedVersions(t *testing.T) {
	h := newHarness(t)
	var body struct {
		APIVersion  int   `json:"api_version"`
		APIVersions []int `json:"api_versions"`
	}
	decode(t, h.do(t, "GET", "/api/health", nil), &body)
	if body.APIVersion != APIVersion {
		t.Errorf("api_version = %d, want %d", body.APIVersion, APIVersion)
	}
	if len(body.APIVersions) == 0 {
		t.Error("api_versions is empty; a client cannot tell what else this build speaks")
	}
}
