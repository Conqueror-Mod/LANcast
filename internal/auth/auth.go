// Package auth handles password verification and session tokens.
//
// LANcast is a single-password personal server: one password guards the whole
// instance. The schema is already keyed by user (ADR 0006), so real multi-user
// accounts can arrive later without a migration or data loss.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	// CookieName is the session cookie.
	CookieName = "lancast_session"

	// SessionTTL is how long a session stays valid without use. Touching a
	// session extends it, so an active viewer is never logged out mid-film.
	SessionTTL = 30 * 24 * time.Hour

	// MinPasswordLength is deliberately modest. This guards a media server on
	// a home network, and a requirement people work around by choosing
	// "Password1!" buys nothing.
	MinPasswordLength = 8

	// bcryptCost is above the library default of 10. Logins are rare; a
	// hundred milliseconds is invisible to a person and expensive in bulk.
	bcryptCost = 12
)

var (
	// ErrBadPassword covers both "no such user" and "wrong password". They are
	// never distinguished in responses.
	ErrBadPassword = errors.New("incorrect password")
	// ErrTooManyAttempts is returned when a client is being throttled.
	ErrTooManyAttempts = errors.New("too many attempts")
	// ErrWeakPassword is returned when a proposed password is too short.
	ErrWeakPassword = fmt.Errorf("password must be at least %d characters", MinPasswordLength)
)

// HashPassword returns a bcrypt hash suitable for storing.
func HashPassword(password string) (string, error) {
	if len([]rune(password)) < MinPasswordLength {
		return "", ErrWeakPassword
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(h), nil
}

// CheckPassword verifies a password against a stored hash in constant time.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// NewToken returns a fresh session token and the hash to store against it.
func NewToken() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, HashToken(token), nil
}

// HashToken is the one-way mapping from cookie value to stored key.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Cookie builds the session cookie. secure marks it Secure so it is only sent
// over HTTPS — pass true when the request that mints it arrived over TLS.
//
// SameSite=Strict is load-bearing rather than decorative: without it, any page
// in the user's browser could issue authenticated requests to the server on
// localhost or a LAN address, and every state-changing endpoint would carry
// their session. Combined with the origin check in the API middleware, that is
// the CSRF defence.
func Cookie(token string, ttl time.Duration, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Now().Add(ttl),
		MaxAge:   int(ttl.Seconds()),
		// Secure follows the connection: a LAN-bound server serves HTTPS
		// (ADR 0014), and marking the cookie Secure there stops it being
		// downgraded to a plaintext request. A loopback-only server serves
		// plain HTTP, where a Secure cookie would never be sent at all — so it
		// is set only when the minting request arrived over TLS.
		Secure: secure,
	}
}

// ClearCookie expires the session cookie.
func ClearCookie() *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	}
}

// Throttle limits password attempts per client.
//
// A single shared password is one guessable secret, so unlimited attempts
// against it is the whole attack. This is deliberately simple: no distributed
// state, no lockout that an attacker could trigger against the owner —
// failures decay on their own.
type Throttle struct {
	mu       sync.Mutex
	attempts map[string]*window
	Max      int
	Window   time.Duration
}

type window struct {
	count int
	reset time.Time
}

func NewThrottle() *Throttle {
	return &Throttle{
		attempts: map[string]*window{},
		Max:      10,
		Window:   5 * time.Minute,
	}
}

// Allow reports whether another attempt from key is permitted.
func (t *Throttle) Allow(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	w, ok := t.attempts[key]
	if !ok || now.After(w.reset) {
		t.attempts[key] = &window{count: 1, reset: now.Add(t.Window)}
		return true
	}
	if w.count >= t.Max {
		return false
	}
	w.count++
	return true
}

// Reset clears the counter after a successful login.
func (t *Throttle) Reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.attempts, key)
}

// ClientKey identifies a caller for throttling. It is the remote address only:
// forwarded headers are attacker-controlled unless a trusted proxy sets them,
// and trusting them here would let anyone reset their own counter.
func ClientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// SameOriginRequest reports whether a state-changing request came from the
// page itself.
//
// The second half of the CSRF defence. A cross-site form post carries an
// Origin that will not match; a request with no Origin and no Referer is
// allowed, because non-browser clients (curl, a TV app, a script) legitimately
// send neither and they are not the CSRF threat.
func SameOriginRequest(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		if ref := r.Header.Get("Referer"); ref != "" {
			origin = ref
		} else {
			return true
		}
	}

	host := hostOf(origin)
	if host == "" {
		return false
	}
	return strings.EqualFold(host, r.Host)
}

// hostOf extracts host[:port] from an absolute URL without pulling in net/url
// error handling for a value that may be junk.
func hostOf(raw string) string {
	s := raw
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	} else {
		return ""
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return s
}
