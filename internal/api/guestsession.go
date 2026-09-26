package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"lancast/internal/auth"
	"lancast/internal/guestticket"
	"lancast/internal/identity"
	"lancast/internal/store"
)

/*
 * Redeeming a ticket: the host side of
 * [ADR 0046](../../docs/adr/0046-remote-guests.md) §2.
 *
 * A ticket arrives from a client this server has never met, signed by a server
 * it has. Verification is the whole of the trust decision — the pin, the
 * audience, the expiry and the nonce — and what comes back is a restricted
 * session that is **not yet good for anything**. What it may reach is the
 * allow-list and the object-level checks, which are the next steps of the
 * Phase 4 plan; until those exist this mints a credential no route accepts.
 *
 * That ordering is deliberate. A session type wired into handlers before the
 * default-deny middleware exists is the shape most likely to look finished and
 * not be.
 */

/*
 * guestSessionTTL is how long a redeemed session lasts.
 *
 * ADR 0046 ends a guest session with its room. A friend (ADR 0071) has no room
 * to end with, and neither ADR says what bounds it instead — so it is decided
 * here: a short life, and the client presents a fresh ticket when it lapses.
 *
 * Short wins because the alternative is a bearer token that stays useful for
 * as long as somebody keeps it. Renewal costs nothing new: the client already
 * has to be able to obtain a ticket, so re-presenting one is machinery that
 * exists rather than machinery to build.
 *
 * It is not what makes revocation work. Un-sharing and unpairing take effect
 * on the next request, because what a friend may reach is resolved per request
 * from the host's own rows rather than frozen into this session.
 */
const guestSessionTTL = 15 * time.Minute

// maxGuestSessions bounds the book. Refuses rather than evicts, for the reason
// the nonce store gives: evicting to make room is a way to be pushed out of
// your own session by somebody noisier.
const maxGuestSessions = 512

/*
 * guestSession is a redeemed ticket.
 *
 * It holds who and from where, and deliberately not what may be reached. A
 * session carrying a set of library ids would be a snapshot of permission
 * taken at admission, and un-sharing would then have to hunt down and edit
 * live sessions instead of simply being true at the next request.
 */
type guestSession struct {
	// Peer is the issuing server's fingerprint: the unit a share is granted
	// to (ADR 0071 §1).
	Peer string
	// Subject is the person, as their own server named them. Carried for the
	// host's log and for the guest's own UI, never for authorization.
	Subject string
	Expires time.Time
}

type guestBook struct {
	mu sync.Mutex
	m  map[string]guestSession // by hash of the bearer token
}

func newGuestBook() *guestBook { return &guestBook{m: map[string]guestSession{}} }

// add records a session, sweeping the expired first so a quiet period frees
// the book. Reports false when the book is full.
func (b *guestBook) add(hash string, g guestSession, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for h, x := range b.m {
		if !x.Expires.After(now) {
			delete(b.m, h)
		}
	}
	if len(b.m) >= maxGuestSessions {
		return false
	}
	b.m[hash] = g
	return true
}

// lookup returns a live session. An expired one is not found, which is the
// same answer as never having existed.
func (b *guestBook) lookup(hash string, now time.Time) (guestSession, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	g, ok := b.m[hash]
	if !ok || !g.Expires.After(now) {
		return guestSession{}, false
	}
	return g, true
}

/*
 * redeemGuestTicket trades a signed ticket for a restricted session.
 *
 * Every refusal is 401 with the same message, matching guestticket.Verify's
 * uniform error and for the same reason: a caller must not be able to learn
 * whether a ticket failed on its signature, its audience, its expiry or its
 * nonce. The detail goes to this server's log, where the operator can see it.
 */
func (s *Server) redeemGuestTicket(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ticket string `json:"ticket"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<13)).Decode(&body); err != nil || body.Ticket == "" {
		s.refuseGuest(r, "malformed request")
		writeError(w, http.StatusUnauthorized, "unauthorized", "this ticket was not accepted")
		return
	}

	/*
	 * The pairing check, injected so it cannot be skipped. A fingerprint is
	 * SHA-256 of a key, so asking whether we paired with *this* key's
	 * fingerprint is the pin — and unpairing therefore refuses the next
	 * redemption with nothing else to clean up.
	 */
	isPaired := func(issuer string) bool {
		p, err := s.st.PeerByFingerprint(r.Context(), identity.Normalize(issuer))
		return err == nil && p.State == store.PeerPaired
	}

	now := time.Now()
	claims, err := guestticket.Verify(body.Ticket, s.ident.Fingerprint(), isPaired, now)
	if err != nil {
		s.refuseGuest(r, guestticket.Reason(err))
		writeError(w, http.StatusUnauthorized, "unauthorized", "this ticket was not accepted")
		return
	}

	/*
	 * Spending the nonce is what makes a ticket single-use, and it happens
	 * after verification so an unsigned ticket cannot burn a nonce somebody
	 * else was going to use.
	 */
	if !s.nonces.Spend(claims.Issuer(), claims.Nonce, claims.Expires, now) {
		s.refuseGuest(r, "nonce already spent, expired, or peer over its allowance")
		writeError(w, http.StatusUnauthorized, "unauthorized", "this ticket was not accepted")
		return
	}

	token, err := newGuestToken()
	if err != nil {
		s.writeInternal(w, err, "guest token")
		return
	}
	expires := now.Add(guestSessionTTL)
	if !s.guests.add(auth.HashToken(token), guestSession{
		Peer:    claims.Issuer(),
		Subject: claims.Subject,
		Expires: expires,
	}, now) {
		// The book is full. Honest rather than silent: the ticket was good and
		// this server cannot hold another session right now.
		writeError(w, http.StatusServiceUnavailable, "busy",
			"this server is holding as many guest sessions as it can")
		return
	}

	slog.Info("guest admitted", "peer", claims.Issuer(), "subject", claims.Subject,
		"expires_in", guestSessionTTL.String())

	writeJSON(w, http.StatusOK, map[string]any{
		"token":      token,
		"expires_at": expires.Unix(),
		"peer":       claims.Issuer(),
	})
}

// refuseGuest logs why a ticket was turned away. Never written to the
// response — the point of the uniform refusal is that the far side learns only
// that it was not accepted.
func (s *Server) refuseGuest(r *http.Request, reason string) {
	slog.Info("guest ticket refused", "reason", reason, "remote", r.RemoteAddr)
}

// newGuestToken is 256 bits from crypto/rand. It is a bearer credential with
// no structure to parse and nothing derivable from it: the session it names
// lives only in this process's memory.
func newGuestToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
