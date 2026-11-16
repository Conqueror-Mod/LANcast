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

// ErrTooManyChallenges is returned when the registration budget is full of
// live challenges. A sign-in never gets it: see Issue.
var ErrTooManyChallenges = errors.New("too many passkey challenges outstanding")

/*
 * Challenges holds issued challenges in memory: single use, short-lived, and
 * gone on restart, which is harmless (a restart abandons a ceremony in
 * flight, and the person presses the button again).
 *
 * A challenge is bound to its purpose and, for a registration, to the account
 * that asked. A sign-in challenge is bound to nobody: discoverable passkeys
 * say who they belong to, and that is checked against the stored credential.
 *
 * Each purpose has its own budget of Cap. The sign-in route is public, and it
 * used to refuse everybody once a stranger had filled the store, which took a
 * thousand requests every five minutes (R01-6). Now one address holds at most
 * PerClient sign-in challenges, its own oldest going first, and a full store
 * drops its oldest sign-in challenge instead of refusing the new one: whoever
 * floods it pushes out their own entries before anybody else's, and a person
 * mid-ceremony loses theirs only to a flood from many addresses inside the
 * seconds it takes to touch a key. Registration keeps a refusal, since those
 * are issued only after a password.
 */
type Challenges struct {
	mu        sync.Mutex
	m         map[string]challengeEntry
	TTL       time.Duration
	Cap       int
	PerClient int
	now       func() time.Time
	seq       uint64
}

type challengeEntry struct {
	challenge []byte
	purpose   Purpose
	userID    string
	client    string
	expires   time.Time
	// seq orders entries by issue, which a clock read twice in one tick cannot.
	seq uint64
}

// NewChallenges returns the production store: five minutes, at most 1,000
// outstanding of each purpose, and 32 sign-ins per address.
func NewChallenges() *Challenges {
	return &Challenges{m: map[string]challengeEntry{}, TTL: 5 * time.Minute, Cap: 1000, PerClient: 32, now: time.Now}
}

// Issue creates a challenge and returns its id (what the client sends back)
// and its bytes (what goes to the authenticator). client is who asked, as the
// caller identifies an address; it bounds that address's share of sign-ins.
func (c *Challenges) Issue(p Purpose, userID, client string) (id string, challenge []byte, err error) {
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
	if p == PurposeLogin && c.PerClient > 0 {
		for c.count(p, client, true) >= c.PerClient {
			c.evictOldest(p, client, true)
		}
	}
	if c.count(p, "", false) >= c.Cap {
		for k, e := range c.m {
			if !now.Before(e.expires) {
				delete(c.m, k)
			}
		}
		for c.count(p, "", false) >= c.Cap {
			if p != PurposeLogin {
				return "", nil, ErrTooManyChallenges
			}
			c.evictOldest(p, "", false)
		}
	}
	c.seq++
	c.m[id] = challengeEntry{challenge: challenge, purpose: p, userID: userID, client: client,
		expires: now.Add(c.TTL), seq: c.seq}
	return id, challenge, nil
}

// count is how many entries of purpose p there are, from client when byClient.
func (c *Challenges) count(p Purpose, client string, byClient bool) int {
	n := 0
	for _, e := range c.m {
		if e.purpose == p && (!byClient || e.client == client) {
			n++
		}
	}
	return n
}

// evictOldest drops the earliest-issued entry of purpose p (from client when
// byClient). A linear scan: the store holds at most a few thousand.
func (c *Challenges) evictOldest(p Purpose, client string, byClient bool) {
	oldest, found := "", false
	var at uint64
	for k, e := range c.m {
		if e.purpose != p || (byClient && e.client != client) {
			continue
		}
		if !found || e.seq < at {
			oldest, at, found = k, e.seq, true
		}
	}
	if found {
		delete(c.m, oldest)
	}
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
