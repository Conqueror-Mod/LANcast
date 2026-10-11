// Package publiccert is the opt-in public certificate (ADR 0081).
//
// One hostname, chosen by an admin, gets a certificate from a public CA over
// ACME TLS-ALPN-01. Every other connection keeps the certificate it had. The
// certificate is issued for the server's existing serving key, so a client
// that pinned that key keeps trusting the server across every renewal.
//
// Off, the package does nothing at all: no request leaves the machine until an
// admin has entered a name, picked a CA and agreed to its terms.
package publiccert

import (
	"context"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ALPNProto is the protocol the CA advertises when it connects to validate a
// TLS-ALPN-01 challenge (RFC 8737).
const ALPNProto = "acme-tls/1"

// CA is one certificate authority an admin may choose. The directory URL is
// fixed here rather than accepted from the API (ADR 0081 §1).
type CA struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Directory string `json:"-"`
	// TermsURL is where the CA publishes the agreement the admin is asked to
	// accept. A static page rather than the directory's own link, because
	// fetching the directory to find it would be a request made before the
	// admin agreed to anything.
	TermsURL string `json:"terms_url"`
	// Trusted is false for a staging CA, whose root no browser trusts. The UI
	// says so, because a staging certificate still shows a warning.
	Trusted bool `json:"trusted"`
}

// KnownCAs is the whole list. Order is the order they are offered in.
var KnownCAs = []CA{
	{ID: "letsencrypt", Name: "Let's Encrypt",
		Directory: "https://acme-v02.api.letsencrypt.org/directory",
		TermsURL:  "https://letsencrypt.org/repository/", Trusted: true},
	{ID: "letsencrypt-staging", Name: "Let's Encrypt (staging, for testing)",
		Directory: "https://acme-staging-v02.api.letsencrypt.org/directory",
		TermsURL:  "https://letsencrypt.org/repository/", Trusted: false},
}

// LookupCA returns the CA with this id.
func LookupCA(id string) (CA, bool) {
	for _, c := range KnownCAs {
		if c.ID == id {
			return c, true
		}
	}
	return CA{}, false
}

// Config is what the manager acts on. It is built from config.Settings by the
// caller, so this package does not import the settings type.
type Config struct {
	Enabled  bool
	Hostname string
	CA       string
	Email    string
	// DirectoryURL, when set, replaces the CA's directory. It comes only from
	// the settings file (ADR 0081 §1).
	DirectoryURL string
}

func (c Config) directory() (string, error) {
	if c.DirectoryURL != "" {
		return c.DirectoryURL, nil
	}
	ca, ok := LookupCA(c.CA)
	if !ok {
		return "", fmt.Errorf("unknown certificate authority %q", c.CA)
	}
	return ca.Directory, nil
}

// NormalizeHostname validates a name typed by an admin and returns its
// canonical form, or an error that can be shown to them as it is.
//
// The rules are the ones a passkey and a public CA will apply anyway, checked
// here so the admin hears them now rather than as a CA error minutes later: a
// DNS name, not an address, with at least one dot, and not localhost.
func NormalizeHostname(raw string) (string, error) {
	h := strings.ToLower(strings.TrimSpace(raw))
	h = strings.TrimSuffix(h, ".")
	switch {
	case h == "":
		return "", errors.New("enter the name this server is reached by")
	case strings.Contains(h, "://") || strings.ContainsAny(h, "/:@ "):
		return "", errors.New("enter just the name, like example.duckdns.org, with no https:// or port")
	case net.ParseIP(h) != nil:
		return "", errors.New("a public certificate needs a name, not an IP address")
	case h == "localhost" || strings.HasSuffix(h, ".localhost"):
		return "", errors.New("localhost cannot have a public certificate")
	case !strings.Contains(h, "."):
		return "", errors.New("enter the full name, including the domain")
	case strings.HasPrefix(h, "*."):
		return "", errors.New("wildcard names are not supported; enter one name")
	case len(h) > 253:
		return "", errors.New("that name is too long to be a DNS name")
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", fmt.Errorf("%q is not a valid part of a DNS name", label)
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return "", fmt.Errorf("%q is not a valid part of a DNS name", label)
			}
		}
	}
	return h, nil
}

