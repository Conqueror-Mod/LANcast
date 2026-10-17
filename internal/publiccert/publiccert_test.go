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
	"errors"
	"io"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const host = "example.duckdns.org"

var t0 = time.Date(2026, 10, 11, 20, 0, 0, 0, time.UTC)

// selfSigned stands in for the server's existing serving certificate.
func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "LANcast"},
		NotBefore: t0.Add(-time.Hour), NotAfter: t0.Add(10 * 365 * 24 * time.Hour),
		DNSNames: []string{"localhost"}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// fakeCA issues for whatever key it is handed, as a real CA does with a CSR,
// unless told to fail or to issue for some other key.
type fakeCA struct {
	calls     int
	fail      error
	wrongKey  bool
	lifetime  time.Duration
	now       func() time.Time
	presented bool
}

func (f *fakeCA) Issue(_ context.Context, h string, key crypto.Signer,
	present func(*tls.Certificate), withdraw func()) ([][]byte, error) {
	f.calls++
	present(&tls.Certificate{})
	f.presented = true
	withdraw()
	if f.fail != nil {
		return nil, f.fail
	}
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pub := key.Public()
	if f.wrongKey {
		other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		pub = other.Public()
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(int64(f.calls + 1)),
		NotBefore: f.now(), NotAfter: f.now().Add(f.lifetime), DNSNames: []string{h}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, caKey)
	if err != nil {
		return nil, err
	}
	return [][]byte{der}, nil
}

func newTestManager(t *testing.T) (*Manager, *fakeCA, *time.Time) {
	t.Helper()
	clock := t0
	ca := &fakeCA{lifetime: 90 * 24 * time.Hour, now: func() time.Time { return clock }}
	m, err := New(filepath.Join(t.TempDir(), "acme"), selfSigned(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return clock }
	m.issuer = func(Config, string) (Issuer, error) { return ca, nil }
	return m, ca, &clock
}

func enabled() Config { return Config{Enabled: true, Hostname: host, CA: "letsencrypt-staging"} }

func served(t *testing.T, m *Manager, sni string) *tls.Certificate {
	t.Helper()
	c, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: sni})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestOffServesTheExistingCertificateAndAsksNobody(t *testing.T) {
	m, ca, _ := newTestManager(t)
	m.attempt(context.Background())
	if ca.calls != 0 {
		t.Fatalf("the CA was contacted %d times while the opt-in was off", ca.calls)
	}
	if served(t, m, host) != m.fallback {
		t.Fatal("off, but the configured name did not get the existing certificate")
	}
	if _, ok := m.nextWait(); ok {
		t.Fatal("off, but the renewal loop has something scheduled")
	}
}

// ADR 0081 §3: only a client that named the host gets the public certificate.
func TestOnlyTheConfiguredNameGetsThePublicCertificate(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.Configure(enabled())
	m.attempt(context.Background())

	if c := served(t, m, host); c == m.fallback {
		t.Fatal("the configured name still got the self-signed certificate")
	}
	if c := served(t, m, "EXAMPLE.duckdns.org."); c == m.fallback {
		t.Fatal("SNI is case-insensitive and may carry a trailing dot")
	}
	for _, sni := range []string{"", "192.168.1.20", "localhost", "other.duckdns.org"} {
		if served(t, m, sni) != m.fallback {
			t.Errorf("SNI %q got the public certificate; it must keep the self-signed one", sni)
		}
	}
}

// ADR 0081 §4: issued for the serving key, so a pinned client keeps trusting.
func TestThePublicCertificateUsesTheServingKey(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.Configure(enabled())
	m.attempt(context.Background())
	c := served(t, m, host)
	if !samePublicKey(c.Leaf.PublicKey, m.fallback.PrivateKey.(crypto.Signer).Public()) {
		t.Fatal("the public certificate is not for the serving key; every pinned client would break")
	}
}

func TestACertificateForAnotherKeyIsNeverServed(t *testing.T) {
	m, ca, _ := newTestManager(t)
	ca.wrongKey = true
	m.Configure(enabled())
	m.attempt(context.Background())
	if served(t, m, host) != m.fallback {
		t.Fatal("served a certificate for a key this server does not hold")
	}
	if st := m.Status(); st.State != StateFailing || st.LastError == "" {
		t.Fatalf("status %+v; a refused certificate must show as a failure with a reason", st)
	}
}

