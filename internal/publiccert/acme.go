package publiccert

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/acme"
)

// acmeIssuer is the Issuer that talks to a real CA, with x/crypto/acme
// (ADR 0081 §2): already a dependency, and unlike autocert it lets the CSR be
// signed with a key we choose.
type acmeIssuer struct {
	client *acme.Client
	email  string
}

func newACMEIssuer(cfg Config, accountPath string) (Issuer, error) {
	dir, err := cfg.directory()
	if err != nil {
		return nil, err
	}
	key, err := loadOrCreateAccountKey(accountPath)
	if err != nil {
		return nil, fmt.Errorf("ACME account key: %w", err)
	}
	return &acmeIssuer{
		client: &acme.Client{Key: key, DirectoryURL: dir, UserAgent: "LANcast"},
		email:  cfg.Email,
	}, nil
}

// Issue runs one order to completion: account, authorization by
// TLS-ALPN-01, then a certificate for a CSR signed with key.
func (a *acmeIssuer) Issue(ctx context.Context, host string, key crypto.Signer,
	present func(*tls.Certificate), withdraw func()) ([][]byte, error) {
	// Bounded: a CA that never answers must not hold the renewal loop for ever.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	acct := &acme.Account{}
	if a.email != "" {
		acct.Contact = []string{"mailto:" + a.email}
	}
	// The admin ticked the CA's agreement in Settings before this could run
	// (ADR 0081 §1); AcceptTOS records that answer, it does not invent one.
	if _, err := a.client.Register(ctx, acct, acme.AcceptTOS); err != nil &&
		!errors.Is(err, acme.ErrAccountAlreadyExists) {
		return nil, fmt.Errorf("register with the CA: %w", err)
	}

	order, err := a.client.AuthorizeOrder(ctx, acme.DomainIDs(host))
	if err != nil {
		return nil, fmt.Errorf("start an order: %w", err)
	}
	for _, u := range order.AuthzURLs {
		if err := a.authorize(ctx, u, host, present, withdraw); err != nil {
			return nil, err
		}
	}
	if order, err = a.client.WaitOrder(ctx, order.URI); err != nil {
		return nil, fmt.Errorf("wait for the order: %w", err)
	}

	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: host},
		DNSNames: []string{host},
	}, key)
	if err != nil {
		return nil, fmt.Errorf("build the certificate request: %w", err)
	}
	der, _, err := a.client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return nil, fmt.Errorf("finalize the order: %w", err)
	}
	return der, nil
}

func (a *acmeIssuer) authorize(ctx context.Context, url, host string,
	present func(*tls.Certificate), withdraw func()) error {
	z, err := a.client.GetAuthorization(ctx, url)
	if err != nil {
		return fmt.Errorf("read the authorization: %w", err)
	}
	if z.Status == acme.StatusValid {
		return nil
	}
	var chal *acme.Challenge
	for _, c := range z.Challenges {
		if c.Type == "tls-alpn-01" {
			chal = c
			break
		}
	}
	if chal == nil {
		return errors.New("the CA did not offer a tls-alpn-01 challenge")
	}
	cert, err := a.client.TLSALPN01ChallengeCert(chal.Token, host)
	if err != nil {
		return fmt.Errorf("build the challenge certificate: %w", err)
	}
	present(&cert)
	defer withdraw()

	if _, err := a.client.Accept(ctx, chal); err != nil {
		return fmt.Errorf("ask the CA to validate: %w", err)
	}
	if _, err := a.client.WaitAuthorization(ctx, url); err != nil {
		// The CA's words are the useful part: it says what it reached, and
		// "connection refused on 443" is the answer to most first attempts.
		return fmt.Errorf("the CA could not validate %s on port 443: %w", host, err)
	}
	return nil
}

// loadOrCreateAccountKey keeps the ACME account key apart from the serving key
// (ADR 0081 §4), so losing one does not compromise the other.
func loadOrCreateAccountKey(path string) (crypto.Signer, error) {
	if raw, err := os.ReadFile(path); err == nil {
		b, _ := pem.Decode(raw)
		if b == nil {
			return nil, errors.New("account key file holds no key")
		}
		return x509.ParseECPrivateKey(b.Bytes)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}