// Issuer obtains a certificate for one name. It is the part that talks to the
// CA, behind an interface so the manager's decisions are tested without one.
//
// present is called with each challenge certificate before the CA is told to
// validate, and withdraw once validation is over, so the listener answers the
// CA's connection with it in between.
type Issuer interface {
	Issue(ctx context.Context, hostname string, key crypto.Signer,
		present func(*tls.Certificate), withdraw func()) (chain [][]byte, err error)
}

// State is what the status line says the certificate is doing.
type State string

const (
	StateOff     State = "off"     // not enabled
	StatePending State = "pending" // enabled, no certificate yet, first attempt not finished
	StateActive  State = "active"  // a valid certificate is being served
	StateFailing State = "failing" // the last attempt failed
)

// Status is reported to the admin, field for field.
type Status struct {
	State       State     `json:"state"`
	Hostname    string    `json:"hostname,omitempty"`
	CA          string    `json:"ca,omitempty"`
	NotAfter    time.Time `json:"not_after,omitzero"`
	LastAttempt time.Time `json:"last_attempt,omitzero"`
	// LastError is the CA's or the network's own words, because "renewal
	// failed" with no reason is a status nobody can act on.
	LastError   string    `json:"last_error,omitempty"`
	NextAttempt time.Time `json:"next_attempt,omitzero"`
}

const (
	certFile    = "cert.pem"
	accountFile = "account.key"

	firstBackoff = time.Hour
	maxBackoff   = 24 * time.Hour
)

// Manager owns the public certificate: which one to serve, when to renew it,
// and what to say about it.
type Manager struct {
	dir      string // <data>/tls/acme
	fallback *tls.Certificate
	key      crypto.Signer // the serving key; every CSR is signed with it
	log      *slog.Logger
	now      func() time.Time
	issuer   func(Config, string) (Issuer, error)

	cur        atomic.Pointer[tls.Certificate]
	challenges sync.Map // hostname -> *tls.Certificate

	mu      sync.Mutex
	cfg     Config
	status  Status
	backoff time.Duration
	kick    chan struct{}
}

// New prepares a manager that serves fallback for everything until it has a
// public certificate. dir is where the certificate and the ACME account key
// live. A certificate already in dir is picked up if it was issued for the
// serving key; one that was not is ignored, never served.
func New(dir string, fallback tls.Certificate, log *slog.Logger) (*Manager, error) {
	key, ok := fallback.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, errors.New("serving key cannot sign a certificate request")
	}
	m := &Manager{
		dir:      dir,
		fallback: &fallback,
		key:      key,
		log:      log,
		now:      time.Now,
		issuer:   newACMEIssuer,
		kick:     make(chan struct{}, 1),
		status:   Status{State: StateOff},
	}
	if c, err := m.loadCert(); err == nil {
		m.cur.Store(c)
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Warn("acme", "action", "load", "error", err)
	}
	return m, nil
}

// Configure applies what the admin chose. It never blocks on the network:
// issuing happens in Run, which this wakes.
//
// Turning it off deletes the certificate and the account key (ADR 0081 §5),
// so off means off rather than dormant.
func (m *Manager) Configure(cfg Config) {
	m.mu.Lock()
	prev := m.cfg
	m.cfg = cfg
	// A change of name or CA makes the current certificate the wrong one, and
	// an admin pressing save is asking for an attempt now, not after backoff.
	m.backoff = 0
	if !cfg.Enabled {
		m.status = Status{State: StateOff}
	} else {
		m.status.Hostname, m.status.CA = cfg.Hostname, cfg.CA
		m.status.NextAttempt = time.Time{}
		m.status.LastError = ""
		if m.status.State == StateOff {
			m.status.State = StatePending
		}
	}
	m.mu.Unlock()

	if !cfg.Enabled {
		m.forget()
	} else if c := m.cur.Load(); c != nil && !covers(c, cfg.Hostname) {
		m.cur.Store(nil)
		_ = os.Remove(filepath.Join(m.dir, certFile))
	}
	if prev.Enabled != cfg.Enabled || prev.Hostname != cfg.Hostname {
		m.log.Info("acme", "action", "configure", "enabled", cfg.Enabled,
			"hostname", cfg.Hostname, "ca", cfg.CA)
	}
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// forget deletes everything the opt-in created.
func (m *Manager) forget() {
	m.cur.Store(nil)
	for _, f := range []string{certFile, accountFile} {
		if err := os.Remove(filepath.Join(m.dir, f)); err != nil && !errors.Is(err, os.ErrNotExist) {
			m.log.Warn("acme", "action", "forget", "file", f, "error", err)
		}
	}
}

// Status reports what the certificate is doing.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.status
	if c := m.cur.Load(); c != nil && m.cfg.Enabled {
		st.NotAfter = c.Leaf.NotAfter
		if st.State != StateFailing && m.now().Before(c.Leaf.NotAfter) {
			st.State = StateActive
		}
	}
	return st
}

