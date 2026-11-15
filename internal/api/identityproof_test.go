package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"lancast/internal/certpin"
	"lancast/internal/identity"
	"lancast/internal/knownserver"
)

/*
 * GET /api/identity/proof (ADR 0124), driven by the desktop client's own
 * verifier over real TLS. The handler and knownserver.Prove build the same
 * message from internal/identity, so this is the test that the two ends of
 * the protocol agree -- each has tests of its own against a stand-in.
 */

func proofServer(t *testing.T, h *harness) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewTLSServer(h.srvAPI.Handler())
	t.Cleanup(srv.Close)
	pin := certpin.SPKIFromDER(srv.Certificate().RawSubjectPublicKeyInfo)
	return srv, pin
}

func proofAddr(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

func TestTheServerProvesItsIdentityToTheDesktopClient(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	srv, pin := proofServer(t, h)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// No session: the proof is asked for before the client will send one.
	if err := knownserver.Prove(ctx, proofAddr(t, srv.URL), pin, h.srvAPI.ident.Grouped()); err != nil {
		t.Fatalf("the server could not prove its own identity: %v", err)
	}
}

func proofGet(t *testing.T, client *http.Client, base, fingerprint, nonce string) int {
	t.Helper()
	q := url.Values{}
	q.Set("fingerprint", fingerprint)
	q.Set("nonce", nonce)
	resp, err := client.Get(base + "/api/identity/proof?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func goodNonce(t *testing.T) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

/*
 * ADR 0044 section 7 gates GET /api/identity so a scanner cannot ask "who are
 * you". This route keeps that: only a caller who already names the
 * fingerprint gets an answer, and everybody else gets the same 404 as a route
 * that does not exist.
 */
func TestTheProofAnswersOnlySomebodyWhoAlreadyKnowsTheFingerprint(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	srv, _ := proofServer(t, h)

	other, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, fp := range map[string]string{
		"nobody":         "",
		"another server": other.Fingerprint(),
		"garbage":        "hello",
	} {
		if got := proofGet(t, srv.Client(), srv.URL, fp, goodNonce(t)); got != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", name, got)
		}
	}
	if got := proofGet(t, srv.Client(), srv.URL, h.srvAPI.ident.Fingerprint(), goodNonce(t)); got != http.StatusOK {
		t.Errorf("own fingerprint: status = %d, want 200 with no session", got)
	}
}

// Plain HTTP has no connection key, so there is nothing to bind a proof to.
func TestTheProofIsNotGivenOverPlainHTTP(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	if got := proofGet(t, h.srv.Client(), h.srv.URL, h.srvAPI.ident.Fingerprint(), goodNonce(t)); got != http.StatusNotFound {
		t.Errorf("status = %d, want 404 over plain HTTP", got)
	}
}

func TestTheProofNeedsAProperNonce(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	srv, _ := proofServer(t, h)
	fp := h.srvAPI.ident.Fingerprint()

	for name, nonce := range map[string]string{
		"missing":   "",
		"too short": base64.RawURLEncoding.EncodeToString(make([]byte, 8)),
		"too long":  base64.RawURLEncoding.EncodeToString(make([]byte, 65)),
		"not b64":   "!!!!",
	} {
		if got := proofGet(t, srv.Client(), srv.URL, fp, nonce); got != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, got)
		}
	}
}

// A proof for one connection says nothing about another: the same nonce
// verified against a different session's keying material must fail.
func TestAProofDoesNotTransferBetweenConnections(t *testing.T) {
	h := newHarness(t)
	id := h.srvAPI.ident
	nonce := make([]byte, 32)
	ekmA := make([]byte, identity.ProofEKMSize)
	ekmB := make([]byte, identity.ProofEKMSize)
	ekmB[0] = 1
	sig := id.Prove(nonce, ekmA)
	if err := identity.VerifyProof(id.Fingerprint(), id.Public(), nonce, ekmB, sig); !errors.Is(err, identity.ErrProof) {
		t.Errorf("a proof over one session verified over another: %v", err)
	}
}
