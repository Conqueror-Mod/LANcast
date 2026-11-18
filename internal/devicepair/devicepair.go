/*
Package devicepair is a TV signing in by a code it shows and a person types
(ADR 0125 §1–2).

Pure: the code, and the in-memory book of pairings waiting for somebody to
approve them. Nothing here touches the database, the network or an API key;
the routes in internal/api do that, so every rule about expiry, single use and
the ceiling is tested in milliseconds.

# THE CODE IS COMPUTED, NOT ISSUED

The server does not choose the code. Both ends derive it from a nonce the
server chose and the certificate the TV was served (Code). A machine in the
middle serves the TV its own certificate, so the TV shows a code the server
never filed, and the approval fails as an unknown code. The person compares
nothing; copying the code is the comparison.
*/
package devicepair

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	// Lifetime is how long a pairing waits for an approval, and then for the
	// TV to collect its key.
	Lifetime = 10 * time.Minute
	// Interval is how often a TV may poll.
	Interval = 5 * time.Second
	// MaxPending is the ceiling on pairings waiting at once. The start route
	// needs no session, so it is the cheapest thing on the server to flood.
	MaxPending = 32
	// MaxName is the longest device name kept.
	MaxName = 64

	// domain separates this hash from every other use of SHA-256 on a
	// certificate. The version is in it so a later scheme cannot collide.
	domain = "LANcast pairing v1"
)

/*
 * Alphabet is Crockford's base32: 0–9 and A–Z without I, L, O and U.
 *
 * ADR 0125 asked for 32 symbols with none of 0/O and 1/I/L, and there are only
 * 31 such letters and digits. Crockford's answer to the same problem is to show
 * 0 and 1 and never show O, I or L, then read those back as 0 and 1
 * (Normalize). Nothing on the screen is ambiguous, and nothing a person might
 * type for it is refused.
 */
const Alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// CodeLength is 8 symbols, 40 bits.
const CodeLength = 8

/*
 * Code is the eight symbols a TV shows:
 *
 *	base32(SHA-256("LANcast pairing v1" || 0x00 || nonce || certDER))[:8]
 *
 * in Alphabet, most significant bits first. certDER is the leaf the connection
 * presented. The Roku app computes the same thing in BrightScript, so this is a
 * wire format: changing it is a new domain string, not an edit.
 */
func Code(nonce, certDER []byte) string {
	h := sha256.New()
	h.Write([]byte(domain))
	h.Write([]byte{0})
	h.Write(nonce)
	h.Write(certDER)
	sum := h.Sum(nil)

	// The first 40 bits, as eight 5-bit groups.
	var v uint64
	for _, b := range sum[:5] {
		v = v<<8 | uint64(b)
	}
	out := make([]byte, CodeLength)
	for i := CodeLength - 1; i >= 0; i-- {
		out[i] = Alphabet[v&31]
		v >>= 5
	}
	return string(out)
}

