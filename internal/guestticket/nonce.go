package guestticket

import (
	"sync"
	"time"
)

/*
 * The nonce store: what stops a ticket being used twice.
 *
 * Verify is pure and deliberately does not check the nonce, because spending
 * one needs state. This is that state, and a caller that verifies without
 * spending has a replayable ticket — which is why Claims.Nonce is handed back
 * rather than consumed somewhere out of sight.
 *
 * # In memory, and lost on restart
 *
 * Correct rather than merely convenient. Every outstanding ticket expires in
 * minutes, so a restart widens the replay window by less than the expiry it
 * already allows. The alternative is a durable table of other people's
 * credentials, which is a thing to back up, migrate and leak.
 *
 * # Bounded per issuer, not globally
 *
 * The plan for this phase said "bounded", and a single global bound is the
 * obvious reading. It is wrong, and the reason is worth keeping: one peer
 * flooding nonces would fill a shared bound and every *other* peer's tickets
 * would then be refused. A denial of service against the host's other friends,
 * delivered by one of them.
 *
 * So the bound is per issuer. A peer can exhaust its own allowance and nobody
 * else's, which turns a shared failure into a local one.
 *
 * # Over the bound it refuses rather than evicts
 *
 * Evicting the oldest entry to make room is the intuitive move and it undoes
 * the whole point: the evicted nonce becomes spendable again, so an attacker
 * who can push entries through the store can replay any ticket they like.
 * Refusing is a denial of service against one peer; evicting is a replay
 * vulnerability for all of them.
 */
type NonceStore struct {
	mu sync.Mutex
	// seen is issuer -> nonce -> when it stops mattering.
	seen    map[string]map[string]time.Time
	perPeer int
}

// DefaultNoncesPerPeer is the allowance one paired server gets.
//
// A ticket lasts minutes, so this is a ceiling on how many admissions one peer
// may attempt inside that window — far above any real household and far below
// anything that troubles memory.
const DefaultNoncesPerPeer = 4096

// NewNonceStore returns a store allowing perPeer outstanding nonces from each
// issuer. Zero or less takes the default.
func NewNonceStore(perPeer int) *NonceStore {
	if perPeer <= 0 {
		perPeer = DefaultNoncesPerPeer
	}
	return &NonceStore{seen: map[string]map[string]time.Time{}, perPeer: perPeer}
}

/*
 * Spend records a nonce and reports whether it was fresh.
 *
 * Keyed by issuer **and** nonce, not by nonce alone. Two servers pick their
 * nonces independently and may collide by chance; worse, a peer that could
 * spend a nonce another peer was about to use would be able to refuse that
 * peer's admissions at will. Namespacing by issuer makes one peer's choices
 * unable to affect another's.
 *
 * false means "do not admit this", whether because it has been used, because
 * it has already expired, or because this peer is over its allowance. The
 * caller does not get to know which, for the same reason Verify gives one
 * error: the difference is a question about the host's state that a stranger
 * should not be able to ask.
 */
func (n *NonceStore) Spend(issuer, nonce string, expires, now time.Time) bool {
	if issuer == "" || nonce == "" {
		return false
	}
	// A nonce that is already past is not fresh, and recording it would be
	// storing something that can never be used.
	if !expires.After(now) {
		return false
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	peer := n.seen[issuer]
	if peer == nil {
		peer = map[string]time.Time{}
		n.seen[issuer] = peer
	}

	// Sweep this peer's expired entries before judging its allowance, so a
	// peer that has been quiet is not punished for having once been busy.
	for k, exp := range peer {
		if !exp.After(now) {
			delete(peer, k)
		}
	}

	if _, used := peer[nonce]; used {
		return false
	}
	if len(peer) >= n.perPeer {
		return false
	}
	peer[nonce] = expires
	return true
}

// Sweep drops everything expired, across every issuer, and forgets peers with
// nothing outstanding. Cheap, and worth calling on a timer so a peer that
// stops talking does not leave an empty map behind for ever.
func (n *NonceStore) Sweep(now time.Time) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for issuer, peer := range n.seen {
		for k, exp := range peer {
			if !exp.After(now) {
				delete(peer, k)
			}
		}
		if len(peer) == 0 {
			delete(n.seen, issuer)
		}
	}
}

// Outstanding is how many live nonces one issuer holds. For tests and for a
// host that wants to see it; not for a response.
func (n *NonceStore) Outstanding(issuer string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.seen[issuer])
}
