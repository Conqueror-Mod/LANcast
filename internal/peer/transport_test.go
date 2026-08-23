package peer

import (
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"testing"

	"lancast/internal/identity"
)

/*
 * serve runs a real TLS listener with the given config, echoing back the
 * fingerprint it read off the client certificate. That echo is the inbound half
 * of the pin: if it comes back right, the server learned who called without
 * being told.
 *
 * Deliberately not httptest.StartTLS, which substitutes its own certificate
 * over the one it was handed — the pin then correctly rejects the server, and
 * the test fails for a reason that has nothing to do with the code under test.
 */
func serve(t *testing.T, cfg *tls.Config) string {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fp, err := FingerprintFromState(r.TLS)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, fp)
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "https://" + ln.Addr().String()
}

func peerServer(t *testing.T, ident identity.Identity) string {
	t.Helper()
	cfg, err := ServerConfig(ident)
	if err != nil {
		t.Fatalf("ServerConfig: %v", err)
	}
	return serve(t, cfg)
}

// The whole of ADR 0044 §4 in one test: each side presents its identity key,
// each side pins the other's, and the connection carries the caller's identity
// with it.
func TestMutualPinnedHandshake(t *testing.T) {
	server, client := testIdentity(t), testIdentity(t)
	url := peerServer(t, server)

	hc, err := Client(client, server.Fingerprint())
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	resp, err := hc.Get(url)
	if err != nil {
		t.Fatalf("peer call: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if string(body) != client.Fingerprint() {
		t.Errorf("server read caller as %s, want %s", body, client.Fingerprint())
	}
}

// The pin is the security property, so this is the test that matters most: a
// server answering on the right address with a perfectly valid certificate for
// the wrong key must not be talked to.
func TestWrongKeyIsRefused(t *testing.T) {
	server, client, impostor := testIdentity(t), testIdentity(t), testIdentity(t)
	url := peerServer(t, server)

	hc, err := Client(client, impostor.Fingerprint())
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	_, err = hc.Get(url)
	if err == nil {
		t.Fatal("connected to a server presenting a key we did not pin")
	}
	if !errors.Is(err, ErrNotPeerKey) {
		t.Errorf("error = %v, want ErrNotPeerKey so the caller can tell identity from reachability", err)
	}
}

// The fingerprint is what a person reads down the phone, so it has to survive
// being typed back in with its grouping spaces.
func TestPinAcceptsTheHumanReadableForm(t *testing.T) {
	server, client := testIdentity(t), testIdentity(t)
	url := peerServer(t, server)

	hc, err := Client(client, server.Grouped())
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	resp, err := hc.Get(url)
	if err != nil {
		t.Fatalf("grouped fingerprint was not accepted as a pin: %v", err)
	}
	resp.Body.Close()
}

func TestClientRefusesToDialWithoutAPin(t *testing.T) {
	if _, err := Client(testIdentity(t), "   "); err == nil {
		t.Error("a client with nothing pinned must not be constructed at all")
	}
}

// A client that does not speak the peer protocol reaches a handler that can
// tell, rather than one that assumes. Without the ALPN check, any TLS client
// presenting any certificate would be read as a peer.
func TestNonPeerConnectionIsNotAPeer(t *testing.T) {
	if _, err := FingerprintFromState(nil); err == nil {
		t.Error("a plaintext request must not produce a fingerprint")
	}
	// A browser is never asked for a client certificate, so having none is
	// exactly what tells the two apart.
	if _, err := FingerprintFromState(&tls.ConnectionState{}); err == nil {
		t.Error("a TLS connection with no client certificate is not a peer")
	}
}

/*
 * Attach is the part that could break the app for everybody, so it gets the
 * most pointed test: a browser-shaped ClientHello must come back with the base
 * configuration untouched, and in particular must never be asked for a client
 * certificate.
 */
func TestAttachLeavesBrowsersAlone(t *testing.T) {
	ident := testIdentity(t)
	base := &tls.Config{MinVersion: tls.VersionTLS12}
	if _, err := Attach(base, ident); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if base.GetConfigForClient == nil {
		t.Fatal("Attach did not install the switch")
	}

	got, err := base.GetConfigForClient(&tls.ClientHelloInfo{SupportedProtos: []string{"h2", "http/1.1"}})
	if err != nil {
		t.Fatalf("browser hello: %v", err)
	}
	if got != nil {
		t.Fatal("a browser was diverted to the peer configuration")
	}
	if base.ClientAuth != tls.NoClientCert {
		t.Errorf("ClientAuth = %v on the base config: every browser session would be prompted for a certificate", base.ClientAuth)
	}
}

func TestAttachDivertsPeers(t *testing.T) {
	ident := testIdentity(t)
	base := &tls.Config{MinVersion: tls.VersionTLS12}
	if _, err := Attach(base, ident); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	got, err := base.GetConfigForClient(&tls.ClientHelloInfo{SupportedProtos: []string{ALPNProto}})
	if err != nil {
		t.Fatalf("peer hello: %v", err)
	}
	if got == nil {
		t.Fatal("a peer was served the browser configuration")
	}
	if got.ClientAuth != tls.RequireAnyClientCert {
		t.Errorf("ClientAuth = %v, want RequireAnyClientCert: a peer that need not identify itself is not mutual", got.ClientAuth)
	}
}

// End to end through Attach, because the two configs being right individually
// says nothing about the switch choosing correctly under a real handshake.
func TestAttachedServerStillAnswersPeers(t *testing.T) {
	server, client, browserSide := testIdentity(t), testIdentity(t), testIdentity(t)

	// A stand-in for ADR 0014's browser certificate: some other certificate
	// entirely, which is the arrangement in production. Attach must add the
	// peer path beside it without disturbing it.
	browserCert, err := Certificate(browserSide)
	if err != nil {
		t.Fatalf("browser certificate: %v", err)
	}
	base := &tls.Config{Certificates: []tls.Certificate{browserCert}, MinVersion: tls.VersionTLS12}
	if _, err := Attach(base, server); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	url := serve(t, base)

	hc, err := Client(client, server.Fingerprint())
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	resp, err := hc.Get(url)
	if err != nil {
		t.Fatalf("peer call through an attached server: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != client.Fingerprint() {
		t.Errorf("got %q, want the caller's fingerprint %s", body, client.Fingerprint())
	}
}

func TestCertificateCarriesTheIdentityKey(t *testing.T) {
	ident := testIdentity(t)
	cert, err := Certificate(ident)
	if err != nil {
		t.Fatalf("Certificate: %v", err)
	}
	// The certificate must be the identity, not merely signed by it: the pin
	// fingerprints the key it is handed, so a certificate carrying a fresh
	// throwaway key would fail every peer's check.
	got, err := fingerprintOfDER(cert.Certificate)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if got != ident.Fingerprint() {
		t.Errorf("certificate key fingerprints to %s, want the identity's %s", got, ident.Fingerprint())
	}
}
