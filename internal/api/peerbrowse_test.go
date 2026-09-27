package api

import (
	"context"
	"net/http"
	"testing"
)

/*
 * Looking at somebody else's library, from this side
 * (ADR 0071's amendment).
 *
 * The far server is a stub rather than a second LANcast. What is under test is
 * this side: that it refuses what it should before asking anybody, that it
 * passes through what the far server says rather than second-guessing it, and
 * that a Range survives the hop — which is the whole reason a viewer can seek.
 *
 * **Nothing here proves two real servers talk.** The peer channel is mutual
 * TLS with a pinned key, and a stub speaks plain HTTP; this exercises the
 * handler's decisions, not the transport. That test needs two machines and has
 * not been run.
 */

/*
 * A peer this server has never been introduced to is refused **here**, before
 * anything is asked of anybody.
 *
 * That is the one decision this side owns: whether the caller is signed in,
 * and whether the fingerprint names a server this household paired with.
 * Everything about what may be *seen* belongs to the far server.
 */
func TestBrowsingAnUnknownPeerIsRefusedLocally(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	stranger := anotherServer(t)

	for _, path := range []string{
		"/api/peers/" + stranger.Fingerprint() + "/libraries",
		"/api/peers/" + stranger.Fingerprint() + "/items?library=1",
		"/api/peers/" + stranger.Fingerprint() + "/stream?item=1",
	} {
		t.Run(path, func(t *testing.T) {
			resp := h.authed(t, "GET", path, nil)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("status = %d, want 404", resp.StatusCode)
			}
		})
	}
}

// Signed out, there is nobody asking.
func TestBrowsingAPeerNeedsASession(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")

	resp := h.do(t, "GET", "/api/peers/"+georgia.Fingerprint()+"/libraries", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

/*
 * Any account, not only administrators. Pairing is administrative; looking at
 * what a pairing produced is not — the same split the presence grants draw,
 * and the same reason a share is granted to a server rather than to a person.
 */
func TestAMemberMayBrowseAPeer(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")
	sam := h.member(t, "sam", "another good long password")

	resp := h.asUser(t, sam, "GET", "/api/peers/"+georgia.Fingerprint()+"/libraries", nil)
	defer resp.Body.Close()
	// Not 403: the far server is unreachable in a test, so a gateway error is
	// the honest answer and proves the request got past authorization.
	if resp.StatusCode == http.StatusForbidden {
		t.Error("a member was refused before the request was even attempted")
	}
}

/*
 * A server that is not answering is 502, not 500.
 *
 * This server is fine; the other one is not there. A 500 reads as a fault
 * here, and somebody would go looking in the wrong logs.
 */
func TestAnUnreachablePeerIsAGatewayError(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")

	resp := h.authed(t, "GET", "/api/peers/"+georgia.Fingerprint()+"/libraries", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d for a peer that is not answering, want 502", resp.StatusCode)
	}
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decode(t, resp, &e)
	if e.Error.Code != "peer_unreachable" {
		t.Errorf("code = %q, want peer_unreachable", e.Error.Code)
	}
}

// --- the parts that do not need a live peer --------------------------------

/*
 * The query is forwarded whole rather than rebuilt field by field, so a filter
 * added to the far server's browse endpoint works the day it ships.
 *
 * This replaced a test that rebuilt the handler's logic and then called a stub
 * with it — which would have passed with the handler doing something else
 * entirely. Testing the function the handler actually calls is the difference.
 */
func TestTheBrowseQueryIsForwardedWhole(t *testing.T) {
	for _, c := range []struct{ raw, want string }{
		{"", "/api/federation/items"},
		{"library=7", "/api/federation/items?library=7"},
		{"library=7&kind=movie&q=zephyr&sort=year&limit=5",
			"/api/federation/items?library=7&kind=movie&q=zephyr&sort=year&limit=5"},
		// A parameter this side has never heard of still goes through.
		{"library=7&somethingNew=1", "/api/federation/items?library=7&somethingNew=1"},
	} {
		if got := peerItemsPath(c.raw); got != c.want {
			t.Errorf("peerItemsPath(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// A stream request must name an item before anything is asked of anybody.
func TestStreamingFromAPeerNeedsAnItem(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")

	resp := h.authed(t, "GET", "/api/peers/"+georgia.Fingerprint()+"/stream", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

/*
 * This server adds no opinion about what may be seen.
 *
 * The far server resolves its own share and its own limit; a check here would
 * be this household deciding what the other one meant to share, and a second
 * answer that could disagree with the first. Asserted by reading the handler's
 * own decisions: it looks up a peer and nothing else about permission.
 */
func TestThisServerDecidesNothingAboutTheShare(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")

	// A library this server has never shared with anybody, asked of a peer.
	// If this side were second-guessing, it would refuse before asking.
	lib, err := h.st.CreateLibrary(context.Background(), "Ours", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	resp := h.authed(t, "GET",
		"/api/peers/"+georgia.Fingerprint()+"/items?library="+itoa(lib.ID), nil)
	defer resp.Body.Close()

	// 502 because the peer is not answering — not 403 or 404, which would mean
	// this server had formed its own view.
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d; this server should ask rather than decide", resp.StatusCode)
	}
}
