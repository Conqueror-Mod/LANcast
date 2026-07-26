// Package tlscert provides a self-signed certificate for LANcast when the
// operator has not supplied their own. It encrypts the wire so the password and
// session cookie no longer travel in plaintext on a semi-trusted LAN; it does
// not authenticate the server. See ADR 0014.
//
// The certificate is persisted so that trust granted on first use survives a
// restart. A cert that changes on every start is a fresh "do you trust this?"
// prompt each time and is precisely what makes self-signed miserable.
package tlscert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// validity is how long a generated certificate lasts. Long, because the failure
// mode of expiry is a new browser warning to re-accept, and re-accepting it
// yearly is friction with no security benefit for a private cert.
const validity = 10 * 365 * 24 * time.Hour

// renewBefore regenerates a cert this long ahead of its expiry, so a
// long-running server does not one day serve an expired cert.
const renewBefore = 30 * 24 * time.Hour

// LoadOrGenerate returns a certificate from dir, generating and persisting a new
// self-signed one if none exists or the existing one is missing, unreadable, or
// near expiry. cert.pem and key.pem are written 0600 under dir.
//
// hosts are the names and IPs the certificate should cover, in addition to the
// loopback identities that are always included.
func LoadOrGenerate(dir string, hosts []string) (tls.Certificate, error) {
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	if cert, ok := loadValid(certPath, keyPath); ok {
		return cert, nil
	}

	certPEM, keyPEM, err := generate(hosts, validity)
	if err != nil {
		return tls.Certificate{}, err
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return tls.Certificate{}, fmt.Errorf("create tls dir: %w", err)
	}
	if err := writeFileSync(certPath, certPEM); err != nil {
		return tls.Certificate{}, err
	}
	if err := writeFileSync(keyPath, keyPEM); err != nil {
		return tls.Certificate{}, err
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("parse generated keypair: %w", err)
	}
	return cert, nil
}

// loadValid loads the persisted keypair and reports whether it is usable and not
// near expiry. Any problem — missing files, a corrupt PEM, an aging cert — is a
// cache miss that triggers regeneration, never an error the server dies on.
func loadValid(certPath, keyPath string) (tls.Certificate, bool) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, false
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return tls.Certificate{}, false
	}
	if time.Now().Add(renewBefore).After(leaf.NotAfter) {
		return tls.Certificate{}, false
	}
	return cert, true
}

// generate produces a self-signed certificate and key in PEM form covering the
// given hosts plus the loopback identities. It is pure: no filesystem, no
// network, so it is exercised directly by tests.
func generate(hosts []string, validFor time.Duration) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial: %w", err)
	}

	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{Organization: []string{"LANcast"}, CommonName: "LANcast"},
		NotBefore:             now.Add(-time.Hour), // tolerate minor clock skew between server and client
		NotAfter:              now.Add(validFor),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	for _, h := range dedupeHosts(hosts) {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create certificate: %w", err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal key: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

// dedupeHosts prepends the loopback identities every LANcast cert must carry and
// removes duplicates and blanks, preserving first-seen order.
func dedupeHosts(hosts []string) []string {
	all := append([]string{"localhost", "127.0.0.1", "::1"}, hosts...)
	seen := make(map[string]bool, len(all))
	out := make([]string, 0, len(all))
	for _, h := range all {
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	return out
}

// LocalIPs returns the machine's non-loopback IP addresses, so a generated cert
// covers the address a browser on the LAN actually connects to. A failure to
// enumerate is not fatal: the cert still covers loopback, and the operator can
// supply their own cert with the right names.
func LocalIPs() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []string
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		out = append(out, ip.String())
	}
	return out
}

// writeFileSync writes 0600 via a temp file and rename, so a crash mid-write
// never leaves a truncated cert or key that would wedge startup.
func writeFileSync(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	return nil
}