/*
 * GetCertificate chooses by the name the client asked for (ADR 0081 §3).
 *
 * Only a client that named the configured host gets the public certificate.
 * Everybody else gets what they always got: a client that connected by IP
 * would be handed a certificate for a name it did not ask for, which is an
 * error, and so worse than the self-signed one it gets today.
 *
 * An expired public certificate is never served. The name falls back to the
 * self-signed certificate, which is no worse and is what it served before.
 */
func (m *Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if c := m.servable(hello.ServerName); c != nil {
		return c, nil
	}
	return m.fallback, nil
}

func (m *Manager) servable(serverName string) *tls.Certificate {
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()
	if !cfg.Enabled || !strings.EqualFold(strings.TrimSuffix(serverName, "."), cfg.Hostname) {
		return nil
	}
	c := m.cur.Load()
	if c == nil || !m.now().Before(c.Leaf.NotAfter) {
		return nil
	}
	return c
}

/*
 * ChallengeConfig answers the CA's validation connection.
 *
 * It is consulted from the listener's GetConfigForClient, before the peer
 * branch, and claims only handshakes advertising acme-tls/1. With a challenge
 * pending for the name asked, the answer is the validation certificate and
 * nothing else. With none, the handshake is refused: acme-tls/1 is spoken only
 * by a CA mid-validation, so there is nobody else to serve.
 */
func (m *Manager) ChallengeConfig(hello *tls.ClientHelloInfo) (*tls.Config, bool, error) {
	if !advertises(hello, ALPNProto) {
		return nil, false, nil
	}
	v, ok := m.challenges.Load(strings.ToLower(hello.ServerName))
	if !ok {
		return nil, true, errors.New("no ACME challenge pending for this name")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{*v.(*tls.Certificate)},
		NextProtos:   []string{ALPNProto},
		MinVersion:   tls.VersionTLS12,
	}, true, nil
}

func advertises(hello *tls.ClientHelloInfo, proto string) bool {
	for _, p := range hello.SupportedProtos {
		if p == proto {
			return true
		}
	}
	return false
}

// Run renews in the background until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	for {
		wait, ok := m.nextWait()
		var t *time.Timer
		var timer <-chan time.Time
		if ok {
			t = time.NewTimer(wait)
			timer = t.C
		}
		fired := false
		select {
		case <-ctx.Done():
		case <-m.kick:
		case <-timer:
			fired = true
		}
		if t != nil {
			t.Stop()
		}
		if ctx.Err() != nil {
			return
		}
		if fired {
			m.attempt(ctx)
		}
	}
}

// nextWait says how long until the next attempt, or false when there is
// nothing to do until something changes.
func (m *Manager) nextWait() (time.Duration, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.cfg.Enabled {
		return 0, false
	}
	now := m.now()
	if !m.status.NextAttempt.IsZero() {
		return max(0, m.status.NextAttempt.Sub(now)), true
	}
	if c := m.cur.Load(); c != nil && covers(c, m.cfg.Hostname) {
		return max(0, renewAt(c.Leaf).Sub(now)), true
	}
	return 0, true
}

// renewAt is when a third of the certificate's lifetime remains (ADR 0081 §5).
// A third rather than a fixed thirty days, so it keeps working as lifetimes
// shrink.
func renewAt(leaf *x509.Certificate) time.Time {
	life := leaf.NotAfter.Sub(leaf.NotBefore)
	return leaf.NotAfter.Add(-life / 3)
}