// Normalize reads a typed code the way Crockford's base32 does: case folded,
// spaces and dashes dropped, O read as 0 and I or L as 1. "" when what is left
// is not a code.
func Normalize(typed string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(typed) {
		switch r {
		case ' ', '-', '\t':
			continue
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		if !strings.ContainsRune(Alphabet, r) {
			return ""
		}
		b.WriteRune(r)
	}
	if b.Len() != CodeLength {
		return ""
	}
	return b.String()
}

// Format shows a code in two groups of four, as the TV and the screen do.
func Format(code string) string {
	if len(code) != CodeLength {
		return code
	}
	return code[:4] + "-" + code[4:]
}

// State is where a pairing has got to.
type State int

const (
	Waiting State = iota
	Approved
	Denied
)

// Pairing is one TV waiting. Everything a person sees on the approval screen
// is here; the token the TV polls with is not, beyond the map it is filed
// under.
type Pairing struct {
	Code    string
	Name    string
	Device  string
	Addr    string
	Created time.Time
	Expires time.Time
	State   State
	// ApproverID is the account the key will belong to, once approved.
	ApproverID string

	lastPoll time.Time
}

// Started is what the TV is told when it asks to pair.
type Started struct {
	Token string
	Nonce []byte
}

var (
	// ErrFull means MaxPending pairings are already waiting.
	ErrFull = errors.New("too many pairings are waiting")
	// ErrUnknown is a token or code with no pairing filed under it: never
	// issued, expired, denied and collected, or already used.
	ErrUnknown = errors.New("no such pairing")
	// ErrTooSoon is a poll faster than Interval.
	ErrTooSoon = errors.New("polled too soon")
	// ErrDecided is a second decision on a pairing that already has one.
	ErrDecided = errors.New("this pairing has already been decided")
)

// Book holds the pairings waiting, in memory only: ten minutes, single use,
// gone on restart. Nothing about an unfinished pairing is worth keeping.
type Book struct {
	mu      sync.Mutex
	byToken map[string]*Pairing
	byCode  map[string]*Pairing
	now     func() time.Time
	random  func([]byte) error
}

// NewBook is an empty book.
func NewBook() *Book {
	return &Book{
		byToken: map[string]*Pairing{},
		byCode:  map[string]*Pairing{},
		now:     time.Now,
		random:  func(b []byte) error { _, err := rand.Read(b); return err },
	}
}

// SetClock replaces the clock, for tests.
func (b *Book) SetClock(now func() time.Time) { b.now = now }

// sweep forgets expired pairings. Called with mu held.
func (b *Book) sweep(now time.Time) {
	for tok, p := range b.byToken {
		if !now.Before(p.Expires) {
			delete(b.byToken, tok)
			if b.byCode[p.Code] == p {
				delete(b.byCode, p.Code)
			}
		}
	}
}

/*
 * Start files a pairing for a TV, and returns the token it polls with and the
 * nonce it derives its code from. certDER is the leaf this connection was
 * served, so the code filed here is the code a TV that saw the same
 * certificate will show.
 */
func (b *Book) Start(name, device, addr string, certDER []byte) (Started, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	b.sweep(now)
	if len(b.byToken) >= MaxPending {
		return Started{}, ErrFull
	}
	tok := make([]byte, 32)
	if err := b.random(tok); err != nil {
		return Started{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(tok)

	// A fresh nonce until the code is not one already waiting. Two waiting
	// pairings under one code would let approving one hand over the other.
	var nonce []byte
	var code string
	for tries := 0; ; tries++ {
		nonce = make([]byte, 16)
		if err := b.random(nonce); err != nil {
			return Started{}, err
		}
		code = Code(nonce, certDER)
		if _, taken := b.byCode[code]; !taken {
			break
		}
		if tries > 8 {
			return Started{}, errors.New("could not choose an unused code")
		}
	}
	p := &Pairing{
		Code: code, Name: name, Device: device, Addr: addr,
		Created: now, Expires: now.Add(Lifetime), State: Waiting,
	}
	b.byToken[token] = p
	b.byCode[code] = p
	return Started{Token: token, Nonce: nonce}, nil
}

// Lookup is a waiting pairing by the code a person typed, for the approval
// screen. Only one still waiting for a decision.
func (b *Book) Lookup(code string) (Pairing, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweep(b.now())
	p, ok := b.byCode[code]
	if !ok || p.State != Waiting {
		return Pairing{}, ErrUnknown
	}
	return *p, nil
}

/*
 * Decide approves or denies a waiting pairing. An approval records whose key
 * it will be; the key itself is made when the TV collects it, so a pairing
 * approved and never collected leaves nothing behind.
 */
func (b *Book) Decide(code string, approve bool, approverID string) (Pairing, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweep(b.now())
	p, ok := b.byCode[code]
	if !ok {
		return Pairing{}, ErrUnknown
	}
	if p.State != Waiting {
		return Pairing{}, ErrDecided
	}
	if approve {
		p.State, p.ApproverID = Approved, approverID
	} else {
		p.State = Denied
	}
	// The code is spent either way: nobody can decide it again, and a
	// different TV can be filed under it.
	delete(b.byCode, code)
	return *p, nil
}

/*
 * Poll is the TV asking whether it has been approved.
 *
 * Waiting: the pairing, unchanged. Approved: the pairing, and the TV's token is
 * forgotten, so the key can be collected exactly once; the caller then makes
 * the key, and calls Restore if it could not. Denied: ErrUnknown, and the
 * token is forgotten, which is what an expired one says too.
 */
func (b *Book) Poll(token string) (Pairing, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	b.sweep(now)
	p, ok := b.byToken[token]
	if !ok {
		return Pairing{}, ErrUnknown
	}
	// A second's grace: a TV polling on a five-second timer is not early
	// because its timer and this clock disagree by a few milliseconds.
	if !p.lastPoll.IsZero() && now.Sub(p.lastPoll) < Interval-time.Second {
		return Pairing{}, ErrTooSoon
	}
	p.lastPoll = now
	switch p.State {
	case Approved:
		delete(b.byToken, token)
	case Denied:
		delete(b.byToken, token)
		return Pairing{}, ErrUnknown
	}
	return *p, nil
}

// Restore puts back an approved pairing whose key could not be made, so the
// TV's next poll tries again rather than finding nothing.
func (b *Book) Restore(token string, p Pairing) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.now().Before(p.Expires) {
		return
	}
	cp := p
	cp.lastPoll = time.Time{}
	b.byToken[token] = &cp
}

// Waiting counts pairings not yet collected, expired or denied, for tests.
func (b *Book) Waiting() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweep(b.now())
	return len(b.byToken)
}
