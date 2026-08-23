/*
 * Peer transport: how two LANcast servers prove who they are to each other.
 *
 * [ADR 0044](../../docs/adr/0044-server-identity-and-peering.md) §4 is the whole
 * specification, and it is short: *"the certificate presented on a peer
 * connection must carry the public key that arrived in the invite, or the
 * connection does not happen"* — mutual, pinned, no CA and no hostname. Every
 * other TLS connection this server makes validates normally and is untouched.
 *
 * # Why this shares a port with the browser, and how
 *
 * The obvious build is a second listener on a second port. It was not chosen,
 * for a reason that is about people rather than code: an invite already carries
 * addresses, those addresses already carry the port the server answers on, and
 * a second port means every one of them is a hint that is right about the
 * machine and wrong about where to knock. Federation would then fail in the one
 * way that is hardest to diagnose from the far end — reachable, and silent.
 *
 * So peer connections arrive on the same port as everything else and are told
 * apart by **ALPN**. A peer client advertises `lancast-peer/1`; nothing else
 * does, and no browser ever will. `GetConfigForClient` sees it in the
 * ClientHello — before any certificate is chosen — and answers with an entirely
 * different TLS configuration: the identity certificate rather than ADR 0014's
 * browser one, and a demand for a client certificate.
 *
 * That switch is what keeps the two worlds separate. A browser negotiates
 * `http/1.1`, gets the ordinary config, and is never asked for a certificate it
 * does not have — which matters, because a client-certificate request on the
 * main config is a modal prompt in front of every person who opens the app.
 *
 * # The certificate is the identity, not a statement about a name
 *
 * It is self-signed, it carries the Ed25519 identity key, and it asserts no
 * hostname worth checking. Both ends verify by fingerprinting the key they were
 * handed and comparing it to the one from the invite, which is exactly what
 * `internal/certpin` already does for the desktop window — this project's trust
 * primitive, applied in the direction ADR 0044 needed.
 *
 * Its expiry is long and deliberately uninteresting. A pinned key is trusted
 * because it is *that key*, so a date on the wrapper decides nothing; a short
 * expiry would only introduce a way for a working pairing to stop working at
 * three in the morning for a reason nobody could see.
 */
package peer

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"lancast/internal/identity"
)

/*
 * ALPNProto is the marker a peer client advertises so the server knows, at
 * ClientHello time, to answer with the peer configuration rather than the
 * browser one.
 *
 * It is advertised but **not negotiated**. `net/http` drops any connection
 * whose agreed protocol is neither `http/1.1` nor empty — it looks the protocol
 * up in `TLSNextProto`, finds nothing, and closes, which from the far end is a
 * clean handshake followed by EOF. So the peer client offers this marker
 * *ahead of* `http/1.1` and the server agrees on `http/1.1`, leaving ordinary
 * HTTP to run over the connection.
 *
 * The marker therefore does its whole job before the handshake finishes, which
 * is also why it cannot be what identifies a peer afterwards — see
 * FingerprintFromState.
 */
const ALPNProto = "lancast-peer/1"

// certLifetime is long because the key is pinned and the wrapper decides
// nothing. See the package comment.
const certLifetime = 20 * 365 * 24 * time.Hour

// ErrNotPeerKey is returned when the far end presented a certificate carrying
// some other key than the one expected. It is an identity failure, not a
// reachability one, and the distinction matters to the caller: retrying will
// not help and the address should not be blamed.
var ErrNotPeerKey = errors.New("peer presented a different key")

/*
 * Certificate builds the self-signed certificate that carries this server's
 * identity key.
 *
 * Signed by the identity itself through crypto.Signer, so the private half is
 * used without ever being copied out of `internal/identity`.
 */
func Certificate(ident identity.Identity) (tls.Certificate, error) {
	pub := ident.Public()

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("peer certificate serial: %w", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		// The fingerprint, so anybody reading the certificate by hand sees the
		// same string they read down the phone. It authenticates nothing — the
		// key does that — and exists to make the two views agree.
		Subject:   pkix.Name{CommonName: ident.Fingerprint()},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  time.Now().Add(certLifetime),
		KeyUsage:  x509.KeyUsageDigitalSignature,
		// Both, because one identity serves both ends of a mutual connection.
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, ident.Signer())
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("peer certificate: %w", err)
	}
	// Leaf must be the *parsed* certificate, not the template it was built
	// from. A template carries no Raw bytes, and Go hands Leaf straight to the
	// handshake — so setting it to tmpl presents an empty certificate and the
	// far end closes the connection with "bad certificate", which reads
	// exactly like a pin failure and is not one.
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("parse own peer certificate: %w", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: ident.Signer(), Leaf: leaf}, nil
}

/*
 * ServerConfig is the TLS configuration a peer connection is answered with.
 *
 * RequireAnyClientCert rather than a verified chain: Go must *demand* a
 * certificate, and must not try to validate it against roots it does not have.
 * Deciding whether the presented key is a peer we know is this project's job
 * and happens in the handler, against the database, where "who is a peer" is
 * actually recorded.
 */
