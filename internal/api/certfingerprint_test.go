package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lancast/internal/certpin"
	"lancast/internal/config"
)

/*
 * The fingerprint a server shows is the one a client will be offered.
 *
 * The whole feature rests on those being the same string. If this reported
 * anything else -- a different certificate, a stale copy, the one it would
 * have generated rather than the one it serves -- then somebody comparing the
 * two would find a mismatch and conclude they were under attack, which is the
 * most expensive possible way for a field to be wrong.
 */
func TestCertFingerprintMatchesTheCertificateOnDisk(t *testing.T) {
	dir := t.TempDir()
	want := writeCert(t, certpin.CertPath(dir))

	s := &Server{dataDir: dir, settings: settingsHolding(t, config.Settings{})}

	if got := s.certFingerprint(); got != want {
		t.Errorf("fingerprint = %q, want %q", got, want)
	}
}

// A supplied certificate is the one being served, so it is the one reported.
func TestCertFingerprintFollowsASuppliedCertificate(t *testing.T) {
	dir := t.TempDir()
	// A generated certificate also present, which must NOT be the answer.
	writeCert(t, certpin.CertPath(dir))

	supplied := filepath.Join(dir, "mine.pem")
	want := writeCert(t, supplied)

	s := &Server{dataDir: dir, settings: settingsHolding(t, config.Settings{
		TLSCertFile: supplied,
		TLSKeyFile:  filepath.Join(dir, "mine.key"),
	})}

	if got := s.certFingerprint(); got != want {
		t.Errorf("fingerprint = %q, want the supplied certificate's %q", got, want)
	}
}

/*
 * A loopback-only server has no certificate, and that is ordinary.
 *
 * Empty rather than an error, because it is exactly the server nobody needs to
 * verify: nothing can reach it from another machine, which is the rule that
 * makes an unsecured server safe in the first place.
 */
func TestCertFingerprintIsEmptyWithNoCertificate(t *testing.T) {
	s := &Server{dataDir: t.TempDir(), settings: settingsHolding(t, config.Settings{})}

	if got := s.certFingerprint(); got != "" {
		t.Errorf("fingerprint = %q, want empty", got)
	}
}

// A certificate that cannot be read is reported as none rather than as a
// broken settings response: the rest of the page is still true.
func TestCertFingerprintSurvivesAnUnreadableCertificate(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tls"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certpin.CertPath(dir), []byte("not a certificate"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{dataDir: dir, settings: settingsHolding(t, config.Settings{})}

	if got := s.certFingerprint(); got != "" {
		t.Errorf("fingerprint = %q, want empty", got)
	}
}

// settingsHolding is a settings store containing one value.
func settingsHolding(t *testing.T, v config.Settings) *config.SettingsStore {
	t.Helper()
	store, err := config.LoadSettings(t.TempDir())
	if err != nil {
		t.Fatalf("settings store: %v", err)
	}
	if err := store.Set(v); err != nil {
		t.Fatalf("set settings: %v", err)
	}
	return store
}

// writeCert generates a certificate at path and returns its expected pin.
func writeCert(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "LANcast"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(
		&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certpin.SPKIFromDER(leaf.RawSubjectPublicKeyInfo)
}
