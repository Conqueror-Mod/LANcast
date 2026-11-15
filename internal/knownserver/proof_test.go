package knownserver

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	identitykey "lancast/internal/identity"
)

/*
 * Prove against real TLS, because the property is about TLS: a signature
 * over exported keying material is only worth anything if it fails for
 * somebody sitting in the middle. These tests stand that somebody up.
 */

// proofHandler answers the way the server does (internal/api identitykey.go):
// only for its own fingerprint, only over TLS. Kept here rather than imported
// so this package does not depend on the API; the API has its own test that
// drives its real handler through Prove.
func proofHandler(id identitykey.Identity) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != proofPath || r.TLS == nil ||
			identitykey.Normalize(r.URL.Query().Get("fingerprint")) != id.Fingerprint() {
			http.NotFound(w, r)
			return
		}
		nonce, err := base64.RawURLEncoding.DecodeString(r.URL.Query().Get("nonce"))
		if err != nil {
			http.Error(w, "bad nonce", http.StatusBadRequest)
			return
		}
		ekm, err := r.TLS.ExportKeyingMaterial(identitykey.ProofExporterLabel, nil, identitykey.ProofEKMSize)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		writeProof(w, id, nonce, ekm)
	}
}

func writeProof(w http.ResponseWriter, id identitykey.Identity, nonce, ekm []byte) {
	_ = json.NewEncoder(w).Encode(proofAnswer{
		PublicKey: base64.StdEncoding.EncodeToString(id.Public()),
		Signature: base64.StdEncoding.EncodeToString(id.Prove(nonce, ekm)),
	})
}

func newIdentity(t *testing.T) identitykey.Identity {
	t.Helper()
	id, err := identitykey.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	return id
}

func proofCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestARealServerProvesItsIdentity(t *testing.T) {
	id := newIdentity(t)
	srv, pin := tlsServerWith(t, proofHandler(id))
	defer srv.Close()

	if err := Prove(proofCtx(t), hostPort(t, srv.URL), pin, id.Grouped()); err != nil {
		t.Fatalf("a server holding the identity could not prove it: %v", err)
	}
}

// A server that is not the one on record answers 404, like a route that does
// not exist, and the client falls back to asking a person. Never acceptance.
func TestAnotherServerOffersNoProof(t *testing.T) {
	recorded, other := newIdentity(t), newIdentity(t)
	srv, pin := tlsServerWith(t, proofHandler(other))
	defer srv.Close()

	err := Prove(proofCtx(t), hostPort(t, srv.URL), pin, recorded.Fingerprint())
	if !errors.Is(err, ErrNoProof) {
		t.Fatalf("err = %v, want ErrNoProof", err)
	}
}

// An older server has no such route.
func TestAServerWithoutTheRouteOffersNoProof(t *testing.T) {
	id := newIdentity(t)
	srv, pin := tlsServerWith(t, http.NotFound)
	defer srv.Close()

	if err := Prove(proofCtx(t), hostPort(t, srv.URL), pin, id.Fingerprint()); !errors.Is(err, ErrNoProof) {
		t.Fatalf("err = %v, want ErrNoProof", err)
	}
}

// A server that answers for an identity with a key that is not it.
func TestAServerAnsweringWithTheWrongKeyFails(t *testing.T) {
	recorded, liar := newIdentity(t), newIdentity(t)
	srv, pin := tlsServerWith(t, func(w http.ResponseWriter, r *http.Request) {
		nonce, _ := base64.RawURLEncoding.DecodeString(r.URL.Query().Get("nonce"))
		ekm, _ := r.TLS.ExportKeyingMaterial(identitykey.ProofExporterLabel, nil, identitykey.ProofEKMSize)
		writeProof(w, liar, nonce, ekm)
	})
	defer srv.Close()

	err := Prove(proofCtx(t), hostPort(t, srv.URL), pin, recorded.Fingerprint())
	if !errors.Is(err, ErrProofFailed) {
		t.Fatalf("err = %v, want ErrProofFailed", err)
	}
}

/*
 * The attack in R11-1, end to end. Something answers at the address with its
 * own certificate and relays the request to the real server, which proves
 * itself faithfully -- over *its* session with the relay. The client checks
 * the proof over *its* session with the relay, and the two exports differ.
 */
func TestAnImpostorRelayingTheRealServerFails(t *testing.T) {
	id := newIdentity(t)
	real, _ := tlsServerWith(t, proofHandler(id))
	defer real.Close()

	upstream := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test relay
	}}
	impostor, impostorPin := tlsServerWith(t, func(w http.ResponseWriter, r *http.Request) {
		resp, err := upstream.Get(real.URL + r.URL.RequestURI())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})
	defer impostor.Close()

	err := Prove(proofCtx(t), hostPort(t, impostor.URL), impostorPin, id.Fingerprint())
	if !errors.Is(err, ErrProofFailed) {
		t.Fatalf("a relayed proof was accepted or misreported: %v", err)
	}
}

// The connection proven must present the key the caller is about to store.
func TestAProofIsOnlyForTheKeyBeingChecked(t *testing.T) {
	id := newIdentity(t)
	srv, _ := tlsServerWith(t, proofHandler(id))
	defer srv.Close()
	_, otherPin := tlsServer(t)

	err := Prove(proofCtx(t), hostPort(t, srv.URL), otherPin, id.Fingerprint())
	if !errors.Is(err, ErrNoProof) {
		t.Fatalf("err = %v, want ErrNoProof for a key that is not the one being checked", err)
	}
}

func TestProveNeedsSomethingToProveAgainst(t *testing.T) {
	if err := Prove(context.Background(), "", "pin", "FP"); !errors.Is(err, ErrNoAddress) {
		t.Errorf("no address: %v", err)
	}
	if err := Prove(context.Background(), "127.0.0.1:1", "", "FP"); !errors.Is(err, ErrNoProof) {
		t.Errorf("no pin: %v", err)
	}
	if err := Prove(context.Background(), "127.0.0.1:1", "pin", " - "); !errors.Is(err, ErrNoProof) {
		t.Errorf("no fingerprint: %v", err)
	}
}