func ServerConfig(ident identity.Identity) (*tls.Config, error) {
	cert, err := Certificate(ident)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAnyClientCert,
		MinVersion:   tls.VersionTLS13,
		// http/1.1, not the marker: see ALPNProto. The marker got us to this
		// configuration; agreeing on it would leave net/http unable to serve
		// the connection it selected.
		NextProtos: []string{"http/1.1"},
	}, nil
}

/*
 * Attach makes an existing browser-facing TLS configuration also answer peers.
 *
 * The base config is returned unmodified for every ordinary connection. Only a
 * ClientHello advertising ALPNProto is diverted, and it is diverted *whole* —
 * different certificate, different client-auth policy — rather than by patching
 * fields on a shared config, which would leak the client-certificate demand
 * into every browser session.
 *
 * A server whose identity cannot produce a certificate keeps serving browsers.
 * Federation failing is a feature being unavailable; refusing to start is the
 * app being unavailable, and those are not the same size of problem.
 */
func Attach(base *tls.Config, ident identity.Identity) (*tls.Config, error) {
	peerCfg, err := ServerConfig(ident)
	if err != nil {
		return nil, err
	}
	base.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		for _, p := range hello.SupportedProtos {
			if p == ALPNProto {
				return peerCfg, nil
			}
		}
		return nil, nil // nil means "carry on with the base config"
	}
	return base, nil
}

/*
 * ClientConfig dials a peer, presenting our identity and pinning theirs.
 *
 * InsecureSkipVerify disables the checks that are meaningless here — a chain to
 * a root nobody shares, and a hostname on a certificate that asserts none — and
 * VerifyPeerCertificate replaces them with the only check that means anything:
 * is this the key from the invite.
 *
 * The name is unfortunate and worth the same sentence `internal/certpin` spends
 * on it: this is not *ignore certificate errors*. It is a stricter test than
 * the one it replaces, because a CA-issued certificate for the right hostname
 * would still fail it.
 */
func ClientConfig(ident identity.Identity, wantFingerprint string) (*tls.Config, error) {
	cert, err := Certificate(ident)
	if err != nil {
		return nil, err
	}
	want := identity.Normalize(wantFingerprint)
	if want == "" {
		return nil, errors.New("no peer fingerprint to pin")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		// The marker first so the server diverts us, http/1.1 second so there
		// is something both ends can actually agree to speak.
		NextProtos:         []string{ALPNProto, "http/1.1"},
		InsecureSkipVerify: true, // replaced by the pin below, not relaxed
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			got, err := fingerprintOfDER(raw)
			if err != nil {
				return err
			}
			if got != want {
				return fmt.Errorf("%w: got %s, want %s", ErrNotPeerKey, got, want)
			}
			return nil
		},
	}, nil
}

/*
 * FingerprintFromState reads who connected, from the certificate they
 * presented.
 *
 * This is the inbound half of the pin. The handler calls it and then asks the
 * database whether that fingerprint is a peer — so an unknown key reaches a
 * lookup and a refusal, never a handler.
 *
 * **A client certificate is the discriminator, not the ALPN.** The marker is
 * gone by now (see ALPNProto), and it does not need to be here: only the peer
 * configuration ever asks for a client certificate, and the browser
 * configuration never does. So a connection carrying one went through the peer
 * path by construction, and a browser cannot produce this state by choosing a
 * protocol.
 */
func FingerprintFromState(cs *tls.ConnectionState) (string, error) {
	if cs == nil {
		return "", errors.New("not a TLS connection")
	}
	if len(cs.PeerCertificates) == 0 {
		// Not a peer: nothing else on this server asks for a certificate.
		return "", errors.New("not a peer connection: no client certificate")
	}
	pub, ok := cs.PeerCertificates[0].PublicKey.(ed25519.PublicKey)
	if !ok {
		return "", fmt.Errorf("client key is %T, want ed25519", cs.PeerCertificates[0].PublicKey)
	}
	return identity.FingerprintOf(pub), nil
}

func fingerprintOfDER(raw [][]byte) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("peer presented no certificate")
	}
	crt, err := x509.ParseCertificate(raw[0])
	if err != nil {
		return "", fmt.Errorf("parse peer certificate: %w", err)
	}
	pub, ok := crt.PublicKey.(ed25519.PublicKey)
	if !ok {
		return "", fmt.Errorf("peer key is %T, want ed25519", crt.PublicKey)
	}
	return identity.FingerprintOf(pub), nil
}

/*
 * Client is an HTTP client that will talk to exactly one peer.
 *
 * One client per peer rather than one shared client, because the pin is baked
 * into the transport: a shared client would need the fingerprint at call time,
 * and a call site that can pass the wrong pin is a call site that eventually
 * does.
 *
 * The timeout is short. Everything asked over this connection is a question
 * about *now* — is the peer up, what is somebody watching — and a slow answer
 * to that is not a late answer, it is a wrong one.
 */
func Client(ident identity.Identity, fingerprint string) (*http.Client, error) {
	cfg, err := ClientConfig(ident, fingerprint)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     cfg,
			ForceAttemptHTTP2:   false,
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     30 * time.Second,
		},
	}, nil
}
