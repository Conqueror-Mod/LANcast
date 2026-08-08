package certpin

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/tlscert"
)

// generate writes a real server certificate the way the server does, so the
// test pins the same bytes production would.
func generate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := tlscert.LoadOrGenerate(filepath.Join(dir, "tls"), []string{"127.0.0.1"}); err != nil {
		t.Fatalf("generate certificate: %v", err)
	}
	return dir
}

// The pin has to match what the server actually presents on the wire, or it
// pins nothing and the window stays broken in a way no test would catch.
func TestPinMatchesTheCertificateTheServerServes(t *testing.T) {
	dir := generate(t)

	got, err := SPKI(dir)
	if err != nil {
		t.Fatalf("SPKI: %v", err)
	}

	// Recompute independently from the loaded key pair — the same path the TLS
	// listener uses to serve it.
	pair, err := tls.LoadX509KeyPair(CertPath(dir), filepath.Join(dir, "tls", "key.pem"))
	if err != nil {
		t.Fatalf("load key pair: %v", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	sum := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	want := base64.StdEncoding.EncodeToString(sum[:])

	if got != want {
		t.Errorf("pin = %q, want %q — the pin does not describe the served certificate", got, want)
	}
}

// A loopback-only server has no certificate and needs no pin. That is a state,
// not a failure, and the caller distinguishes it with errors.Is.
func TestMissingCertificateIsItsOwnAnswer(t *testing.T) {
	_, err := SPKI(t.TempDir())
	if !errors.Is(err, ErrNoCertificate) {
		t.Errorf("err = %v, want ErrNoCertificate", err)
	}
}

// Garbage in the file must not produce a pin. A wrong pin is worse than none:
// none falls back to a browser that at least offers a way through, while a
// wrong one silently trusts nothing and looks identical to the bug this fixes.
func TestGarbageIsRejectedRatherThanPinned(t *testing.T) {
	for _, content := range []string{
		"",
		"not a certificate at all",
		"-----BEGIN CERTIFICATE-----\nbm90IGEgY2VydA==\n-----END CERTIFICATE-----\n",
		"-----BEGIN PRIVATE KEY-----\nbm90IGEgY2VydA==\n-----END PRIVATE KEY-----\n",
	} {
		if _, err := SPKIFromPEM([]byte(content)); err == nil {
			t.Errorf("accepted %q as a certificate", content)
		}
	}
}

// The pin survives certificate rotation, because it is over the public key and
// the server reuses its key material when it regenerates. If this ever fails,
// the pin must move to the full certificate and the client must re-read it on
// every launch.
func TestPinIsStableAcrossReload(t *testing.T) {
	dir := generate(t)
	first, err := SPKI(dir)
	if err != nil {
		t.Fatal(err)
	}
	// LoadOrGenerate again: an existing, valid certificate is reused.
	if _, err := tlscert.LoadOrGenerate(filepath.Join(dir, "tls"), []string{"127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	second, err := SPKI(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("pin changed on reload: %q then %q", first, second)
	}
}

// An unreadable file is reported, not swallowed. A service writes this as
// LocalSystem and a desktop user may not be able to read it — the caller has to
// be able to tell that apart from "no certificate", because one is a normal
// loopback server and the other is a permission problem worth naming.
func TestUnreadableCertificateIsDistinctFromMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tls"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A directory where the file should be: reliably unreadable as a file on
	// every platform, unlike chmod which Windows largely ignores.
	if err := os.MkdirAll(CertPath(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := SPKI(dir)
	if err == nil {
		t.Fatal("expected an error reading a directory as a certificate")
	}
	if errors.Is(err, ErrNoCertificate) {
		t.Error("an unreadable certificate was reported as a missing one")
	}
}