// attempt makes one issuance attempt and records how it went.
func (m *Manager) attempt(ctx context.Context) {
	m.mu.Lock()
	cfg := m.cfg
	m.mu.Unlock()
	if !cfg.Enabled {
		return
	}

	m.log.Info("acme", "action", "issue", "hostname", cfg.Hostname, "ca", cfg.CA)
	chain, err := m.issue(ctx, cfg)
	var leaf *tls.Certificate
	if err == nil {
		leaf, err = m.accept(chain, cfg.Hostname)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg != cfg {
		// Reconfigured while the CA was working. What came back is for a
		// configuration nobody wants any more; the kick from Configure will
		// start the right attempt.
		return
	}
	now := m.now()
	m.status.LastAttempt = now
	if err != nil {
		if m.backoff == 0 {
			m.backoff = firstBackoff
		} else {
			m.backoff = min(m.backoff*2, maxBackoff)
		}
		m.status.State = StateFailing
		m.status.LastError = err.Error()
		m.status.NextAttempt = now.Add(m.backoff)
		m.log.Warn("acme", "action", "issue failed", "hostname", cfg.Hostname,
			"error", err, "retry_in", m.backoff)
		return
	}
	m.cur.Store(leaf)
	m.backoff = 0
	m.status.State = StateActive
	m.status.LastError = ""
	m.status.NextAttempt = time.Time{}
	m.log.Info("acme", "action", "issued", "hostname", cfg.Hostname,
		"not_after", leaf.Leaf.NotAfter.Format(time.RFC3339))
}

func (m *Manager) issue(ctx context.Context, cfg Config) ([][]byte, error) {
	iss, err := m.issuer(cfg, filepath.Join(m.dir, accountFile))
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(cfg.Hostname)
	return iss.Issue(ctx, host, m.key,
		func(c *tls.Certificate) { m.challenges.Store(host, c) },
		func() { m.challenges.Delete(host) })
}

/*
 * accept checks what the CA returned before anything serves it, and saves it.
 *
 * The leaf must be for the configured name and for the serving key. The second
 * is the one that matters: it is the property ADR 0081 §4 is built on, and a
 * certificate for a different key would break every pinned desktop client the
 * moment it was served. Nothing in ACME guarantees it except that we sent the
 * CSR, so it is checked rather than assumed.
 */
func (m *Manager) accept(chain [][]byte, hostname string) (*tls.Certificate, error) {
	if len(chain) == 0 {
		return nil, errors.New("the CA returned no certificate")
	}
	leaf, err := x509.ParseCertificate(chain[0])
	if err != nil {
		return nil, fmt.Errorf("the CA returned a certificate that does not parse: %w", err)
	}
	if err := leaf.VerifyHostname(hostname); err != nil {
		return nil, fmt.Errorf("the CA returned a certificate for the wrong name: %w", err)
	}
	if !samePublicKey(leaf.PublicKey, m.key.Public()) {
		return nil, errors.New("the CA returned a certificate for a different key")
	}
	cert := &tls.Certificate{Certificate: chain, PrivateKey: m.key, Leaf: leaf}
	if err := m.saveCert(chain); err != nil {
		// Served anyway: it is valid, and a disk problem is a reason to issue
		// again after a restart, not to keep the warning up now.
		m.log.Warn("acme", "action", "save", "error", err)
	}
	return cert, nil
}

func samePublicKey(a, b crypto.PublicKey) bool {
	ak, ok := a.(interface{ Equal(crypto.PublicKey) bool })
	return ok && ak.Equal(b)
}

func covers(c *tls.Certificate, hostname string) bool {
	return c.Leaf != nil && c.Leaf.VerifyHostname(hostname) == nil
}

func (m *Manager) saveCert(chain [][]byte) error {
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return err
	}
	var buf []byte
	for _, der := range chain {
		buf = append(buf, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	tmp := filepath.Join(m.dir, certFile+".tmp")
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(m.dir, certFile))
}

func (m *Manager) loadCert() (*tls.Certificate, error) {
	raw, err := os.ReadFile(filepath.Join(m.dir, certFile))
	if err != nil {
		return nil, err
	}
	var chain [][]byte
	for {
		var b *pem.Block
		b, raw = pem.Decode(raw)
		if b == nil {
			break
		}
		if b.Type == "CERTIFICATE" {
			chain = append(chain, b.Bytes)
		}
	}
	if len(chain) == 0 {
		return nil, errors.New("stored public certificate holds no certificate")
	}
	leaf, err := x509.ParseCertificate(chain[0])
	if err != nil {
		return nil, err
	}
	if !samePublicKey(leaf.PublicKey, m.key.Public()) {
		return nil, errors.New("stored public certificate is for a different key; ignoring it")
	}
	return &tls.Certificate{Certificate: chain, PrivateKey: m.key, Leaf: leaf}, nil
}
