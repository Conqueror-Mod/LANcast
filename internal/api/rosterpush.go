package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"lancast/internal/store"
)

/*
 * Roster push (ADR 0103): a server sends its peers the roster they would
 * otherwise have to fetch.
 *
 * The fetch (refreshPeer) needs this server to reach the peer. Joining a
 * friend's film needs nothing else in that direction, so a friend behind a
 * router nobody has opened could pair, be seen in the host's roster and join,
 * if only the roster came to the host instead. That is this file.
 *
 * Both sides of it are here: the route that takes a pushed roster, and the
 * sender the peer watcher calls every tick.
 */

const (
	// pushEvery is how often an unchanged roster is sent again: the fetch's
	// own schedule, so a push-only pairing is as fresh as a fetched one.
	pushEvery = rosterEvery
	// pushEagerFor is how long after adding a peer a refusal is retried on the
	// watcher's quick tick rather than pushEvery: the window in which the far
	// side is probably still pasting our invite.
	pushEagerFor = 10 * time.Minute
	// asOfSlack is how much older than the last accepted roster a new one may
	// be and still be refused as overtaken. Anything older than this is a
	// clock that moved, not a message that arrived late, and is taken.
	asOfSlack = 10 * time.Minute
	// maxRosterPeople and maxRosterBytes bound a pushed roster. A household is
	// a handful of accounts; these are far past any of them.
	maxRosterPeople = 1000
	maxRosterBytes  = 256 << 10
)

// rosterPush is the push's state, in memory: what it describes is this
// process talking to its peers, not the pairing, which is why none of it is
// written down.
type rosterPush struct {
	mu sync.Mutex
	// sent is, per peer, when this server last pushed and the roster it sent.
	sent map[string]pushRecord
	// asOf is, per peer, the as_of of the last roster accepted from it, by
	// push or by fetch.
	asOf map[string]int64
	// heard is, per peer, when it last pushed us a roster: how People tells a
	// friend who calls in but cannot be called from one who is simply off.
	heard map[string]time.Time
}

type pushRecord struct {
	at       time.Time
	key      string // the roster as sent, for "has it changed"
	accepted bool   // the peer took it (204), as opposed to refusing or failing
}

func newRosterPush() *rosterPush {
	return &rosterPush{sent: map[string]pushRecord{}, asOf: map[string]int64{}, heard: map[string]time.Time{}}
}

// heardWithin is how recently a push must have arrived for a peer to count
// as reaching us: two of its schedules and a minute, so one late push is not
// a change of state.
const heardWithin = 2*pushEvery + time.Minute

func (rp *rosterPush) noteHeard(fingerprint string, at time.Time) {
	rp.mu.Lock()
	rp.heard[fingerprint] = at
	rp.mu.Unlock()
}

// heardFrom reports whether this peer has pushed us a roster lately.
func (rp *rosterPush) heardFrom(fingerprint string) bool {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	at, ok := rp.heard[fingerprint]
	return ok && time.Since(at) < heardWithin
}

// rosterBody is a roster on the wire, pushed or fetched. AsOf is the sender's
// clock in milliseconds when it read the roster; 0 from a server too old to
// send one.
type rosterBody struct {
	People []store.RemotePerson `json:"people"`
	AsOf   int64                `json:"as_of,omitempty"`
}

/*
 * acceptAsOf reports whether a roster stamped asOf from this peer may replace
 * what is held, and records it if so.
 *
 * Refused only when it is older than the last accepted one by less than
 * asOfSlack. A fetch answered by an old server (asOf 0) is always taken, as
 * every fetch was before this existed.
 */
func (rp *rosterPush) acceptAsOf(fingerprint string, asOf int64) bool {
	if asOf <= 0 {
		return true
	}
	rp.mu.Lock()
	defer rp.mu.Unlock()
	last := rp.asOf[fingerprint]
	if asOf < last && last-asOf < asOfSlack.Milliseconds() {
		return false
	}
	rp.asOf[fingerprint] = asOf
	return true
}

/*
 * federationRosterPush answers PUT /api/federation/roster: a paired server
 * telling us who is on it.
 *
 * Gated like every federation route: a peer certificate this server holds, or
 * a refusal (federationPeer). A server only calls servers it holds, so a push
 * arriving here proves the far side holds us, and the pairing is promoted on
 * the strength of it (ADR 0103, the rule ADR 0079 §5 applies to any call-in).
 *
 * Stored exactly as a fetched roster is, wholesale, so an opt-out on the far
 * side still takes back its grants here (ADR 0035).
 */
