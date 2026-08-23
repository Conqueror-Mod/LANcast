package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"lancast/internal/peer"
	"lancast/internal/presence"
	"lancast/internal/store"
)

/*
 * Presence: the routes that let one person see that another is watching
 * something, and the one that answers a peer asking.
 *
 * Governed end to end by [ADR 0045](../../docs/adr/0045-live-presence-between-paired-servers.md).
 * Three of its rules are structural rather than checks, and they are why this
 * file is shaped the way it is:
 *
 * **§3 bounds the disclosure exhaustively** — online, watching-or-idle, and the
 * work by title. So the wire type below has exactly three fields and no item
 * id, position or library. A field that exists is a field that gets rendered,
 * and then relied on.
 *
 * **§6 gives an administrator no privileged position.** Every grant route reads
 * the caller's own id from their session and cannot be told to act for somebody
 * else. There is no admin variant, which is the enforcement.
 *
 * **§4 forbids persistence.** Presence lives in `internal/presence` and is
 * never written down. Nothing in this file may put it in the database, the
 * audit log, or a log line — a log line is a record, and it accumulates exactly
 * the way ADR 0035 warns about.
 */

// visible is one person, as much as ADR 0045 §3 allows to be said about them.
//
// Watching is the *work's* title and empty when idle. There is deliberately
// nothing else here.
type visible struct {
	// ID is the answering account's own id, which is exactly the id the asking
	// server already holds for them in `remote_person` — that is what makes it
	// the join key. Matching on the display name instead would break on two
	// people called Sam, and would quietly re-point a grant when somebody
	// renamed themselves. It discloses nothing: the asker was given this id in
	// the roster before any of this.
	ID       string `json:"id"`
	Name     string `json:"name"`
	Online   bool   `json:"online"`
	Watching string `json:"watching,omitempty"`
}

/*
 * federationPresence answers a paired server asking what its person may see.
 *
 * Authentication is the mutual TLS pin, not a session: the caller proved which
 * *server* it is by presenting the identity key we hold for it (ADR 0044 §4).
 * Which *person* is asking is then that server's word, and this is the point
 * where that trust is deliberate and worth naming — Georgia's server vouches
 * for Georgia, exactly as ADR 0046 has it vouch for her when she is issued a
 * guest ticket. A pairing is a statement that you trust the far server about
 * its own people; if that is not true, the pairing is the thing to undo.
 *
 * A GET, and not only because it reads. State-changing methods go through the
 * CSRF check in requireAuth, which compares an Origin a peer has no reason to
 * send. Making this a read keeps it out of a defence built for browsers,
 * instead of loosening that defence to let a peer through.
 */
func (s *Server) federationPresence(w http.ResponseWriter, r *http.Request) {
	fingerprint, err := peer.FingerprintFromState(r.TLS)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "peer connections must present their identity")
		return
	}

	// Known peers only. An unknown key reaches a lookup and a refusal, never a
	// disclosure — and unpairing therefore stops answering immediately, which
	// is what ADR 0045 §5 means by revocation being complete.
	if _, err := s.st.PeerByFingerprint(r.Context(), fingerprint); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "not a paired server")
		return
	}

	person := r.URL.Query().Get("person")
	if person == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "which person is asking")
		return
	}

	readers, err := s.st.ReadersOf(r.Context(), fingerprint, person)
	if err != nil {
		s.writeInternal(w, err, "presence readers")
		return
	}

	out := []visible{}
	for _, userID := range readers {
		st, ok := s.presence.Snapshot(userID)
		if !ok {
			// Offline. Named anyway, because "Chris is offline" and "Chris has
			// not shared with you" are different sentences and the People page
			// is required to tell them apart. Nothing is disclosed by it: the
			// asker already knows this person granted them.
			out = append(out, visible{ID: userID, Name: s.displayName(r.Context(), userID)})
			continue
		}
		out = append(out, visible{
			ID:       userID,
			Name:     s.displayName(r.Context(), userID),
			Online:   true,
			Watching: st.Watching,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	writeJSON(w, http.StatusOK, map[string]any{"people": out})
}

/*
 * federationRoster hands a peer the accounts here that have chosen to be seen.
 *
 * Authenticated the same way as presence — the mutual-TLS pin — and it answers
 * only `RosterForPeers`, which is the accounts with `visible_to_peers` set. An
 * account that has not opted in is absent, and being absent is what makes it
 * impossible for anybody on the far side to name them in a grant.
 *
 * Answering this call at all is also a *statement*: this server only reaches
 * the handler for a fingerprint it already holds as a peer. So a successful
 * call proves the far side holds the caller too, which is precisely the
 * mutuality ADR 0044 §3 requires and which nothing until now could establish.
 * That is why the caller of this — refreshPeer — is what finally moves a peer
 * from `added` to `paired`.
 */
func (s *Server) federationRoster(w http.ResponseWriter, r *http.Request) {
	fingerprint, err := peer.FingerprintFromState(r.TLS)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "peer connections must present their identity")
		return
	}
	if _, err := s.st.PeerByFingerprint(r.Context(), fingerprint); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "not a paired server")
		return
	}

	people, err := s.st.RosterForPeers(r.Context())
	if err != nil {
		s.writeInternal(w, err, "roster")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": people})
}

