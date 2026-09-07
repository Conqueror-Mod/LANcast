package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

/*
 * request issues one request with whatever credentials the case needs.
 *
 * A single helper taking both, rather than one per combination, because the
 * point of several of these tests is precisely what happens when a cookie and a
 * bearer header arrive *together* — and a helper that could not express that
 * would quietly make the most important case untestable.
 */
func (h *harness) request(t *testing.T, method, path, origin, bearer string,
	cookie bool, body any) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, h.srv.URL+path, r)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if bearer != "" {
		req.Header.Set("Authorization", bearer)
	}
	if cookie && h.cookie != nil {
		req.AddCookie(h.cookie)
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// doKey authenticates with a key and nothing else — no cookie, which is how a
// third-party client actually calls.
func (h *harness) doKey(t *testing.T, method, path, secret string, body any) *http.Response {
	t.Helper()
	return h.request(t, method, path, "", "Bearer "+secret, false, body)
}

// doOrigin sends a cookie *and* the given Authorization header from a foreign
// origin, which is the shape the CSRF cases turn on.
func (h *harness) doOrigin(t *testing.T, method, path, origin, bearer string, body any) *http.Response {
	t.Helper()
	return h.request(t, method, path, origin, bearer, true, body)
}

func errorMessage(t *testing.T, resp *http.Response) string {
	t.Helper()
	var body struct {
		Error apiError `json:"error"`
	}
	b, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(b, &body)
	return body.Error.Message
}

/*
 * API keys, and the four things about them that are security rather than
 * plumbing (ADR 0061).
 *
 * The plumbing — create, list, revoke — is worth a test each and would be
 * caught by using the feature once. The four below would not: each of them
 * fails *open*, quietly, in a way that looks exactly like the feature working.
 *
 *   a key reaching an admin route
 *   a key minting another key
 *   a key skipping CSRF for the cookie path as well as its own
 *   a key reading somebody else's keys, or revoking them
 *
 * Every one of these is a test that must be watched to fail before it is worth
 * anything, and each was.
 */

// mint creates a key through the API and returns its secret, the way a person
// would get one. Uses the harness's own session, so this also proves the
// session path still works.
func mint(t *testing.T, h *harness, name string) (secret, id string) {
	t.Helper()
	resp := h.authed(t, "POST", "/api/keys", map[string]any{"name": name})
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		t.Fatalf("create key: status = %d, want 201", resp.StatusCode)
	}
	var body struct {
		Secret string `json:"secret"`
		Key    struct {
			ID string `json:"id"`
		} `json:"key"`
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Secret == "" {
		t.Fatal("no secret in the creation response; the key is unusable")
	}
	return body.Secret, body.Key.ID
}

// A key authenticates ordinary reads, which is the whole point of it.
func TestAKeyAuthenticatesAnOrdinaryRequest(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	secret, _ := mint(t, h, "backup script")

	resp := h.doKey(t, "GET", "/api/libraries", secret, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 — a valid key did not authenticate", resp.StatusCode)
	}
}

/*
 * A key never reaches an admin route, even though this one belongs to an admin.
 *
 * Adding a library is arbitrary filesystem read access at a path the request
 * chooses. A session is bounded by somebody sitting in front of the app; a key
 * lives unattended in a config file on another machine. If this test goes green
 * by accident, a leaked key can mount any path on the server and read it back
 * over HTTP.
 */
func TestAKeyCannotReachAnAdminRoute(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	secret, _ := mint(t, h, "backup script")

	resp := h.doKey(t, "POST", "/api/libraries", secret, map[string]any{
		"name": "Anything", "kind": "movie", "path": t.TempDir(),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 — an API key performed administration",
			resp.StatusCode)
	}
	if msg := errorMessage(t, resp); !strings.Contains(msg, "API key") {
		t.Errorf("message was %q; it should say why, so somebody does not "+
			"spend an afternoon suspecting their key is invalid", msg)
	}
}

/*
 * A key cannot mint another key.
 *
 * Otherwise revoking a stolen key does not end anything: whoever has it makes a
 * second one first and keeps that. Revocation has to be the end of the story.
 */
func TestAKeyCannotMintAnotherKey(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	secret, _ := mint(t, h, "first")

	resp := h.doKey(t, "POST", "/api/keys", secret, map[string]any{"name": "second"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 — a key minted a key, so revoking one "+
			"revokes nothing", resp.StatusCode)
	}
}

// And it cannot list or revoke keys either, for the same reason: a stolen key
// that can revoke the others is a stolen key that locks the owner out.
func TestAKeyCannotListOrRevokeKeys(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	secret, id := mint(t, h, "first")

	for _, c := range []struct{ method, path string }{
		{"GET", "/api/keys"},
		{"DELETE", "/api/keys/" + id},
	} {
		resp := h.doKey(t, c.method, c.path, secret, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s: status = %d, want 403", c.method, c.path, resp.StatusCode)
		}
	}
}

/*
 * A foreign Origin does not turn the CSRF check off for the cookie path.
 *
 * This is the failure the ADR names: skipping CSRF whenever an Authorization
 * header is *present* would let any page switch the check off by adding a
 * meaningless header while the cookie went on doing the authenticating. The
 * skip has to be conditional on the key actually resolving.
 */
func TestAnUnusableBearerHeaderDoesNotDisableCSRF(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")

	resp := h.doOrigin(t, "POST", "/api/keys", "https://evil.example",
		"Bearer not-a-real-key", map[string]any{"name": "x"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 — a junk Authorization header "+
			"disabled the cross-origin check for a cookie-authenticated request",
			resp.StatusCode)
	}
	if msg := errorMessage(t, resp); !strings.Contains(msg, "cross-origin") {
		t.Errorf("refused with %q, which is not the CSRF refusal — the request "+
			"was rejected for the wrong reason, so this test proves nothing", msg)
	}
}

/*
 * A real key *does* work cross-origin, which is the other half and the reason
 * the skip exists at all.
 *
 * A browser-based third-party client has a foreign Origin by definition. If it
 * were refused, a key would authenticate reads and fail every write, which is a
 * feature that looks like it works until somebody tries to use it.
 */
func TestARealKeyWorksFromAnotherOrigin(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	secret, _ := mint(t, h, "third-party client")

	resp := h.doOrigin(t, "POST", "/api/enrich", "https://someone-elses-app.example",
		"Bearer "+secret, nil)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		if msg := errorMessage(t, resp); strings.Contains(msg, "cross-origin") {
			t.Fatal("a key-authenticated write was refused as cross-origin; " +
				"nothing attaches an Authorization header by itself, so there " +
				"is no ambient credential to forge")
		}
	}
}

