package api

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"lancast/internal/store"
)

/*
 * The peer watcher: one server asking its paired servers, so that no open
 * page has to (ADR 0079 §5).
 *
 * Before this, the only thing that ever asked a peer for its roster, and so
 * the only thing that could promote a pairing from `added` to `paired`, was
 * the People page while somebody had it open. A pairing nobody looked at from
 * that page never became mutual, and Settings went on saying "Added, not yet
 * mutual" about a pair of servers that were talking perfectly well. The first
 * remote evening spent an hour on that.
 *
 * Now the server keeps one schedule per peer, whatever the number of windows:
 *
 *   - the roster, and with it promotion: as soon as a peer is added or calls
 *     in, then every rosterEvery;
 *   - presence: every presenceEvery, for each person here with a window open.
 *
 * Presence is per person because the far server decides what each asker may
 * see (ADR 0045 §6), and a person with no window open has nobody to tell.
 *
 * When an answer differs from the last one, the open windows hear about it on
 * the event stream and ask again through their usual routes. A peer that is
 * not answering is asked about presence only when its roster is due, so a
 * household switched off for a weekend costs one call every few minutes, not
 * one every ten seconds.
 *
 * Federation stays pull-only. Nothing here adds a route between servers.
 */

const (
	// presenceEvery is how often each open person's view of each peer is
	// re-asked: the freshness the People page used to buy by polling.
	presenceEvery = 10 * time.Second
	// rosterEvery is how often a peer's roster is re-fetched by the watcher.
	// A roster changes when somebody opts in or out, not minute to minute.
	rosterEvery = 5 * time.Minute
)

/*
 * WatchPeers runs until ctx ends. cmd/lancastd starts it once.
 *
 * Every tick looks at every peer concurrently, so a peer that has gone quiet
 * costs the tick one peerDeadline, not one per peer.
 */
func (s *Server) WatchPeers(ctx context.Context) {
	s.peerTick(ctx)
	t := time.NewTicker(presenceEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.peerTick(ctx)
		case fp := <-s.peerKick:
			s.refreshNow(ctx, fp)
		}
	}
}

/*
 * kickPeer asks the watcher to fetch one peer's roster now, outside its
 * schedule. Never blocks: a full queue means a refresh is already coming.
 *
 * Called when a peer is added, and when a peer we still hold as `added` calls
 * in. A peer only calls servers it holds, so the second is the moment the
 * pairing became mutual on the far side, and waiting out rosterEvery to notice
 * would leave Settings wrong for five minutes.
 */
func (s *Server) kickPeer(fingerprint string) {
	select {
	case s.peerKick <- fingerprint:
	default:
	}
}

func (s *Server) refreshNow(ctx context.Context, fingerprint string) {
	p, err := s.st.PeerByFingerprint(ctx, fingerprint)
	if err != nil {
		return
	}
	s.rosterMu.Lock()
	delete(s.rosterAt, fingerprint)
	s.rosterMu.Unlock()
	s.watchRoster(ctx, p)
}

func (s *Server) peerTick(ctx context.Context) {
	peers, err := s.st.Peers(ctx)
	if err != nil {
		return
	}
	users := s.events.Users()
	s.forgetPresenceOf(users)

	var wg sync.WaitGroup
	for _, p := range peers {
		wg.Add(1)
		go func(p store.Peer) {
			defer wg.Done()
			s.watchPeer(ctx, p, users)
		}(p)
	}
	wg.Wait()
}

func (s *Server) watchPeer(ctx context.Context, p store.Peer, users []string) {
	s.rosterMu.Lock()
	last, seen := s.rosterAt[p.Fingerprint]
	_, down := s.peerDown[p.Fingerprint]
	s.rosterMu.Unlock()

	rosterDue := !seen || time.Since(last) >= rosterEvery
	if rosterDue {
		if !s.watchRoster(ctx, p) {
			return
		}
	} else if down {
		// Not answering, and not yet due another try.
		return
	}
	if len(users) == 0 {
		return
	}
	for _, user := range users {
		seen, err := s.askPeerPresence(ctx, p, user)
		if err != nil {
			s.peerSilent(p, err)
			return
		}
		s.peerAnswered(p)
		if s.notePresence(p.Fingerprint, user, seen) {
			s.events.PublishTo(user, TopicPresence)
		}
	}
}

// watchRoster refreshes one peer's roster and reports whether it answered.
// Promotion to `paired` happens inside refreshPeer, and the store announces it.
func (s *Server) watchRoster(ctx context.Context, p store.Peer) bool {
	if err := s.refreshPeer(ctx, p); err != nil {
		// refreshPeer stamps rosterAt before calling, so a failure waits out
		// rosterEvery like a success does. That is the back-off.
		s.peerSilent(p, err)
		return false
	}
	s.peerAnswered(p)
	return true
}

/*
 * notePresence records one person's view of one peer and reports whether it
 * differs from the last view recorded.
 *
 * Compared as a canonical string of exactly what the People page draws, so a
 * reordering by the far server is not a change and a film starting is.
 */
func (s *Server) notePresence(fingerprint, user string, seen []visible) bool {
	sort.Slice(seen, func(i, j int) bool { return seen[i].ID < seen[j].ID })
	var b strings.Builder
	for _, v := range seen {
		fmt.Fprintf(&b, "%s\x00%s\x00%t\x00%s\x01", v.ID, v.Name, v.Online, v.Watching)
	}
	key := fingerprint + "\x00" + user
	now := b.String()

	s.rosterMu.Lock()
	defer s.rosterMu.Unlock()
	before, had := s.presenceSeen[key]
	s.presenceSeen[key] = now
	return !had || before != now
}

// forgetPresenceOf drops remembered views for people with no window open, so
// the map holds the people being watched for and nobody else. A person who
// comes back starts fresh and their first answer counts as a change, which
// costs one refetch.
func (s *Server) forgetPresenceOf(open []string) {
	keep := map[string]bool{}
	for _, u := range open {
		keep[u] = true
	}
	s.rosterMu.Lock()
	defer s.rosterMu.Unlock()
	for key := range s.presenceSeen {
		if i := strings.IndexByte(key, 0); i >= 0 && !keep[key[i+1:]] {
			delete(s.presenceSeen, key)
		}
	}
}
