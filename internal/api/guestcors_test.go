package api

import (
	"net/http"
	"strings"
	"testing"
)

/*
 * CORS, which is the only place this server has ever sent an
 * Access-Control-Allow-Origin header.
 *
 * Each of these is a dimension ADR 0046 §7 narrows, and a test for what
 * happens when it is not narrowed: a wildcard, the whole API, an origin that
 * is not paired, an origin that was paired and is not any more, and
 * credentials.
 */

// header issues a request with an Origin and returns the response headers.
func (f redeemFixture) withOrigin(t *testing.T, method, path, origin string, preflightFor string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, f.h.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", origin)
	if preflightFor != "" {
		req.Header.Set("Access-Control-Request-Method", preflightFor)
	}
	resp, err := f.h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// peerOrigin is an origin built from the address the fixture's invite carries.
const peerOrigin = "https://10.121.240.21:8080"

func TestAPairedOriginGetsCORSOnAGuestRoute(t *testing.T) {
	f := newRedeemFixture(t)

	resp := f.withOrigin(t, http.MethodOptions, "/api/guest/me", peerOrigin, http.MethodGet)
	defer resp.Body.Close()

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != peerOrigin {
		t.Errorf("Allow-Origin = %q, want the paired origin echoed back", got)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("preflight status = %d, want 204", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Error("preflight does not permit Authorization, so the token cannot be sent")
	}
	if v := resp.Header.Get("Vary"); !strings.Contains(v, "Origin") {
		t.Errorf("Vary = %q, want Origin — a shared cache could otherwise hand "+
			"one origin's permission to another", v)
	}
}

// Never `*`, and never credentials: the guest credential is a bearer token
// precisely so no cookie is involved (§6).
func TestCORSIsNeverWildcardAndNeverCredentialed(t *testing.T) {
	f := newRedeemFixture(t)

	resp := f.withOrigin(t, http.MethodOptions, "/api/guest/me", peerOrigin, http.MethodGet)
	defer resp.Body.Close()

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got == "*" {
		t.Error("Allow-Origin is a wildcard")
	}
	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("Allow-Credentials = %q, want it never sent: asking browsers "+
			"to attach this server's cookies to a foreign page is what "+
			"SameSite exists to prevent", got)
	}
}

/*
 * Only guest routes. Opening a route to guests later stays one deliberate
 * edit, not two — the CORS answer is derived from the same allow-list.
 */
func TestCORSIsNotOpenedOnTheOrdinaryAPI(t *testing.T) {
	f := newRedeemFixture(t)

	for _, path := range []string{"/api/libraries", "/api/items", "/api/settings", "/api/peers"} {
		t.Run(path, func(t *testing.T) {
			resp := f.withOrigin(t, http.MethodOptions, path, peerOrigin, http.MethodGet)
			defer resp.Body.Close()
			if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
				t.Errorf("Allow-Origin = %q on %s, want none", got, path)
			}
		})
	}
}

// An origin nobody paired with gets nothing, however plausible it looks.
func TestAnUnpairedOriginGetsNoCORS(t *testing.T) {
	f := newRedeemFixture(t)

	for _, origin := range []string{
		"https://evil.example",
		"https://10.121.240.21",      // right host, no port
		"https://10.121.240.21:9999", // right host, wrong port
		"null",
	} {
		t.Run(origin, func(t *testing.T) {
			resp := f.withOrigin(t, http.MethodOptions, "/api/guest/me", origin, http.MethodGet)
			defer resp.Body.Close()
			if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
				t.Errorf("Allow-Origin = %q for %s, want none", got, origin)
			}
		})
	}
}

/*
 * Unpairing closes it, on the next request. The answer is computed from the
 * peer table each time, so there is no cached list to invalidate — which is
 * the same property the object check has.
 */
func TestUnpairingClosesCORS(t *testing.T) {
	f := newRedeemFixture(t)

	resp := f.withOrigin(t, http.MethodOptions, "/api/guest/me", peerOrigin, http.MethodGet)
	resp.Body.Close()
	if resp.Header.Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("fixture: no CORS while paired")
	}

	f.h.authed(t, "DELETE", "/api/peers/"+f.georgia.Fingerprint(), nil).Body.Close()

	resp = f.withOrigin(t, http.MethodOptions, "/api/guest/me", peerOrigin, http.MethodGet)
	defer resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q after unpairing, want none", got)
	}
}

// Redemption is included, because it is where a session begins and there is
// no token to present yet.
func TestRedemptionIsCORSEnabled(t *testing.T) {
	f := newRedeemFixture(t)

	resp := f.withOrigin(t, http.MethodOptions, "/api/guest/session", peerOrigin, http.MethodPost)
	defer resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != peerOrigin {
		t.Errorf("Allow-Origin = %q on redemption, want the paired origin", got)
	}
}

/*
 * A preflight authorises nothing. It is answered before the session gate
 * because the browser strips Authorization from it, and that is exactly the
 * property that would be dangerous if it leaked into the real request.
 */
func TestAPreflightDoesNotAdmitTheRealRequest(t *testing.T) {
	f := newRedeemFixture(t)

	pre := f.withOrigin(t, http.MethodOptions, "/api/guest/me", peerOrigin, http.MethodGet)
	pre.Body.Close()
	if pre.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status = %d", pre.StatusCode)
	}

	// The same route, same origin, no token.
	resp := f.withOrigin(t, http.MethodGet, "/api/guest/me", peerOrigin, "")
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("a preflight let an unauthenticated request through")
	}
}

// An origin with a path, query or fragment is not an origin. Accepting one
// would mean matching on a string the page can partly choose.
func TestAMalformedOriginIsRefused(t *testing.T) {
	for _, bad := range []string{
		"https://10.121.240.21:8080/path",
		"https://10.121.240.21:8080?q=1",
		"https://10.121.240.21:8080#f",
		"https://user@10.121.240.21:8080",
		"10.121.240.21:8080",
		"",
	} {
		if got := originHost(bad); got != "" && strings.ContainsAny(got, "/?#@") {
			t.Errorf("originHost(%q) = %q, which is not a bare host", bad, got)
		}
	}
	if originHost("https://10.121.240.21:8080/path") != "" {
		t.Error("an origin with a path was accepted")
	}
}

/*
 * Added is not paired, and the CORS answer has to know the difference.
 *
 * Accepting an invite records a peer; it does not establish a pairing, which
 * ADR 0044 makes mutual and only the transport can confirm. An origin whose
 * pairing never completed has no confirmed key on either side, so nothing
 * from it can be verified — opening CORS to it would be opening it to a
 * server this one has never actually spoken to.
 *
 * Written after a break-check found nothing testing it: making the CORS
 * lookup ignore peer state failed no tests at all.
 */
func TestAnAddedButUnpairedOriginGetsNoCORS(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)

	// Added from an invite, deliberately never moved to paired.
	h.authed(t, "POST", "/api/peers", map[string]any{
		"invite": inviteFrom(t, georgia, "Utopia"),
	}).Body.Close()

	f := redeemFixture{h: h, georgia: georgia, hostFP: h.srvAPI.ident.Fingerprint()}
	resp := f.withOrigin(t, http.MethodOptions, "/api/guest/me", peerOrigin, http.MethodGet)
	defer resp.Body.Close()

	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q for a pairing that never completed, want none", got)
	}
}