func (s *Server) federationRosterPush(w http.ResponseWriter, r *http.Request) {
	fingerprint, ok := s.federationPeer(w, r)
	if !ok {
		return
	}
	var body rosterBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRosterBytes))
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "a roster is {people, as_of}")
		return
	}
	if len(body.People) > maxRosterPeople {
		writeError(w, http.StatusBadRequest, "bad_request",
			fmt.Sprintf("a roster of more than %d people", maxRosterPeople))
		return
	}
	for _, p := range body.People {
		if p.ID == "" || len(p.ID) > 64 || len(p.Name) > 200 || strings.ContainsAny(p.ID, "\x00/") {
			writeError(w, http.StatusBadRequest, "bad_request", "a person in the roster is malformed")
			return
		}
	}
	// Heard, whatever happens to the roster: it reached us.
	s.push.noteHeard(fingerprint, time.Now())
	if !s.push.acceptAsOf(fingerprint, body.AsOf) {
		// Overtaken: a fresher roster from the same server is already held.
		// Not an error on the sender's part, so not answered as one.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ctx := r.Context()
	if err := s.st.ReplaceRemotePeople(ctx, fingerprint, body.People); err != nil {
		s.writeInternal(w, err, "store pushed roster")
		return
	}
	// A push counts as a fetch: do not dial out at once to learn what we
	// were just told.
	s.rosterMu.Lock()
	s.rosterAt[fingerprint] = time.Now()
	s.rosterMu.Unlock()

	p, err := s.st.PeerByFingerprint(ctx, fingerprint)
	if err == nil && p.State != store.PeerPaired {
		if err := s.st.SetPeerState(ctx, fingerprint, store.PeerPaired); err != nil {
			s.writeInternal(w, err, "promote pushing peer")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// rosterKey is a roster as a comparable string: the ids and names, in the
// order RosterForPeers gives them.
func rosterKey(people []store.RemotePerson) string {
	var b strings.Builder
	for _, p := range people {
		b.WriteString(p.ID)
		b.WriteByte(0)
		b.WriteString(p.Name)
		b.WriteByte(1)
	}
	return b.String()
}

/*
 * pushDue reports whether this server should send its roster to p now.
 *
 * Due when the roster differs from the one p last accepted, when pushEvery
 * has passed, or, for a peer added within pushEagerFor that has not yet
 * accepted one, on every tick.
 */
func (rp *rosterPush) pushDue(p store.Peer, key string, now time.Time) bool {
	rp.mu.Lock()
	rec, ok := rp.sent[p.Fingerprint]
	rp.mu.Unlock()
	switch {
	case !ok:
		return true
	case rec.accepted && rec.key != key:
		return true
	case now.Sub(rec.at) >= pushEvery:
		return true
	case !rec.accepted && now.Sub(time.Unix(p.AddedAt, 0)) < pushEagerFor:
		return true
	}
	return false
}

/*
 * pushRoster sends this server's roster to one peer if it is due, and
 * reports whether the peer took it.
 *
 * Quiet on failure. A peer that refuses (does not hold us yet), is too old to
 * take pushes (404/405), or cannot be reached is tried again on the schedule
 * pushDue describes, and none of those is news worth a log line: the fetch
 * side already logs a peer that is not answering (peerhealth.go).
 */
func (s *Server) pushRoster(ctx context.Context, p store.Peer, people []store.RemotePerson) bool {
	key := rosterKey(people)
	now := time.Now()
	if !s.push.pushDue(p, key, now) {
		return false
	}
	ans, err := s.sendPeer(ctx, p, http.MethodPut, "/api/federation/roster",
		rosterBody{People: people, AsOf: now.UnixMilli()})
	accepted := err == nil && ans.status == http.StatusNoContent
	s.push.mu.Lock()
	s.push.sent[p.Fingerprint] = pushRecord{at: now, key: key, accepted: accepted}
	s.push.mu.Unlock()
	return accepted
}

// pushRosters sends the roster to every peer it is due for. Called from the
// peer watcher's tick, so a roster that changes reaches peers within one
// tick and nothing else has to remember to announce it.
func (s *Server) pushRosters(ctx context.Context, peers []store.Peer) {
	if len(peers) == 0 {
		return
	}
	people, err := s.st.RosterForPeers(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			s.log.Warn("could not read the roster to send to peers", "error", err)
		}
		return
	}
	var wg sync.WaitGroup
	for _, p := range peers {
		wg.Add(1)
		go func(p store.Peer) {
			defer wg.Done()
			s.pushRoster(ctx, p, people)
		}(p)
	}
	wg.Wait()
}
