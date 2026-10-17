package tlscert

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

/*
 * Serving is the certificate this server hands to the network (ADR 0088).
 *
 * The operator's own when one is configured and loads; the generated one
 * otherwise. Three rules make a certificate somebody else maintains safe to
 * depend on:
 *
 *   - **It is followed.** The two files are re-read when they change, so a
 *     renewal tool rewriting them in place is picked up without a restart.
 *   - **It cannot stop the server.** A file that is missing, unreadable,
 *     mismatched or out of date means the generated certificate is served
 *     and the reason is reported. Still HTTPS; a browser warning, not a
 *     server that will not start, which a setting changed in the app must
 *     never be able to cause.
 *   - **This machine keeps the generated one.** A connection arriving on a
 *     loopback address is the desktop window on this PC, which trusts the
 *     server by pinning the generated certificate's key read off disk. A
 *     custom certificate there would be a security wall inside a native
 *     window. Other devices are who the custom certificate is for.
 */
type Serving struct {
	generated *tls.Certificate
	log       *slog.Logger
	now       func() time.Time

	mu       sync.Mutex
	certFile string
	keyFile  string
	stamp    string    // the two files' sizes and times when last read
	checked  time.Time // when the files were last looked at
	custom   *tls.Certificate
	err      error
}

// recheckEvery is how often the files are looked at for a renewal. A stat of
// two files, at most twice a minute, on the handshake path.
const recheckEvery = 30 * time.Second

// NewServing serves generated until Configure names a certificate of the
// operator's own.
func NewServing(generated tls.Certificate, log *slog.Logger) *Serving {
	if generated.Leaf == nil && len(generated.Certificate) > 0 {
		generated.Leaf, _ = x509.ParseCertificate(generated.Certificate[0])
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Serving{generated: &generated, log: log, now: time.Now}
}

// Configure names the operator's certificate and key, or neither for the
// generated certificate. It never fails: a pair that will not load is served
// around and reported by Status.
func (s *Serving) Configure(certFile, keyFile string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.certFile, s.keyFile = certFile, keyFile
	s.custom, s.err, s.stamp = nil, nil, ""
	switch {
	case certFile == "" && keyFile == "":
		return
	case certFile == "" || keyFile == "":
		s.err = errors.New("both a certificate file and a key file are needed")
		return
	}
	s.reloadLocked()
}

// For is the certificate for one handshake.
func (s *Serving) For(hello *tls.ClientHelloInfo) *tls.Certificate {
	if onLoopback(hello) {
		return s.generated
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshLocked()
	if s.custom != nil {
		return s.custom
	}
	return s.generated
}

// GetCertificate is For in the shape tls.Config wants.
func (s *Serving) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return s.For(hello), nil
}

// ServingStatus is what the Network pane shows about the certificate other
// devices are served.
type ServingStatus struct {
	// CertFile and KeyFile are what is configured, empty for none.
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
	// Serving is "custom" or "generated": which one other devices get.
	Serving string `json:"serving"`
	// Error is why the configured certificate is not the one being served,
	// or why a newer copy of its files was not taken. Empty when all is well.
	Error string `json:"error,omitempty"`
	CertInfo
}

// CertInfo describes one certificate.
type CertInfo struct {
	Subject     string    `json:"subject"`
	Issuer      string    `json:"issuer"`
	Names       []string  `json:"names"`
	NotAfter    time.Time `json:"not_after"`
	SelfSigned  bool      `json:"self_signed"`
	Fingerprint string    `json:"fingerprint"`
}

// Status reports what is configured and what is being served.
func (s *Serving) Status() ServingStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshLocked()
	out := ServingStatus{CertFile: s.certFile, KeyFile: s.keyFile, Serving: "generated"}
	served := s.generated
	if s.custom != nil {
		served, out.Serving = s.custom, "custom"
	}
	if s.err != nil {
		out.Error = s.err.Error()
	}
	if served.Leaf != nil {
		out.CertInfo = Describe(served.Leaf)
	}
	return out
}

