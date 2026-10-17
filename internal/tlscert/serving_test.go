package tlscert

import (
	"crypto/tls"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lancast/internal/certpin"
)

/*
 * The operator's own certificate (ADR 0088): followed when its files change,
 * served around when it cannot be used, and never handed to this machine's
 * own window, which pins the generated key.
 */

// addrConn is a connection that arrived on local.
type addrConn struct {
	net.Conn
	local net.Addr
}

func (c addrConn) LocalAddr() net.Addr { return c.local }

func helloOn(ip string) *tls.ClientHelloInfo {
	return &tls.ClientHelloInfo{Conn: addrConn{local: &net.TCPAddr{IP: net.ParseIP(ip), Port: 8080}}}
}

var fromLAN = helloOn("192.168.1.20")

// pair writes a fresh certificate and key under dir and returns their paths.
func pair(t *testing.T, dir, name string) (certFile, keyFile string) {
	t.Helper()
	certPEM, keyPEM, err := generate([]string{name}, 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile = filepath.Join(dir, name+".crt"), filepath.Join(dir, name+".key")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

func newTestServing(t *testing.T) (*Serving, *time.Time) {
	t.Helper()
	certPEM, keyPEM, err := generate(nil, validity)
	if err != nil {
		t.Fatal(err)
	}
	gen, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	s := NewServing(gen, nil)
	now := time.Now()
	s.now = func() time.Time { return now }
	return s, &now
}

func subjectOf(c *tls.Certificate) []string { return c.Leaf.DNSNames }

func TestACustomCertificateIsServedToTheNetwork(t *testing.T) {
	s, _ := newTestServing(t)
	cf, kf := pair(t, t.TempDir(), "media.home.example")
	s.Configure(cf, kf)

	if got := s.For(fromLAN); got == s.generated || !contains(subjectOf(got), "media.home.example") {
		t.Fatalf("a LAN connection was served %v, want the custom certificate", subjectOf(got))
	}
	st := s.Status()
	if st.Serving != "custom" || st.Error != "" || !contains(st.Names, "media.home.example") || st.Fingerprint == "" {
		t.Errorf("status = %+v", st)
	}
}

// The desktop window on this PC pins the generated key; a custom certificate
// there is a security wall inside a native window.
func TestThisMachineKeepsTheGeneratedCertificate(t *testing.T) {
	s, _ := newTestServing(t)
	cf, kf := pair(t, t.TempDir(), "media.home.example")
	s.Configure(cf, kf)

	for _, ip := range []string{"127.0.0.1", "::1"} {
		if got := s.For(helloOn(ip)); got != s.generated {
			t.Errorf("a connection on %s was served %v, want the generated certificate", ip, subjectOf(got))
		}
	}
}

func TestAPairThatWillNotLoadServesTheGeneratedOneAndSaysWhy(t *testing.T) {
	dir := t.TempDir()
	certA, _ := pair(t, dir, "a.example")
	_, keyB := pair(t, dir, "b.example")
	cases := []struct {
		name, cert, key, want string
	}{
		{"mismatched", certA, keyB, "usable pair"},
		{"missing", filepath.Join(dir, "gone.crt"), keyB, "can't be read"},
		{"relative", "a.example.crt", keyB, "full path"},
		{"half", certA, "", "both"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _ := newTestServing(t)
			s.Configure(c.cert, c.key)
			if s.For(fromLAN) != s.generated {
				t.Error("served something other than the generated certificate")
			}
			if st := s.Status(); st.Serving != "generated" || !strings.Contains(st.Error, c.want) {
				t.Errorf("status = %+v, want an error mentioning %q", st, c.want)
			}
		})
	}
}

// A renewal tool rewrites the files in place; the next look takes them.
func TestARenewalIsPickedUpWithoutARestart(t *testing.T) {
	s, now := newTestServing(t)
	dir := t.TempDir()
	cf, kf := pair(t, dir, "media.home.example")
	s.Configure(cf, kf)
	before := s.For(fromLAN)

	// The renewal: a new pair written over the same paths.
	ncf, nkf := pair(t, t.TempDir(), "media.home.example")
	copyFile(t, ncf, cf)
	copyFile(t, nkf, kf)

	if s.For(fromLAN) != before {
		t.Fatal("the files were re-read before the recheck interval")
	}
	*now = now.Add(recheckEvery + time.Second)
	after := s.For(fromLAN)
	if after == before || after == s.generated {
		t.Errorf("the renewed certificate was not picked up")
	}
}

// Caught halfway through a renewal: keep serving the good copy.
func TestAHalfWrittenRenewalKeepsThePreviousCopy(t *testing.T) {
	s, now := newTestServing(t)
	cf, kf := pair(t, t.TempDir(), "media.home.example")
	s.Configure(cf, kf)
	good := s.For(fromLAN)

	if err := os.WriteFile(cf, []byte("-----BEGIN CERT"), 0o600); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(recheckEvery + time.Second)
	if s.For(fromLAN) != good {
		t.Error("a half-written file replaced a good certificate")
	}
	if st := s.Status(); st.Serving != "custom" || st.Error == "" {
		t.Errorf("status = %+v, want custom with the reason the new files were not taken", st)
	}
}

func TestAnExpiredCertificateIsServedAround(t *testing.T) {
	s, now := newTestServing(t)
	cf, kf := pair(t, t.TempDir(), "media.home.example")
	s.Configure(cf, kf)

	*now = now.Add(91 * 24 * time.Hour)
	if s.For(fromLAN) != s.generated {
		t.Error("an expired certificate was still served")
	}
	if st := s.Status(); !strings.Contains(st.Error, "expired") {
		t.Errorf("status = %+v, want it to say the certificate expired", st)
	}
}

// Not valid yet becomes valid without its file changing.
func TestACertificateNotYetValidIsTakenWhenItBecomesValid(t *testing.T) {
	s, now := newTestServing(t)
	cf, kf := pair(t, t.TempDir(), "media.home.example")
	*now = now.Add(-2 * time.Hour)
	s.Configure(cf, kf)
	if st := s.Status(); st.Serving != "generated" || !strings.Contains(st.Error, "isn't valid until") {
		t.Fatalf("status = %+v", st)
	}
	*now = now.Add(3 * time.Hour)
	if s.For(fromLAN) == s.generated {
		t.Error("the certificate was not taken once it became valid")
	}
}

func TestNothingConfiguredIsTheGeneratedCertificate(t *testing.T) {
	s, _ := newTestServing(t)
	s.Configure("", "")
	st := s.Status()
	if st.Serving != "generated" || st.Error != "" || !st.SelfSigned || s.For(fromLAN) != s.generated {
		t.Errorf("status = %+v", st)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTheFingerprintIsCertpinsPin(t *testing.T) {
	s, _ := newTestServing(t)
	if got, want := s.Status().Fingerprint, certpin.SPKIFromDER(s.generated.Leaf.RawSubjectPublicKeyInfo); got != want {
		t.Errorf("fingerprint %q, certpin says %q", got, want)
	}
}
