// Package certpin reads the public key of the server's own TLS certificate, so
// a client on the same machine can trust that one certificate and nothing else.
//
// The problem it solves: beyond loopback, LANcast serves a self-signed
// certificate ([ADR 0014](../../docs/adr/0014-transport-security.md)). A
// browser meets that with a warning and a way through. The WebView2 window the
// desktop client uses does not — it fails the handshake outright and retries,
// so the app never loads at all. Owning the window is supposed to remove that
// papercut ([ADR 0023](../../docs/adr/0023-native-desktop-client.md)); left
// alone it makes it worse.
//
// The trust here is deliberately narrow. Not "ignore certificate errors", which
// would accept anything on the LAN pretending to be the server — the exact
// attack TLS is here to stop. This pins **one public key**, read from the
// server's own key material on local disk, and every other certificate is
// validated normally.
package certpin

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNoCertificate means the server has not generated a certificate here. That
// is the normal state for a loopback-only server, which serves plain HTTP and
// needs no pin at all, so callers treat it as "nothing to do" rather than as a
// failure.
var ErrNoCertificate = errors.New("no server certificate on disk")

// CertPath is where the server keeps the certificate for a data directory.
func CertPath(dataDir string) string {
	return filepath.Join(dataDir, "tls", "cert.pem")
}

// SPKI returns the base64 SHA-256 of the certificate's SubjectPublicKeyInfo —
// the form Chromium's pinning takes.
//
// Keyed on the public key rather than the whole certificate on purpose: the
// server regenerates its certificate as expiry approaches while keeping nothing
// else about it stable, and a pin over the full DER would break on that
// rotation with no visible cause. A public key is the thing being trusted; the
// certificate is a wrapper with dates on it.
func SPKI(dataDir string) (string, error) {
	pemBytes, err := os.ReadFile(CertPath(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNoCertificate
	}
	if err != nil {
		// Notably includes permission denied: a service running as LocalSystem
		// writes this file, and a desktop user may not be able to read it. The
		// caller degrades rather than failing, so the wrapped error matters.
		return "", fmt.Errorf("read server certificate: %w", err)
	}
	return SPKIFromPEM(pemBytes)
}

// SPKIFromPEM is SPKI over bytes already in hand — the testable half, and the
// entry point for a certificate that did not come from the default location.
func SPKIFromPEM(pemBytes []byte) (string, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", errors.New("server certificate is not a PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse server certificate: %w", err)
	}
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return base64.StdEncoding.EncodeToString(sum[:]), nil
}