// reloadLocked reads the files now. A failure keeps a previous good copy
// while it is still in date: a renewal tool caught halfway through writing is
// the likeliest reason, and the next look will see the finished files.
func (s *Serving) reloadLocked() {
	s.checked = s.now()
	s.stamp = stampOf(s.certFile, s.keyFile)
	c, err := LoadCustom(s.certFile, s.keyFile, s.now())
	if err != nil {
		if s.custom != nil && !s.now().Before(s.custom.Leaf.NotAfter) {
			s.custom = nil
		}
		if s.err == nil || s.err.Error() != err.Error() {
			s.log.Warn("custom certificate not loaded", "cert", s.certFile, "error", err,
				"serving", map[bool]string{true: "previous copy", false: "generated"}[s.custom != nil])
		}
		s.err = err
		return
	}
	if s.custom == nil || !bytes.Equal(s.custom.Certificate[0], c.Certificate[0]) {
		s.log.Info("custom certificate loaded", "cert", s.certFile,
			"subject", c.Leaf.Subject.CommonName, "expires", c.Leaf.NotAfter.UTC().Format(time.DateOnly))
	}
	s.custom, s.err = &c, nil
}

// refreshLocked looks at the files when it has been a while: again whenever
// they changed, and again regardless while nothing good is loaded, since a
// certificate not valid *yet* becomes valid without its file changing.
func (s *Serving) refreshLocked() {
	if s.certFile == "" || s.keyFile == "" || s.now().Sub(s.checked) < recheckEvery {
		return
	}
	s.checked = s.now()
	if s.custom == nil || stampOf(s.certFile, s.keyFile) != s.stamp {
		s.reloadLocked()
		return
	}
	if !s.now().Before(s.custom.Leaf.NotAfter) {
		s.err = fmt.Errorf("the certificate expired on %s", s.custom.Leaf.NotAfter.UTC().Format(time.DateOnly))
		s.custom = nil
	}
}

/*
 * LoadCustom reads an operator's certificate and key and checks them the way
 * a browser will: a pair, and in date. It is also what the settings route
 * runs before saving, so an admin hears what is wrong while they are still
 * looking at the form.
 */
func LoadCustom(certFile, keyFile string, now time.Time) (tls.Certificate, error) {
	if !filepath.IsAbs(certFile) || !filepath.IsAbs(keyFile) {
		return tls.Certificate{}, errors.New("the certificate and key must each be a full path")
	}
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("the certificate file can't be read: %w", err)
	}
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("the key file can't be read: %w", err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("the certificate and key aren't a usable pair: %w", err)
	}
	if cert.Leaf == nil {
		if cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0]); err != nil {
			return tls.Certificate{}, fmt.Errorf("the certificate can't be read: %w", err)
		}
	}
	if now.Before(cert.Leaf.NotBefore) {
		return tls.Certificate{}, fmt.Errorf("the certificate isn't valid until %s",
			cert.Leaf.NotBefore.UTC().Format(time.DateOnly))
	}
	if !now.Before(cert.Leaf.NotAfter) {
		return tls.Certificate{}, fmt.Errorf("the certificate expired on %s",
			cert.Leaf.NotAfter.UTC().Format(time.DateOnly))
	}
	return cert, nil
}

// Describe is what a person needs to recognise a certificate.
func Describe(leaf *x509.Certificate) CertInfo {
	names := append([]string{}, leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		names = append(names, ip.String())
	}
	return CertInfo{
		Subject:     nameOf(leaf.Subject.CommonName, leaf.Subject.Organization),
		Issuer:      nameOf(leaf.Issuer.CommonName, leaf.Issuer.Organization),
		Names:       names,
		NotAfter:    leaf.NotAfter.UTC(),
		SelfSigned:  bytes.Equal(leaf.RawIssuer, leaf.RawSubject),
		Fingerprint: spki(leaf.RawSubjectPublicKeyInfo),
	}
}

func nameOf(cn string, org []string) string {
	if cn != "" {
		return cn
	}
	if len(org) > 0 {
		return org[0]
	}
	return ""
}

func stampOf(paths ...string) string {
	var b bytes.Buffer
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil {
			fmt.Fprintf(&b, "%d:%d;", fi.Size(), fi.ModTime().UnixNano())
		} else {
			b.WriteString("-;")
		}
	}
	return b.String()
}

// onLoopback is a connection that arrived on a loopback address: this machine.
func onLoopback(hello *tls.ClientHelloInfo) bool {
	if hello == nil || hello.Conn == nil {
		return false
	}
	a, ok := hello.Conn.LocalAddr().(*net.TCPAddr)
	return ok && a.IP.IsLoopback()
}

// spki is certpin.SPKIFromDER, which this package cannot import (certpin's
// tests import tlscert). The two must agree or a trusted server reads as an
// impostor; TestTheFingerprintIsCertpinsPin holds them together.
func spki(der []byte) string {
	sum := sha256.Sum256(der)
	return base64.StdEncoding.EncodeToString(sum[:])
}
