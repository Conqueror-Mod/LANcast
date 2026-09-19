package api

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"lancast/internal/auth"
	"lancast/internal/store"
)

/*
 * Stream tickets: how a native player reaches the file (ADR 0068).
 *
 * The desktop client plays through libmpv (ADR 0067), and mpv is not the
 * browser. It cannot carry the session cookie — the cookie is HttpOnly, so
 * neither the page nor the client process can read it — and it should not
 * carry an API key, which opens the whole API for as long as it lives.
 *
 * A ticket is the narrowest credential that does the job:
 *
 *  - one item. It opens `GET /api/stream/{id}` for the item it was minted for
 *    and nothing else; any other path, or another id, is as unauthorized as no
 *    credential at all.
 *  - borrowed, not owned. It records the hash of the credential that minted it
 *    and re-checks that credential on every use, so signing out, a password
 *    change or revoking a key ends the ticket with it — the promise the
 *    password change makes about "every session" is kept.
 *  - a header, never a query parameter, for the reason apiKey gives: a
 *    credential in a URL is a credential in every log it passes through.
 *  - in memory. A restart forgets every ticket, which costs a player one
 *    re-mint and means a ticket is never on disk.
 *
 * Twenty-four hours, because a paused film is still a film being watched: the
 * Dogma report was a pause overnight, and a ticket that expired under it would
 * reproduce that fault on the path built to escape it.
 */

const (
	ticketTTL = 24 * time.Hour
	// maxTickets bounds the book. Minting is authenticated, so this guards
	// against a runaway client rather than a stranger.
	maxTickets = 4096
	// ticketScheme is the Authorization scheme a ticket is presented under.
	ticketScheme = "Ticket "
)

type ticket struct {
	itemID  int64
	credH   string // hash of the minting credential; "" on an unsecured server
	viaKey  bool
	expires time.Time
}

type ticketBook struct {
	mu sync.Mutex
	m  map[string]ticket // by hash of the ticket
}

func newTicketBook() *ticketBook { return &ticketBook{m: map[string]ticket{}} }

func (b *ticketBook) add(hash string, t ticket, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.m) >= maxTickets {
		var oldest string
		var oldestAt time.Time
		for h, x := range b.m {
			if !now.Before(x.expires) {
				delete(b.m, h)
				continue
			}
			if oldest == "" || x.expires.Before(oldestAt) {
				oldest, oldestAt = h, x.expires
			}
		}
		if len(b.m) >= maxTickets {
			delete(b.m, oldest)
		}
	}
	b.m[hash] = t
}

func (b *ticketBook) get(hash string, now time.Time) (ticket, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t, ok := b.m[hash]
	if !ok {
		return ticket{}, false
	}
	if !now.Before(t.expires) {
		delete(b.m, hash)
		return ticket{}, false
	}
	return t, true
}

// StreamTicket is what minting returns.
type StreamTicket struct {
	Ticket    string `json:"ticket"`
	ItemID    int64  `json:"item_id"`
	ExpiresAt int64  `json:"expires_at"`
}

// mintStreamTicket is POST /api/items/{id}/stream-ticket.
func (s *Server) mintStreamTicket(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	// The same visibility the stream itself applies: a ticket for an item the
	// caller cannot open would be a way round the rating ceiling.
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	if it.Path == "" {
		writeError(w, http.StatusNotFound, "not_found", "item has no playable file")
		return
	}

	t := ticket{itemID: id}
	if s.secured(r.Context()) {
		h, viaKey, ok := mintingCredential(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
			return
		}
		t.credH, t.viaKey = h, viaKey
	}

	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		s.log.Error("stream ticket entropy", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not issue a ticket")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	now := time.Now()
	t.expires = now.Add(ticketTTL)
	s.tickets.add(auth.HashToken(token), t, now)

	writeJSON(w, http.StatusOK, StreamTicket{Ticket: token, ItemID: id, ExpiresAt: t.expires.Unix()})
}

// mintingCredential is the hash of whatever authenticated this request, so a
// ticket can be tied to it. A key wins when it resolved, matching requireAuth.
func mintingCredential(r *http.Request) (hash string, viaKey bool, ok bool) {
	if authedByKey(r) {
		h := r.Header.Get("Authorization")
		return auth.HashToken(strings.TrimSpace(h[len("Bearer "):])), true, true
	}
	c, err := r.Cookie(auth.CookieName)
	if err != nil || c.Value == "" {
		return "", false, false
	}
	return auth.HashToken(c.Value), false, true
}

/*
 * streamTicket resolves a ticket presented on a stream request, returning the
 * session it stands in for.
 *
 * Only `GET` or `HEAD` of exactly `/api/stream/{id}` for the ticket's own item.
 * Anything else returns false and the request carries on to the ordinary
 * checks, so a ticket shown anywhere else is simply not a credential there.
 */
func (s *Server) streamTicket(r *http.Request) (*store.Session, bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return nil, false
	}
	h := r.Header.Get("Authorization")
	if len(h) <= len(ticketScheme) || !strings.EqualFold(h[:len(ticketScheme)], ticketScheme) {
		return nil, false
	}
	rest, found := strings.CutPrefix(r.URL.Path, "/api/stream/")
	if !found {
		return nil, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != rest {
		return nil, false
	}
	t, ok := s.tickets.get(auth.HashToken(strings.TrimSpace(h[len(ticketScheme):])), time.Now())
	if !ok || t.itemID != id {
		return nil, false
	}
	if t.viaKey {
		sess, _, err := s.st.LookupAPIKey(r.Context(), t.credH)
		return sess, err == nil
	}
	sess, err := s.st.LookupSession(r.Context(), t.credH)
	return sess, err == nil
}
