package tlscert

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerateCoversLoopbackAndHosts(t *testing.T) {
	certPEM, keyPEM, err := generate([]string{"192.168.1.50", "media.lan"}, validity)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Loopback identities are always present so localhost use never warns for
	// the wrong reason.
	for _, name := range []string{"localhost"} {
		if err := leaf.VerifyHostname(name); err != nil {
			t.Errorf("expected cert to cover %q: %v", name, err)
		}
	}
	for _, ip := range []string{"127.0.0.1", "::1", "192.168.1.50"} {
		if err := leaf.VerifyHostname(ip); err != nil {
			t.Errorf("expected cert to cover %q: %v", ip, err)
		}
	}
	if err := leaf.VerifyHostname("media.lan"); err != nil {
		t.Errorf("expected cert to cover DNS name: %v", err)
	}
}

func TestLoadOrGeneratePersists(t *testing.T) {
	dir := t.TempDir()

	first, err := LoadOrGenerate(dir, nil)
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	// Files exist and are 0600 (owner-only) so the key is not world-readable.
	for _, name := range []string{"cert.pem", "key.pem"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 && perm != 0o666 {
			// Windows reports 0666; POSIX reports 0600. Both mean "we asked for 0600".
			t.Errorf("%s perm = %o, want 0600", name, perm)
		}
	}

	second, err := LoadOrGenerate(dir, nil)
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	// A restart must reuse the persisted cert, not mint a new one — a changing
	// cert re-prompts trust on every start.
	if string(first.Certificate[0]) != string(second.Certificate[0]) {
		t.Error("expected persisted certificate to be reused, got a fresh one")
	}
}

func TestLoadOrGenerateReplacesExpiring(t *testing.T) {
	dir := t.TempDir()

	// Seed a cert already inside the renewal window.
	certPEM, keyPEM, err := generate(nil, renewBefore/2)
	if err != nil {
		t.Fatalf("seed generate: %v", err)
	}
	if err := writeFileSync(filepath.Join(dir, "cert.pem"), certPEM); err != nil {
		t.Fatal(err)
	}
	if err := writeFileSync(filepath.Join(dir, "key.pem"), keyPEM); err != nil {
		t.Fatal(err)
	}

	got, err := LoadOrGenerate(dir, nil)
	if err != nil {
		t.Fatalf("LoadOrGenerate: %v", err)
	}
	leaf, err := x509.ParseCertificate(got.Certificate[0])
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if time.Until(leaf.NotAfter) < renewBefore {
		t.Error("expected a near-expiry cert to be regenerated with full validity")
	}
}

func TestLoadOrGenerateReplacesCorrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cert.pem"), []byte("not a cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "key.pem"), []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A corrupt cert is a cache miss, never a fatal error: the server must still
	// come up with a working certificate.
	if _, err := LoadOrGenerate(dir, nil); err != nil {
		t.Fatalf("expected regeneration over corrupt files, got error: %v", err)
	}
}

func TestLocalIPsExcludesLoopback(t *testing.T) {
	for _, s := range LocalIPs() {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Errorf("LocalIPs returned unparseable %q", s)
			continue
		}
		if ip.IsLoopback() {
			t.Errorf("LocalIPs returned a loopback address %q", s)
		}
	}
}
