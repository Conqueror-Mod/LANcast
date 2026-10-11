package auth

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

/*
 * AccountBackoff slows password guessing aimed at one account from many
 * addresses.
 *
 * Throttle counts per address, which is the whole defence against one machine
 * guessing. It is no defence against many: an attacker with a thousand
 * addresses gets ten guesses each every five minutes, and the server has been
 * publicly reachable since 2026-10. So failures are also counted against the
 * *name* that was tried.
 *
 * **A wait, never a lockout.** The project's rule is that an attacker who can
 * lock the owner out has a denial of service for free (docs/security.md). Three
 * things keep this on the right side of it:
 *
 *   - It grows and then stops growing. A few failures cost nothing; after that
 *     each one doubles the wait, to a ceiling of minutes, not hours.
 *   - An attempt made during the wait is refused without being counted. The
 *     owner retrying does not extend it — only fresh failures do, and those
 *     have to wait their turn like everything else.
 *   - It does not apply from inside the house (InsideNetwork). The person most
 *     likely to be locked out by an attack is the owner, and the owner can
 *     always sign in from home, where the per-address throttle still applies.
 *
 * **Names that match no account are tracked too.** If only real accounts could
 * be made to wait, a 429 would answer "does this account exist" — the question
 * login refuses to answer everywhere else.
 */
type AccountBackoff struct {
	mu      sync.Mutex
	entries map[string]*accountEntry

	// Free is how many failures cost nothing.
	Free int
	// Base is the first wait after the free failures; each further failure
	// doubles it, up to Max.
	Base time.Duration
	Max  time.Duration
	// Forget is how long an account must go without a failure before its
	// count starts again from zero.
	Forget time.Duration
	// Cap bounds how many names are remembered, because the names are chosen
	// by whoever is guessing.
	Cap int

	now func() time.Time
}

type accountEntry struct {
	failures int
	last     time.Time // the most recent counted failure
	until    time.Time // no attempt before this
}

// NewAccountBackoff returns the production settings: five free failures, then
// 30s doubling to 15 minutes — about a hundred guesses a day against one
// account, however many addresses they come from.
func NewAccountBackoff() *AccountBackoff {
	return &AccountBackoff{
		entries: map[string]*accountEntry{},
		Free:    5,
		Base:    30 * time.Second,
		Max:     15 * time.Minute,
		Forget:  time.Hour,
		Cap:     10000,
		now:     time.Now,
	}
}

// accountKey folds a submitted username the way the user table does
// (COLLATE NOCASE), so "Chris" and "chris" share one count.
func accountKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// Allow reports whether name may be tried now, and if not, how long remains.
// It does not count anything: Fail does that, and only for attempts Allow let
// through.
func (b *AccountBackoff) Allow(name string) (bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	e, ok := b.entries[accountKey(name)]
	if !ok {
		return true, 0
	}
	now := b.now()
	if now.Sub(e.last) >= b.Forget {
		delete(b.entries, accountKey(name))
		return true, 0
	}
	if now.Before(e.until) {
		return false, e.until.Sub(now)
	}
	return true, 0
}

// Fail records a wrong password for name.
func (b *AccountBackoff) Fail(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	key := accountKey(name)
	now := b.now()
	e, ok := b.entries[key]
	if ok && now.Sub(e.last) >= b.Forget {
		ok = false
	}
	if !ok {
		b.makeRoom()
		e = &accountEntry{}
		b.entries[key] = e
	}
	e.failures++
	e.last = now
	if over := e.failures - b.Free; over > 0 {
		e.until = now.Add(b.wait(over))
	}
}

// Reset forgets name after a correct password, which is the proof the wait
// exists to demand.
func (b *AccountBackoff) Reset(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.entries, accountKey(name))
}

// wait is Base doubled once per failure past the free ones, capped at Max. The
// loop stops at the cap rather than shifting, so a long attack cannot overflow
// it into a negative duration.
func (b *AccountBackoff) wait(over int) time.Duration {
	d := b.Base
	for i := 1; i < over && d < b.Max; i++ {
		d *= 2
	}
	if d > b.Max {
		d = b.Max
	}
	return d
}

/*
 * makeRoom keeps the map under Cap. Called with mu held.
 *
 * Forgotten entries go first. If that is not enough — somebody spraying
 * invented names faster than they expire — the entry with the fewest failures
 * goes. That choice is the point: a spray leaves thousands of names with one
 * failure each, and the account actually under attack has many. Evicting by
 * age instead would let the spray push the real target out of memory and reset
 * its wait.
 */
func (b *AccountBackoff) makeRoom() {
	if len(b.entries) < b.Cap {
		return
	}
	now := b.now()
	for k, e := range b.entries {
		if now.Sub(e.last) >= b.Forget {
			delete(b.entries, k)
		}
	}
	for len(b.entries) >= b.Cap {
		var victim string
		var least *accountEntry
		for k, e := range b.entries {
			if least == nil || e.failures < least.failures ||
				(e.failures == least.failures && e.last.Before(least.last)) {
				victim, least = k, e
			}
		}
		delete(b.entries, victim)
	}
}

/*
 * InsideNetwork reports whether the caller is on this machine or a private
 * network: loopback, RFC 1918 and IPv6 unique-local, or link-local.
 *
 * It is the exemption from AccountBackoff, and it errs towards *outside*. A
 * phone at home reaching the server on a global IPv6 address is classed as
 * outside and simply gets the same treatment as the internet — the safe
 * direction. Like ClientKey it reads only the remote address: a forwarded
 * header claiming to be 192.168.x.x is exactly what an attacker would send.
 *
 * Behind a reverse proxy on the same machine every caller looks inside, so the
 * backoff does not apply there; docs/security.md already says the proxy has to
 * rate-limit in that arrangement.
 */
func InsideNetwork(r *http.Request) bool {
	ip := net.ParseIP(ClientKey(r))
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}