func TestAnExpiredCertificateFallsBack(t *testing.T) {
	m, _, clock := newTestManager(t)
	m.Configure(enabled())
	m.attempt(context.Background())
	*clock = clock.Add(91 * 24 * time.Hour)
	if served(t, m, host) != m.fallback {
		t.Fatal("served an expired public certificate")
	}
}

func TestRenewalIsDueWithAThirdOfTheLifetimeLeft(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.Configure(enabled())
	m.attempt(context.Background())
	wait, ok := m.nextWait()
	if !ok || wait != 60*24*time.Hour {
		t.Fatalf("next attempt in %v, want 60 days into a 90-day certificate", wait)
	}
}

func TestAFailureKeepsTheCurrentCertificateAndBacksOff(t *testing.T) {
	m, ca, clock := newTestManager(t)
	m.Configure(enabled())
	m.attempt(context.Background())
	good := served(t, m, host)

	*clock = clock.Add(61 * 24 * time.Hour)
	ca.fail = errors.New("connection refused on 443")
	for i, want := range []time.Duration{time.Hour, 2 * time.Hour, 4 * time.Hour} {
		m.attempt(context.Background())
		if served(t, m, host) != good {
			t.Fatal("a failed renewal replaced a still-valid certificate")
		}
		st := m.Status()
		if st.State != StateFailing || st.LastError != "connection refused on 443" {
			t.Fatalf("status %+v; the CA's own words must be kept", st)
		}
		if got := st.NextAttempt.Sub(*clock); got != want {
			t.Fatalf("failure %d: retry in %v, want %v", i+1, got, want)
		}
	}
}

func TestBackoffIsCappedAtADay(t *testing.T) {
	m, ca, clock := newTestManager(t)
	ca.fail = errors.New("no")
	m.Configure(enabled())
	for i := 0; i < 10; i++ {
		m.attempt(context.Background())
	}
	if got := m.Status().NextAttempt.Sub(*clock); got != maxBackoff {
		t.Fatalf("retry in %v after ten failures, want the %v cap", got, maxBackoff)
	}
}

// Pressing save is asking for an attempt now, not after a day of backoff.
func TestReconfiguringResetsTheBackoff(t *testing.T) {
	m, ca, _ := newTestManager(t)
	ca.fail = errors.New("no")
	m.Configure(enabled())
	for i := 0; i < 5; i++ {
		m.attempt(context.Background())
	}
	m.Configure(enabled())
	if wait, ok := m.nextWait(); !ok || wait != 0 {
		t.Fatalf("after saving again the next attempt is in %v, want now", wait)
	}
}

// ADR 0081 §5: off deletes the certificate and the account key.
func TestTurningItOffForgetsEverything(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.Configure(enabled())
	m.attempt(context.Background())
	if err := os.WriteFile(filepath.Join(m.dir, accountFile), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}

	m.Configure(Config{})
	if served(t, m, host) != m.fallback {
		t.Fatal("still serving the public certificate after turning it off")
	}
	for _, f := range []string{certFile, accountFile} {
		if _, err := os.Stat(filepath.Join(m.dir, f)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s survived turning the public certificate off", f)
		}
	}
	if st := m.Status(); st.State != StateOff {
		t.Fatalf("state %q after turning it off", st.State)
	}
}

func TestANewNameDropsTheOldCertificate(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.Configure(enabled())
	m.attempt(context.Background())
	cfg := enabled()
	cfg.Hostname = "other.duckdns.org"
	m.Configure(cfg)
	if m.cur.Load() != nil {
		t.Fatal("kept a certificate for a name that is no longer configured")
	}
}

// A restart picks the saved certificate up again, but only for the same key.
func TestASavedCertificateSurvivesARestartForTheSameKeyOnly(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.Configure(enabled())
	m.attempt(context.Background())

	again, err := New(m.dir, *m.fallback, m.log)
	if err != nil {
		t.Fatal(err)
	}
	if again.cur.Load() == nil {
		t.Fatal("the saved certificate was not picked up after a restart")
	}

	other, err := New(m.dir, selfSigned(t), m.log)
	if err != nil {
		t.Fatal(err)
	}
	if other.cur.Load() != nil {
		t.Fatal("picked up a saved certificate issued for a different key")
	}
}

