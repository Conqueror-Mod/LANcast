package api

import (
	"context"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"lancast/internal/identity"
)

/*
 * Default-deny, asserted against the whole router.
 *
 * ADR 0046 §3 buys one property: a route added next year is refused until
 * somebody deliberately adds it to the list. That is a claim about routes
 * nobody has written yet, so it cannot be tested by naming them — it is tested
 * by enumerating every route the server registers and requiring each one to be
 * either on the guest list or refused.
 *
 * The router is scraped from the source the same way docs/api.md is checked
 * next door. It is the only enumeration available: patterns are not reachable
 * from a running mux, and this middleware runs before routing anyway.
 */

var guestRouteRe = regexp.MustCompile(`mux\.HandleFunc\("([A-Z]+) (/[^"]*)"`)

// concrete turns a router pattern into a path a request could carry, so the
// matcher is exercised on the shape it will really see.
func concrete(pattern string) string {
	out := []string{}
	for _, seg := range strings.Split(strings.TrimPrefix(pattern, "/"), "/") {
		if strings.HasPrefix(seg, "{") {
			seg = "1"
		}
		out = append(out, seg)
	}
	return "/" + strings.Join(out, "/")
}

func allRoutes(t *testing.T) [][2]string {
	t.Helper()
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("read router: %v", err)
	}
	m := guestRouteRe.FindAllStringSubmatch(string(src), -1)
	if len(m) < 50 {
		t.Fatalf("scraped %d routes, which is too few to be the whole router; "+
			"the pattern has probably stopped matching", len(m))
	}
	out := make([][2]string, 0, len(m))
	for _, r := range m {
		out = append(out, [2]string{r[1], r[2]})
	}
	return out
}

/*
 * The load-bearing test. Every route the server registers is either on the
 * guest allow-list or unreachable by a guest.
 *
 * When somebody adds a handler and this fails, that is the test working: the
 * new route is closed, and opening it is a deliberate edit to a list that is
 * reviewed rather than a default that was never noticed.
 */
func TestAGuestIsRefusedEveryRouteNotOnTheList(t *testing.T) {
	allowed := map[[2]string]bool{}
	for _, r := range guestAllowed {
		allowed[[2]string{r.method, r.pattern}] = true
	}

	var reachable []string
	for _, r := range allRoutes(t) {
		method, pattern := r[0], r[1]
		if _, ok := guestMayReach(method, concrete(pattern)); ok && !allowed[[2]string{method, pattern}] {
			reachable = append(reachable, method+" "+pattern)
		}
	}
	if len(reachable) > 0 {
		t.Errorf("a guest can reach routes that are not on the allow-list:\n  %s\n"+
			"Every route is closed to a guest unless guestAllowed says otherwise.",
			strings.Join(reachable, "\n  "))
	}
}

// An entry naming a route that no longer exists is a permission with nothing
// behind it, and it makes the list read as broader than it is.
func TestEveryAllowListEntryHasARoute(t *testing.T) {
	routes := map[[2]string]bool{}
	for _, r := range allRoutes(t) {
		routes[[2]string{r[0], r[1]}] = true
	}
	for _, r := range guestAllowed {
		if !routes[[2]string{r.method, r.pattern}] {
			t.Errorf("allow-list names %s %s, which is not a route", r.method, r.pattern)
		}
	}
}

/*
 * The matcher itself, where a generous one would be a hole.
 *
 * Each of these is a way somebody might try to spell their way past an entry
 * that looks specific.
 */
func TestTheMatcherIsNotGenerous(t *testing.T) {
	for _, c := range []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		{"exact", http.MethodGet, "/api/guest/me", true},
		{"wrong method", http.MethodPost, "/api/guest/me", false},
		{"trailing slash", http.MethodGet, "/api/guest/me/", false},
		{"extra segment", http.MethodGet, "/api/guest/me/extra", false},
		{"prefix only", http.MethodGet, "/api/guest", false},
		{"different case", http.MethodGet, "/api/Guest/Me", false},
		{"traversal", http.MethodGet, "/api/guest/../items/1", false},
		{"double slash", http.MethodGet, "/api//guest/me", false},
		{"unrelated", http.MethodGet, "/api/items/1", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, got := guestMayReach(c.method, c.path); got != c.want {
				t.Errorf("guestMayReach(%q, %q) = %v, want %v", c.method, c.path, got, c.want)
			}
		})
	}
}