/*
 * refreshPeer fetches a peer's roster and records what came back.
 *
 * Rate-limited in memory rather than by a column, because how recently we asked
 * is a fact about this process and not about the pairing — a restart may
 * legitimately ask again immediately.
 *
 * The roster is stored wholesale (see store.ReplaceRemotePeople): somebody who
 * turned `visible_to_peers` off is absent from it, is removed here, and their
 * grants cascade away with them. That is an opt-out being able to take back
 * what it gave, which ADR 0035 requires.
 */
func (s *Server) refreshPeer(ctx context.Context, p store.Peer) error {
	s.rosterMu.Lock()
	last, seen := s.rosterAt[p.Fingerprint]
	fresh := seen && time.Since(last) < rosterInterval && p.State == store.PeerPaired
	if !fresh {
		s.rosterAt[p.Fingerprint] = time.Now()
	}
	s.rosterMu.Unlock()
	if fresh {
		return nil
	}

	var body struct {
		People []store.RemotePerson `json:"people"`
	}
	if err := s.callPeer(ctx, p, "/api/federation/roster", &body); err != nil {
		return err
	}
	if err := s.st.ReplaceRemotePeople(ctx, p.Fingerprint, body.People); err != nil {
		return err
	}
	// It answered a call only a server holding us can answer, so the pairing is
	// mutual and can finally say so.
	if p.State != store.PeerPaired {
		if err := s.st.SetPeerState(ctx, p.Fingerprint, store.PeerPaired); err != nil {
			return err
		}
	}
	return nil
}

// displayName resolves a local account's name for disclosure. A missing user is
// rendered as nothing rather than as an id: an internal identifier is not a
// name, and showing one to another server leaks a shape for no benefit.
func (s *Server) displayName(ctx context.Context, userID string) string {
	u, err := s.st.UserByID(ctx, userID)
	if err != nil || u == nil {
		return ""
	}
	return u.Name
}

/*
 * peerPresence is what the People page reads: every paired peer, the people on
 * it, and what those people are doing.
 *
 * The outbound calls run concurrently and with a short deadline. A peer that is
 * asleep must cost the page the timeout once, not the timeout times the number
 * of peers — the common household case is two servers where one is off, and
 * that case has to render briskly or the page stops being opened.
 */
func (s *Server) peerPresence(w http.ResponseWriter, r *http.Request) {
	me := s.userID(r)

	peers, err := s.st.Peers(r.Context())
	if err != nil {
		s.writeInternal(w, err, "list peers")
		return
	}

	grants, err := s.st.PresenceGrants(r.Context(), me)
	if err != nil {
		s.writeInternal(w, err, "presence grants")
		return
	}
	granted := map[string]bool{}
	for _, g := range grants {
		granted[g.Fingerprint+"\x00"+g.PersonID] = true
	}

	type peerOut struct {
		Fingerprint string           `json:"fingerprint"`
		Name        string           `json:"name"`
		State       string           `json:"state"`
		Reachable   bool             `json:"reachable"`
		People      []map[string]any `json:"people"`
	}

	out := make([]peerOut, len(peers))
	var wg sync.WaitGroup
	for i, p := range peers {
		people, err := s.st.RemotePeople(r.Context(), p.Fingerprint)
		if err != nil {
			s.writeInternal(w, err, "remote people")
			return
		}

		out[i] = peerOut{Fingerprint: p.Fingerprint, Name: p.Name, State: p.State}

		wg.Add(1)
		go func(i int, p store.Peer, people []store.RemotePerson) {
			defer wg.Done()
			// The roster first: a grant can only name somebody already known
			// to be on that server, so a peer whose people have never been
			// fetched has nobody to grant anything to. Rate-limited inside.
			if err := s.refreshPeer(r.Context(), p); err == nil {
				if refreshed, err := s.st.RemotePeople(r.Context(), p.Fingerprint); err == nil {
					people = refreshed
				}
			}

			seen, err := s.askPeerPresence(r.Context(), p, me)
			out[i].Reachable = err == nil

			byID := map[string]visible{}
			for _, v := range seen {
				byID[v.ID] = v
			}
			for _, person := range people {
				row := map[string]any{
					"id":      person.ID,
					"name":    person.Name,
					"granted": granted[p.Fingerprint+"\x00"+person.ID],
				}
				// Three states the People page must tell apart, and the
				// difference is the whole discipline of that page: offline,
				// online but not sharing with you, and online and idle.
				if v, ok := byID[person.ID]; ok {
					row["shares"] = true
					row["online"] = v.Online
					row["watching"] = v.Watching
				} else {
					row["shares"] = false
				}
				out[i].People = append(out[i].People, row)
			}
		}(i, p, people)
	}
	wg.Wait()

	for i := range out {
		if out[i].People == nil {
			out[i].People = []map[string]any{}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"peers": out})
}

