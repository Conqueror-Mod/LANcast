package api

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"testing"
	"time"

	"lancast/internal/guestticket"
	"lancast/internal/identity"
	"lancast/internal/store"
)

/*
 * Minting, from the asking side.
 *
 * The far server is not involved in any of this and is not contacted — what is
 * under test is that this server signs the right assertion about its own
 * person, for the right audience, and refuses the cases where it should not.
 */

// pairedPeer adds a peer from its invite and completes the pairing, which the
// add route deliberately cannot do on its own (ADR 0044 makes it mutual).
func pairedPeer(t *testing.T, h *harness, id identity.Identity, name string) {
	t.Helper()
	h.authed(t, "POST", "/api/peers", map[string]any{
		"invite": inviteFrom(t, id, name),
	}).Body.Close()
	if err := h.st.SetPeerState(context.Background(), id.Fingerprint(), store.PeerPaired); err != nil {
		t.Fatal(err)
	}
}

type mintedTicket struct {
	Ticket    string `json:"ticket"`
	ExpiresAt int64  `json:"expires_at"`
	Peer      string `json:"peer"`
}

func TestAMintedTicketVerifiesAtThePeer(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")

	resp := h.authed(t, "POST", "/api/peers/"+georgia.Fingerprint()+"/ticket", nil)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got mintedTicket
	decode(t, resp, &got)

	if got.Peer != georgia.Fingerprint() {
		t.Errorf("peer = %q, want the audience it was asked for", got.Peer)
	}
	if got.ExpiresAt <= time.Now().Unix() {
		t.Error("the ticket is already expired")
	}

	/*
	 * Verified exactly as the far server would: against *this* server's key,
	 * which is what Georgia pinned at pairing, and with her fingerprint as the
	 * audience. Checking the fields without checking the signature would pass
	 * on a ticket nobody could actually redeem.
	 */
	ours := h.srvAPI.ident
	keyFor := func(issuer string) (ed25519.PublicKey, bool) {
		if issuer == identity.Normalize(ours.Fingerprint()) {
			return ours.Public(), true
		}
		return nil, false
	}
	claims, err := guestticket.Verify(got.Ticket, georgia.Fingerprint(), keyFor, time.Now())
	if err != nil {
		t.Fatalf("the peer would refuse this ticket: %v (%s)", err, guestticket.Reason(err))
	}
	if claims.Subject == "" {
		t.Error("the ticket names nobody")
	}
	if claims.Nonce == "" {
		t.Error("no nonce; the far server could not stop it being replayed")
	}
}

// A ticket names the account that asked. Two people on one server must not
// receive interchangeable tickets, or the subject means nothing on the far side.
func TestATicketNamesTheAccountThatAsked(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")
	sam := h.member(t, "sam", "another good long password")

	subjectOf := func(resp *http.Response) string {
		t.Helper()
		var got mintedTicket
		decode(t, resp, &got)
		ours := h.srvAPI.ident
		claims, err := guestticket.Verify(got.Ticket, georgia.Fingerprint(),
			func(string) (ed25519.PublicKey, bool) { return ours.Public(), true }, time.Now())
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		return claims.Subject
	}

	admin := subjectOf(h.authed(t, "POST", "/api/peers/"+georgia.Fingerprint()+"/ticket", nil))
	member := subjectOf(h.asUser(t, sam, "POST", "/api/peers/"+georgia.Fingerprint()+"/ticket", nil))

	if admin == member {
		t.Fatal("two accounts received tickets naming the same person")
	}
	if member != sam.id {
		t.Errorf("subject = %q, want the asking account %q", member, sam.id)
	}
}

/*
 * A pairing that has not completed has no confirmed key on either side, so a
 * ticket for it could not be verified. Handing one out would produce a failure
 * at the far end, which is the hardest place to diagnose it.
 */
func TestNoTicketForAnIncompletePairing(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)

	// Added from an invite, deliberately not moved to paired.
	h.authed(t, "POST", "/api/peers", map[string]any{
		"invite": inviteFrom(t, georgia, "Utopia"),
	}).Body.Close()

	resp := h.authed(t, "POST", "/api/peers/"+georgia.Fingerprint()+"/ticket", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409 for a pairing that is not complete", resp.StatusCode)
	}
}

// A fingerprint this server has never been introduced to is a 404, not a
// ticket. Unpairing therefore stops minting immediately.
func TestNoTicketForAnUnknownServer(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	stranger := anotherServer(t)

	resp := h.authed(t, "POST", "/api/peers/"+stranger.Fingerprint()+"/ticket", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestUnpairingStopsMinting(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")

	if resp := h.authed(t, "POST", "/api/peers/"+georgia.Fingerprint()+"/ticket", nil); resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("fixture: status %d before unpairing", resp.StatusCode)
	}
	h.authed(t, "DELETE", "/api/peers/"+georgia.Fingerprint(), nil).Body.Close()

	resp := h.authed(t, "POST", "/api/peers/"+georgia.Fingerprint()+"/ticket", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d after unpairing, want 404", resp.StatusCode)
	}
}

// Minting is authentication, not administration: a member may ask for one for
// itself, and there is deliberately no admin gate on this route.
func TestAMemberMayMintItsOwnTicket(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")
	sam := h.member(t, "sam", "another good long password")

	resp := h.asUser(t, sam, "POST", "/api/peers/"+georgia.Fingerprint()+"/ticket", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200: a member may ask for its own ticket", resp.StatusCode)
	}
}

// Signed out, there is nobody for a ticket to name.
func TestNoTicketWithoutASession(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")

	resp := h.do(t, "POST", "/api/peers/"+georgia.Fingerprint()+"/ticket", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// Each ticket carries its own nonce, or the far server's replay defence would
// refuse the second admission of an honest person.
func TestEachTicketHasItsOwnNonce(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")

	ours := h.srvAPI.ident
	keyFor := func(string) (ed25519.PublicKey, bool) { return ours.Public(), true }

	seen := map[string]bool{}
	for i := range 5 {
		var got mintedTicket
		decode(t, h.authed(t, "POST", "/api/peers/"+georgia.Fingerprint()+"/ticket", nil), &got)
		claims, err := guestticket.Verify(got.Ticket, georgia.Fingerprint(), keyFor, time.Now())
		if err != nil {
			t.Fatalf("ticket %d: %v", i, err)
		}
		if seen[claims.Nonce] {
			t.Fatalf("ticket %d reused nonce %q", i, claims.Nonce)
		}
		seen[claims.Nonce] = true
	}
}
