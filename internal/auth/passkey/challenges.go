package passkey

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

// Purpose separates the two ceremonies, so a challenge issued for a sign-in
// can never complete a registration or the reverse.
type Purpose string

const (
	PurposeRegister Purpose = "register"
	PurposeLogin    Purpose = "login"
)

// ErrTooManyChallenges is returned when the store is full of live challenges.
// The sign-in route that issues them is public, so the store is bounded rather
// than allowed to grow with whatever a stranger asks for.
var ErrTooManyChallenges = errors.New("too many passkey challenges outstanding")

/*
 * Challenges holds issued challenges in memory: single use, short-lived, and
 * gone on restart, which is harmless (a restart abandons a ceremony in
 * flight, and the person presses the button again).
 *
 * A challenge is bound to its purpose and, for a registration, to the account
 * that asked. A sign-in challenge is bound to nobody: discoverable passkeys
 * say who they belong to, and that is checked against the stored credential.
 */
type Challenges struct {
	mu  sync.Mutex
	m   map[string]challengeEntry
	TTL time.Duration
	Cap int
	now func() time.Time
}

type challengeEntry struct {
	challenge []byte
	purpose   Purpose
	userID    string
	expires   time.Time
}

// NewChallenges returns the production store: five minutes, at most 1,000
// outstanding.
func NewChallenges() *Challenges {
	return &Challenges{m: map[string]challengeEntry{}, TTL: 5 * time.Minute, Cap: 1000, now: time.Now}
}

// Issue creates a challenge and returns its id (what the client sends back)
// and its bytes (what goes to the authenticator).
func (c *Challenges) Issue(p Purpose, userID string) (id string, challenge []byte, err error) {
	challenge = make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return "", nil, err
	}
	idb := make([]byte, 16)
	if _, err := rand.Read(idb); err != nil {
		return "", nil, err
	}
	id = base64.RawURLEncoding.EncodeToString(idb)

	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if len(c.m) >= c.Cap {
		for k, e := range c.m {
			if !now.Before(e.expires) {
				delete(c.m, k)
			}
		}
		if len(c.m) >= c.Cap {
			return "", nil, ErrTooManyChallenges
		}
	}
	c.m[id] = challengeEntry{challenge: challenge, purpose: p, userID: userID, expires: now.Add(c.TTL)}
	return id, challenge, nil
}

// Take returns and forgets a challenge. It is gone whether or not the
// ceremony then succeeds, so no challenge is ever verified twice.
func (c *Challenges) Take(id string, p Purpose) (challenge []byte, userID string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, found := c.m[id]
	delete(c.m, id)
	if !found || e.purpose != p || !c.now().Before(e.expires) {
		return nil, "", false
	}
	return e.challenge, e.userID, true
}