/*
 * callPeer makes one authenticated request to a peer, trying its addresses in
 * order.
 *
 * The addresses are hints and the fingerprint is the identity (ADR 0044 §5), so
 * a failure at one address is a reason to try the next rather than a verdict
 * about the peer. Even a wrong *key* moves on: a machine that has taken over an
 * address the peer used to hold should not stop us reaching the peer where it
 * actually is.
 */
func (s *Server) callPeer(ctx context.Context, p store.Peer, path string, out any) error {
	client, err := peer.Client(s.ident, p.Fingerprint)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	var lastErr error
	for _, addr := range p.Addrs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+addr+path, nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("peer answered %d", resp.StatusCode)
			continue
		}
		err = json.NewDecoder(resp.Body).Decode(out)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		// Best effort: reaching a peer must not fail over bookkeeping.
		_ = s.st.MarkPeerSeen(context.WithoutCancel(ctx), p.Fingerprint, time.Now())
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("peer %s has no address to try", p.Name)
	}
	return lastErr
}

func (s *Server) askPeerPresence(ctx context.Context, p store.Peer, asAs string) ([]visible, error) {
	var body struct {
		People []visible `json:"people"`
	}
	err := s.callPeer(ctx, p, "/api/federation/presence?person="+url.QueryEscape(asAs), &body)
	return body.People, err
}

/*
 * putPresenceGrant is one account deciding, about itself, who may see it.
 *
 * Self-service by construction: the granting id comes from the session and
 * there is no route that accepts one. ADR 0045 §6 — an admin cannot grant
 * presence on somebody else's behalf, because a switch somebody else can flip
 * is not consent.
 */
func (s *Server) putPresenceGrant(w http.ResponseWriter, r *http.Request) {
	fingerprint := r.PathValue("fingerprint")
	person := r.PathValue("person")

	var req struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}

	me := s.userID(r)
	if req.On {
		// A grant naming somebody the roster does not carry is refused by the
		// foreign key — which is ADR 0045 §2 enforced by the schema rather than
		// by a check somebody has to remember to write.
		if err := s.st.GrantPresence(r.Context(), me, fingerprint, person, time.Now()); err != nil {
			writeError(w, http.StatusConflict, "no_such_person",
				"that person is not on this peer's roster, so they cannot be named")
			return
		}
	} else if err := s.st.RevokePresence(r.Context(), me, fingerprint, person); err != nil {
		s.writeInternal(w, err, "revoke presence")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

/*
 * putPresenceHeartbeat and deletePresence are how the client says what it is
 * doing.
 *
 * The heartbeat rides on the progress write the player already makes every five
 * seconds — see putProgress — so this route exists only for the case that write
 * cannot cover: telling the server that playback *stopped*, now, rather than
 * letting the title stand until the sweep notices. Twenty seconds of "Chris is
 * watching Blade Runner" after Blade Runner ended is a small false statement
 * about the present, which is the one thing presence must not make.
 */
func (s *Server) deletePresence(w http.ResponseWriter, r *http.Request) {
	s.presence.Stopped(s.userID(r))
	w.WriteHeader(http.StatusNoContent)
}

/*
 * recordWatching is called from the progress write, and is where ADR 0045 §3's
 * limits are actually applied.
 *
 * Two reductions happen here and both are the ADR rather than a preference:
 *
 * **Video only.** Music and photographs are excluded rather than left to leak,
 * because nothing has thought about what being seen listening should mean.
 *
 * **The work, not the episode.** An episode discloses its series. "Cowboy
 * Bebop", never "Cowboy Bebop S01E02 — Stray Dog Strut", for the same reason
 * the client hides an unwatched episode's synopsis by default: announcing an
 * episode title to a friend three seasons behind is a choice nobody made on
 * purpose.
 *
 * This is deliberately *not* derived from playback_state. The ADR rejects that
 * explicitly — reading presence out of the record means presence and history
 * are the same data one query apart, and the separation the whole ADR rests on
 * would exist only in whichever handler happened to be written first. The
 * progress write is a convenient *moment*, not a source: what is recorded here
 * goes into memory and nowhere else.
 */
func (s *Server) recordWatching(userID string, it *store.Item) {
	if it == nil {
		return
	}
	title := presenceTitle(it)
	if title == "" {
		s.presence.Stopped(userID)
		return
	}
	s.presence.Watching(userID, title)
}

func presenceTitle(it *store.Item) string {
	switch it.Kind {
	case "movie":
		return it.Title
	case "episode":
		// The series, which is what the work is. An episode whose series is
		// unknown discloses nothing rather than falling back to its own title,
		// because the fallback is exactly the disclosure the rule forbids.
		if it.Series != nil && *it.Series != "" {
			return *it.Series
		}
		return ""
	default:
		// Music, photos, channels, and anything added later. Silence is the
		// safe default for a kind this rule has not considered.
		return ""
	}
}

// rosterInterval is how often a peer's roster is re-fetched while it is
// already paired. Rosters change when somebody opts in or out, which is rare
// and never urgent; presence rides a much shorter timer beside it.
const rosterInterval = 60 * time.Second

// tracker is the live presence for this server. Split out so tests can assert
// against it without reaching through the HTTP layer.
func (s *Server) tracker() *presence.Tracker { return s.presence }