// A pattern segment matches exactly one non-empty segment. An empty one is not
// a wildcard match: "/api/items//stream" is not a request for item "".
func TestAWildcardNeedsASegment(t *testing.T) {
	if segmentsMatch("/api/items/{id}/stream", "/api/items//stream") {
		t.Error("an empty segment satisfied a wildcard")
	}
	if !segmentsMatch("/api/items/{id}/stream", "/api/items/42/stream") {
		t.Error("a real id did not satisfy a wildcard")
	}
}

// --- end to end, with a real redeemed session -------------------------------

// guestToken redeems a ticket and returns the bearer credential.
func guestToken(t *testing.T, f redeemFixture) string {
	t.Helper()
	var got redeemed
	decode(t, f.redeem(t, f.ticketFrom(t, f.georgia, nil)), &got)
	if got.Token == "" {
		t.Fatal("no token")
	}
	return got.Token
}

func (f redeemFixture) asGuest(t *testing.T, token, method, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, f.h.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := f.h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestAGuestCanReadWhoItIs(t *testing.T) {
	f := newRedeemFixture(t)
	token := guestToken(t, f)

	resp := f.asGuest(t, token, http.MethodGet, "/api/guest/me")
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var me struct {
		Peer    string `json:"peer"`
		Subject string `json:"subject"`
	}
	decode(t, resp, &me)
	if me.Peer != identity.Normalize(f.georgia.Fingerprint()) {
		t.Errorf("peer = %q, want the issuing server", me.Peer)
	}
	if me.Subject != "u_georgia" {
		t.Errorf("subject = %q, want the person the ticket named", me.Subject)
	}
}

/*
 * The refusal that matters: a real session, presented to real routes that a
 * member uses every day.
 *
 * These are chosen because each is a different kind of disclosure — the
 * library, somebody's history, the household's people, the settings — and a
 * guest must reach none of them.
 */
func TestAGuestIsRefusedTheOrdinaryAPI(t *testing.T) {
	f := newRedeemFixture(t)
	token := guestToken(t, f)

	for _, path := range []string{
		"/api/libraries",
		"/api/items",
		"/api/items/1",
		"/api/people",
		"/api/settings",
		"/api/peers",
		"/api/playlists",
	} {
		t.Run(path, func(t *testing.T) {
			resp := f.asGuest(t, token, http.MethodGet, path)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("status = %d, want 403 — a guest reached %s", resp.StatusCode, path)
			}
		})
	}
}

// An expired session is not a session. Nothing sweeps on a timer, so the
// check has to be at lookup.
func TestAnExpiredGuestSessionIsRefused(t *testing.T) {
	f := newRedeemFixture(t)
	token := guestToken(t, f)

	// Reach into the book rather than waiting fifteen minutes.
	f.h.srvAPI.guests.mu.Lock()
	for h, g := range f.h.srvAPI.guests.m {
		g.Expires = time.Now().Add(-time.Second)
		f.h.srvAPI.guests.m[h] = g
	}
	f.h.srvAPI.guests.mu.Unlock()

	resp := f.asGuest(t, token, http.MethodGet, "/api/guest/me")
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("an expired guest session was accepted")
	}
}

// A token that was never issued must not resolve to a session, and must not
// be mistaken for an API key either.
func TestAnInventedGuestTokenIsRefused(t *testing.T) {
	f := newRedeemFixture(t)

	resp := f.asGuest(t, "not-a-real-token", http.MethodGet, "/api/guest/me")
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("an invented token was accepted")
	}
}

// --- the object check ------------------------------------------------------

