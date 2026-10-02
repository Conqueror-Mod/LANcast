package api

import (
	"time"

	"lancast/internal/store"
)

/*
 * Whether a paired server is answering, as a state rather than a stream of
 * failures.
 *
 * Every call to another household's server that fails ends in
 * peerUnreachable, and that used to log "peer not answering" every time. The
 * rail polls each peer's libraries once a minute (usePeerLibraries) because a
 * share can change with nobody to tell us, so a peer switched off for a
 * weekend wrote a line a minute, per open window, for as long as it stayed
 * off: 3,263 lines over five days for one peer, which was most of
 * lancastd.log. Not one of them after the first said anything new.
 *
 * So the log follows the state. The first failure says "not answering", the
 * first success after it says "answering again" and how long it was gone,
 * and everything in between is Debug. The calls themselves are unchanged:
 * each one still asks, so a peer that comes back is noticed by the next one.
 * Backing off the polling is the client's half (hooks.ts).
 */

// peerSilent records a failed call and logs it only if the peer had been
// answering.
func (s *Server) peerSilent(p store.Peer, err error) {
	s.rosterMu.Lock()
	since, already := s.peerDown[p.Fingerprint]
	if !already {
		s.peerDown[p.Fingerprint] = time.Now()
	}
	s.rosterMu.Unlock()

	if already {
		s.log.Debug("peer still not answering", "peer", p.Fingerprint, "since", since, "error", err)
		return
	}
	s.log.Info("peer not answering", "peer", p.Fingerprint, "error", err)
}

// peerAnswered records a successful call and logs the recovery if the peer
// had been silent.
func (s *Server) peerAnswered(p store.Peer) {
	s.rosterMu.Lock()
	since, was := s.peerDown[p.Fingerprint]
	delete(s.peerDown, p.Fingerprint)
	s.rosterMu.Unlock()

	if was {
		s.log.Info("peer answering again", "peer", p.Fingerprint,
			"down_for", time.Since(since).Round(time.Second))
	}
}
