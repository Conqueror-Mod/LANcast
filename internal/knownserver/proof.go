package knownserver

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"

	"lancast/internal/certpin"
	identitykey "lancast/internal/identity"
)

/*
 * Asking a server to prove it is the one on record (ADR 0124).
 *
 * This is the answer to review finding R11-1. When the serving key at a known
 * address changes, a person used to be asked to compare the identity this
 * client had on record with the one the operator read out -- two copies of
 * one number, which matched whoever was answering. Here the server is asked
 * to sign a challenge with its identity key instead, over the very connection
 * that presented the new key, and the signature is checked against the
 * identity on record. Nobody has to compare anything.
 */

// proofPath is the endpoint. Unauthenticated by design; ADR 0124 says why.
const proofPath = "/api/identity/proof"

// maxProofBody bounds what is read from a server not yet trusted. A real
// answer is about 150 bytes.
const maxProofBody = 4 << 10

var (
	// ErrNoProof is a server that offered none: an older server, one behind
	// a proxy that terminates TLS, or something that will not try. Never a
	// reason to accept; the caller falls back to asking a person.
	ErrNoProof = errors.New("the server did not offer a proof of its identity")
	// ErrProofFailed is a server that answered for the identity and could not
	// prove it. The strongest refusal: something at this address claims to be
	// the server on record and is not.
	ErrProofFailed = errors.New("the server could not prove it is the one you accepted")
)

type proofAnswer struct {
	PublicKey string `json:"public_key"`
	Signature string `json:"signature"`
}

/*
 * Prove asks the server at address to prove it holds the identity named by
 * fingerprint, on a connection that presents exactly pin.
 *
 * Nil only for a valid proof. ErrNoProof (wrapped) for anything that is not
 * an answer -- unreachable, a different key by the time of dialling, a 404, a
 * session with no exportable keying material. ErrProofFailed (wrapped) for an
 * answer that does not hold.
 *
 * The pin check is part of the proof, not a nicety: the signature is over
 * this connection's keying material, and this connection is only worth
 * proving if it is the key the caller is about to store.
 */
func Prove(ctx context.Context, address, pin, fingerprint string) error {
	if address == "" {
		return ErrNoAddress
	}
	if pin == "" || identitykey.Normalize(fingerprint) == "" {
		return fmt.Errorf("%w: nothing to prove against", ErrNoProof)
	}

	d := &net.Dialer{Timeout: dialTimeout}
	raw, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("%w: reach %s: %v", ErrNoProof, address, err)
	}
	defer raw.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(deadline)
	}

	conn := tls.Client(raw, &tls.Config{
		// Not "insecure": verified below, by key rather than by a CA, which
		// is what every connection this client makes to a LANcast server
		// does (ADR 0070). A self-signed certificate fails CA verification
		// by construction.
		InsecureSkipVerify: true, //nolint:gosec // pinned in VerifyConnection
		MinVersion:         tls.VersionTLS12,
		NextProtos:         []string{"http/1.1"},
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 ||
				certpin.SPKIFromDER(cs.PeerCertificates[0].RawSubjectPublicKeyInfo) != pin {
				return errors.New("the key at this address is not the one being checked")
			}
			return nil
		},
	})
	if err := conn.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrNoProof, err)
	}

	state := conn.ConnectionState()
	// Refused by Go for a TLS 1.2 session without the extended master
	// secret, which is exactly the case where an export could be shared by
	// two sessions. No export, no proof.
	ekm, err := state.ExportKeyingMaterial(identitykey.ProofExporterLabel, nil, identitykey.ProofEKMSize)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoProof, err)
	}

	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("%w: %v", ErrNoProof, err)
	}
	q := url.Values{}
	q.Set("nonce", base64.RawURLEncoding.EncodeToString(nonce))
	q.Set("fingerprint", identitykey.Normalize(fingerprint))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://"+address+proofPath+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoProof, err)
	}
	req.Close = true
	if err := req.Write(conn); err != nil {
		return fmt.Errorf("%w: %v", ErrNoProof, err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoProof, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s answered %d", ErrNoProof, address, resp.StatusCode)
	}

	var ans proofAnswer
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxProofBody)).Decode(&ans); err != nil {
		return fmt.Errorf("%w: unreadable answer: %v", ErrProofFailed, err)
	}
	pub, err1 := base64.StdEncoding.DecodeString(ans.PublicKey)
	sig, err2 := base64.StdEncoding.DecodeString(ans.Signature)
	if err1 != nil || err2 != nil {
		return fmt.Errorf("%w: malformed answer", ErrProofFailed)
	}
	if err := identitykey.VerifyProof(fingerprint, pub, nonce, ekm, sig); err != nil {
		return fmt.Errorf("%w: %v", ErrProofFailed, err)
	}
	return nil
}