/*
 * Allow-listing a route is not allow-listing the library.
 *
 * ADR 0046 §4 is explicit that the item check must be object-level, because
 * /api/stream/{id} streams whatever id it is handed. These are the tests that
 * a guest permitted the route is not thereby permitted everything behind it.
 */
func TestAGuestReachesOnlySharedItems(t *testing.T) {
	f := newRedeemFixture(t)
	token := guestToken(t, f)
	shared := f.h.addFile(t, "shared.mkv", []byte("x"))

	// Nothing shared yet: the route is allow-listed and the object is not.
	resp := f.asGuest(t, token, http.MethodGet, "/api/stream/"+itoa(shared))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d before sharing, want 404", resp.StatusCode)
	}

	// Share the library the item is in, and the same request is admitted as
	// far as the handler.
	if err := f.h.st.ShareLibrary(context.Background(),
		identity.Normalize(f.georgia.Fingerprint()), f.h.lib.ID, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	resp = f.asGuest(t, token, http.MethodGet, "/api/stream/"+itoa(shared))
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Error("still 404 after the library was shared: the gate is refusing a shared item")
	}
}

/*
 * Un-sharing takes effect on the next request (ADR 0071 §6). The session is
 * untouched, because it never carried the permission in the first place.
 */
func TestUnsharingRefusesTheGuestsNextRequest(t *testing.T) {
	f := newRedeemFixture(t)
	token := guestToken(t, f)
	item := f.h.addFile(t, "film.mkv", []byte("x"))
	peer := identity.Normalize(f.georgia.Fingerprint())

	if err := f.h.st.ShareLibrary(context.Background(), peer, f.h.lib.ID, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	resp := f.asGuest(t, token, http.MethodGet, "/api/stream/"+itoa(item))
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Fatal("fixture: the shared item was refused before un-sharing")
	}

	if err := f.h.st.UnshareLibrary(context.Background(), peer, f.h.lib.ID); err != nil {
		t.Fatal(err)
	}
	resp = f.asGuest(t, token, http.MethodGet, "/api/stream/"+itoa(item))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d after un-sharing, want 404 on the very next request",
			resp.StatusCode)
	}
}

// An id that is not a number, or is zero or negative, is not an object. The
// gate must refuse rather than hand a nonsense id to a handler.
func TestAGuestIsRefusedANonsenseObject(t *testing.T) {
	f := newRedeemFixture(t)
	token := guestToken(t, f)

	for _, id := range []string{"abc", "0", "-1", ""} {
		resp := f.asGuest(t, token, http.MethodGet, "/api/stream/"+id)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Errorf("id %q was accepted", id)
		}
	}
}

/*
 * The sibling-literal trap, kept as a test of its own because it is the one
 * the enumeration test found and the one a future entry could reintroduce.
 *
 * /api/items/{id}/subtitles/search is a different handler that calls
 * OpenSubtitles with the host's own API key. A wildcard that swallowed it
 * would hand a stranger the host's quota and credentials.
 */
func TestAGuestCannotReachSubtitleSearchThroughTheWildcard(t *testing.T) {
	f := newRedeemFixture(t)
	token := guestToken(t, f)
	item := f.h.addFile(t, "film.mkv", []byte("x"))
	if err := f.h.st.ShareLibrary(context.Background(),
		identity.Normalize(f.georgia.Fingerprint()), f.h.lib.ID, "", time.Now()); err != nil {
		t.Fatal(err)
	}

	resp := f.asGuest(t, token, http.MethodGet,
		"/api/items/"+itoa(item)+"/subtitles/search?query=x")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403: the wildcard must not swallow a "+
			"sibling literal the router sends elsewhere", resp.StatusCode)
	}
}

// Every entry naming an id segment must declare it, or the object check is
// silently skipped for that route.
func TestEveryRouteWithAnIdDeclaresItsObject(t *testing.T) {
	for _, r := range guestAllowed {
		if strings.Contains(r.pattern, "{id}") && r.item == "" {
			t.Errorf("%s %s has an {id} segment and declares no object, so the "+
				"object check does not run for it", r.method, r.pattern)
		}
	}
}
