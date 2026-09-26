package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"lancast/internal/guestticket"
	"lancast/internal/identity"
)

/*
 * Redemption, from the host side.
 *
 * The tickets here are minted by hand with a second identity rather than taken
 * from the mint route, because what is under test is what this server accepts
 * from a stranger. Reusing its own minting would test a round trip and miss
 * every ticket an attacker would actually send.
 */

type redeemFixture struct {
	h       *harness
	georgia identity.Identity // paired
	mallory identity.Identity // not paired
	hostFP  string
}

func newRedeemFixture(t *testing.T) redeemFixture {
	t.Helper()
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")
	return redeemFixture{
		h: h, georgia: georgia, mallory: anotherServer(t),
		hostFP: h.srvAPI.ident.Fingerprint(),
	}
}

func (f redeemFixture) ticketFrom(t *testing.T, id identity.Identity, mutate func(*guestticket.Claims)) string {
	t.Helper()
	now := time.Now()
	c := guestticket.Claims{
		Subject:  "u_georgia",
		Audience: f.hostFP,
		Nonce:    "nonce-" + t.Name() + time.Now().Format("150405.000000000"),
		IssuedAt: now,
		Expires:  now.Add(2 * time.Minute),
	}
	if mutate != nil {
		mutate(&c)
	}
	tok, err := guestticket.Mint(id, c)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// redeem posts without a session, which is the whole point of the route.
func (f redeemFixture) redeem(t *testing.T, ticket string) *http.Response {
	t.Helper()
	return f.h.do(t, "POST", "/api/guest/session", map[string]any{"ticket": ticket})
}

type redeemed struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	Peer      string `json:"peer"`
}

func TestAGoodTicketIsRedeemed(t *testing.T) {
	f := newRedeemFixture(t)

	resp := f.redeem(t, f.ticketFrom(t, f.georgia, nil))
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got redeemed
	decode(t, resp, &got)

	if got.Token == "" {
		t.Error("no token")
	}
	if got.Peer != identity.Normalize(f.georgia.Fingerprint()) {
		t.Errorf("peer = %q, want the issuing server", got.Peer)
	}
	if got.ExpiresAt <= time.Now().Unix() {
		t.Error("the session is already expired")
	}
}

/*
 * Single use. The replay defence is the nonce, and this is the test that it is
 * actually spent rather than merely carried.
 */
func TestATicketCannotBeRedeemedTwice(t *testing.T) {
	f := newRedeemFixture(t)
	tok := f.ticketFrom(t, f.georgia, nil)

	if resp := f.redeem(t, tok); resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("first redemption: status %d", resp.StatusCode)
	}
	resp := f.redeem(t, tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("second redemption: status = %d, want 401", resp.StatusCode)
	}
}

/*
 * Every refusal, and the property is that they are indistinguishable. A caller
 * that could tell a bad signature from an expired ticket from a spent nonce
 * could ask this server questions about its own state.
 */
func TestEveryBadRedemptionLooksTheSame(t *testing.T) {
	f := newRedeemFixture(t)

	cases := []struct {
		name   string
		ticket func() string
	}{
		{"not a paired server", func() string { return f.ticketFrom(t, f.mallory, nil) }},
		{"audience is somebody else", func() string {
			return f.ticketFrom(t, f.georgia, func(c *guestticket.Claims) {
				c.Audience = f.mallory.Fingerprint()
			})
		}},
		{"expired", func() string {
			return f.ticketFrom(t, f.georgia, func(c *guestticket.Claims) {
				c.IssuedAt = time.Now().Add(-time.Hour)
				c.Expires = time.Now().Add(-30 * time.Minute)
			})
		}},
		{"rubbish", func() string { return "not-a-ticket" }},
		{"empty", func() string { return "" }},
	}

	var bodies []string
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := f.redeem(t, c.ticket())
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", resp.StatusCode)
			}
			var e struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			decode(t, resp, &e)
			bodies = append(bodies, e.Error.Code+"|"+e.Error.Message)
		})
	}
	for i, b := range bodies {
		if b != bodies[0] {
			t.Errorf("case %d answered %q, want the same refusal as %q — "+
				"a distinguishable refusal is an oracle", i, b, bodies[0])
		}
	}
}

// Unpairing refuses the next redemption, with nothing per-person to clean up.
func TestUnpairingRefusesRedemption(t *testing.T) {
	f := newRedeemFixture(t)

	if resp := f.redeem(t, f.ticketFrom(t, f.georgia, nil)); resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("fixture: status %d while paired", resp.StatusCode)
	}
	f.h.authed(t, "DELETE", "/api/peers/"+f.georgia.Fingerprint(), nil).Body.Close()

	resp := f.redeem(t, f.ticketFrom(t, f.georgia, nil))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d after unpairing, want 401", resp.StatusCode)
	}
}

/*
 * A pairing that was added but never completed has no confirmed key, so a
 * ticket claiming to be from it must not be admitted. The mint side refuses to
 * issue one; this is the other end, which cannot rely on that.
 */
func TestAnIncompletePairingCannotRedeem(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	// Added, deliberately not moved to paired.
	h.authed(t, "POST", "/api/peers", map[string]any{
		"invite": inviteFrom(t, georgia, "Utopia"),
	}).Body.Close()

	f := redeemFixture{h: h, georgia: georgia, hostFP: h.srvAPI.ident.Fingerprint()}
	resp := f.redeem(t, f.ticketFrom(t, georgia, nil))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for an incomplete pairing", resp.StatusCode)
	}
}

/*
 * Redemption must work cross-origin. A guest is cross-origin by construction
 * (ADR 0046 Fact 3) — the request comes from the friend's own client, served
 * by the friend's own server — so an Origin check here would refuse every
 * legitimate redemption and no attack.
 *
 * This is the test that would fail if somebody later folded this route back
 * behind the CSRF gate.
 */
func TestRedemptionWorksCrossOrigin(t *testing.T) {
	f := newRedeemFixture(t)

	body, err := json.Marshal(map[string]any{"ticket": f.ticketFrom(t, f.georgia, nil)})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", f.h.srv.URL+"/api/guest/session", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://georgia.example")

	resp, err := f.h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d with a foreign Origin, want 200: a guest is "+
			"cross-origin by construction", resp.StatusCode)
	}
}

// Each redemption gets its own token, or two guests would share a session.
func TestEachRedemptionGetsItsOwnToken(t *testing.T) {
	f := newRedeemFixture(t)

	seen := map[string]bool{}
	for i := range 3 {
		var got redeemed
		decode(t, f.redeem(t, f.ticketFrom(t, f.georgia, nil)), &got)
		if seen[got.Token] {
			t.Fatalf("redemption %d reused a token", i)
		}
		seen[got.Token] = true
	}
}