// Revoking is the end of it: the key stops authenticating immediately, with no
// cache to wait out.
func TestARevokedKeyStopsWorkingAtOnce(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	secret, id := mint(t, h, "temporary")

	resp := h.doKey(t, "GET", "/api/libraries", secret, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the key did not work before revocation: %d", resp.StatusCode)
	}

	resp = h.authed(t, "DELETE", "/api/keys/"+id, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke: status = %d, want 200", resp.StatusCode)
	}

	resp = h.doKey(t, "GET", "/api/libraries", secret, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 — a revoked key still authenticates",
			resp.StatusCode)
	}
}

// The secret is returned once and never again. A list that carried it would put
// every key in every log and screenshot of that screen.
func TestTheSecretIsNeverListed(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	secret, _ := mint(t, h, "named")

	resp := h.authed(t, "GET", "/api/keys", nil)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)
	if strings.Contains(body, secret) {
		t.Error("the list returned a key's secret")
	}
	if !strings.Contains(body, "named") {
		t.Error("the list did not return the key's name, so nothing can be revoked by it")
	}
}

// A nameless key is refused: a list of keys called nothing is a list nobody can
// revoke confidently, which is the only thing the list is for.
func TestAKeyMustBeNamed(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	resp := h.authed(t, "POST", "/api/keys", map[string]any{"name": "   "})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