func TestChallengeAnswersOnlyTheCAsProtocol(t *testing.T) {
	m, _, _ := newTestManager(t)
	ch := &tls.Certificate{Certificate: [][]byte{{1}}}
	m.challenges.Store(host, ch)

	if _, claimed, _ := m.ChallengeConfig(&tls.ClientHelloInfo{ServerName: host,
		SupportedProtos: []string{"h2", "http/1.1"}}); claimed {
		t.Fatal("an ordinary browser handshake was claimed by the ACME branch")
	}
	cfg, claimed, err := m.ChallengeConfig(&tls.ClientHelloInfo{ServerName: "Example.DuckDNS.org",
		SupportedProtos: []string{ALPNProto}})
	if !claimed || err != nil || len(cfg.Certificates) != 1 || cfg.NextProtos[0] != ALPNProto {
		t.Fatalf("the CA's handshake was not answered with the challenge: %v %v %v", cfg, claimed, err)
	}
	if _, claimed, err := m.ChallengeConfig(&tls.ClientHelloInfo{ServerName: "other.example",
		SupportedProtos: []string{ALPNProto}}); !claimed || err == nil {
		t.Fatal("acme-tls/1 with nothing pending must be refused, not served the site")
	}
}

func TestNormalizeHostname(t *testing.T) {
	ok := map[string]string{
		"example.duckdns.org":    "example.duckdns.org",
		" Example.DuckDNS.org. ": "example.duckdns.org",
		"a-b.example.net":        "a-b.example.net",
	}
	for in, want := range ok {
		if got, err := NormalizeHostname(in); err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "203.0.113.5", "2001:db8::1", "localhost", "aither",
		"https://example.duckdns.org", "example.duckdns.org:8080", "*.example.net",
		"-bad.example.net", "bad_.example.net", "ex ample.net"} {
		if _, err := NormalizeHostname(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// Port 443 opens with the certificate and closes with it (ADR 0081 §2,
// amended): IPv6 has no port translation, so the server answers there itself.
func TestPort443FollowsTheCertificate(t *testing.T) {
	m, _, _ := newTestManager(t)
	var calls []bool
	m.SetPortHook(func(on bool) error { calls = append(calls, on); return nil })

	m.Configure(enabled())
	if st := m.Status(); !st.Listening443 {
		t.Fatalf("status %+v; 443 should be open while the certificate is on", st)
	}
	m.Configure(Config{})
	if st := m.Status(); st.Listening443 {
		t.Fatalf("status %+v; 443 should close with the certificate", st)
	}
	if len(calls) != 2 || !calls[0] || calls[1] {
		t.Fatalf("hook calls %v, want [true false]", calls)
	}
}

func TestAPortThatWillNotOpenIsReportedNotFatal(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.SetPortHook(func(on bool) error {
		if on {
			return errors.New("cannot listen on :443: address already in use")
		}
		return nil
	})
	m.Configure(enabled())
	st := m.Status()
	if st.Listening443 || st.Port443Error == "" {
		t.Fatalf("status %+v; the reason must be shown", st)
	}
	if st.State == StateOff {
		t.Fatal("a port problem turned the certificate off")
	}
}

// ADR 0088: with a custom certificate configured, every name but the public
// one goes to it, and the public certificate is still for the generated key.
func TestEveryOtherNameGoesToTheFallbackChooser(t *testing.T) {
	m, _, _ := newTestManager(t)
	custom := selfSigned(t)
	m.SetFallback(func(*tls.ClientHelloInfo) *tls.Certificate { return &custom })
	m.Configure(enabled())
	m.attempt(context.Background())

	if c := served(t, m, host); c == &custom || c == m.fallback {
		t.Fatal("the configured name did not get the public certificate")
	}
	if served(t, m, "192.168.1.20") != &custom {
		t.Error("another name did not go to the chooser")
	}
	if !samePublicKey(served(t, m, host).Leaf.PublicKey, m.fallback.PrivateKey.(crypto.Signer).Public()) {
		t.Error("the public certificate is not for the generated key")
	}
}
