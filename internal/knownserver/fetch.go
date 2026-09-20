package knownserver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"time"

	"lancast/internal/certpin"
)

// dialTimeout bounds the one network act in this package. A server that is not
// there should be reported as not there quickly: the person is standing in
// front of a dialog they just pressed a button on.
const dialTimeout = 6 * time.Second

/*
 * Offered is what a server at an address is currently presenting.
 *
 * Deliberately not a decision. This type says what was seen; List.Check says
 * what it means. Keeping those apart is what lets the meaning be tested
 * without a server and the seeing be tested with one.
 */
type Offered struct {
	// Pin is the SPKI of the certificate the server presented.
	Pin string
	// Subject and Issuer are shown beside the fingerprint, because a
	// self-signed LANcast certificate says "CN=LANcast, O=LANcast" and a
	// person being asked to trust a key is entitled to see whether the thing
	// answering even claims to be one.
	Subject string
	Issuer  string
	// NotAfter is the certificate's expiry. A certificate already expired is
	// not refused here -- the pin is over the key and LANcast issues its own
	// certificates -- but it is worth showing.
	NotAfter time.Time
}

/*
 * Fetch asks what key a server is offering, without trusting it.
 *
 * InsecureSkipVerify is correct here and nowhere else in this project, and the
 * distinction is the whole reason this is a named function rather than an
 * option on a client. It is not "connect insecurely": nothing is sent over
 * this connection and no response is read from it. The handshake is performed
 * only far enough to be handed the certificate, which is then shown to a
 * person before anything is trusted. Verification cannot be the thing that
 * decides, because a self-signed certificate fails verification by
 * construction -- that is what it means to be the first connection to a
 * LANcast server ([ADR 0014]).
 *
 * Everything afterwards uses the pin. This function is the one moment the
 * client looks at an unverified certificate, and it looks at it in order to
 * ask.
 *
 * [ADR 0014]: ../../docs/adr/0014-transport-security.md
 */
func Fetch(ctx context.Context, address string) (Offered, error) {
	if address == "" {
		return Offered{}, ErrNoAddress
	}
	d := &net.Dialer{Timeout: dialTimeout}
	conn, err := tls.DialWithDialer(d, "tcp", address, &tls.Config{
		// See the comment above. The certificate is inspected, not trusted.
		InsecureSkipVerify: true, //nolint:gosec // the point of this function
		MinVersion:         tls.VersionTLS12,
	})
	if err != nil {
		return Offered{}, fmt.Errorf("reach %s: %w", address, err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if err := conn.HandshakeContext(ctx); err != nil {
		return Offered{}, fmt.Errorf("reach %s: %w", address, err)
	}

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return Offered{}, errors.New("the server offered no certificate")
	}
	leaf := certs[0]
	return Offered{
		Pin:      certpin.SPKIFromDER(leaf.RawSubjectPublicKeyInfo),
		Subject:  leaf.Subject.String(),
		Issuer:   leaf.Issuer.String(),
		NotAfter: leaf.NotAfter,
	}, nil
}
