package api

import (
	"errors"
	"io"
	"net/http"
	"net/url"

	"lancast/internal/identity"
	"lancast/internal/peer"
	"lancast/internal/store"
)

/*
 * Looking at somebody else's library, from this side.
 *
 * [ADR 0071's amendment](../../docs/adr/0071-a-shared-library-is-a-standing-grant.md):
 * this household's client cannot reach the other household's server — a window
 * pins one server's key and a self-signed certificate from anybody else is
 * refused — so it asks *this* server, and this server asks theirs over the
 * mutual-TLS peer channel.
 *
 * # What this server is, and is not, in that exchange
 *
 * A pipe. It decides nothing about what may be seen: the far server resolves
 * its own share and its own limit, and whatever it refuses is refused here by
 * being refused there. Adding a check on this side would be this household
 * deciding what the other one meant to share, which is backwards, and it would
 * be a second answer that could disagree with the first.
 *
 * What it *does* decide is that the caller is signed in here, and that the
 * fingerprint names a server this household paired with. Both are facts about
 * this side and nobody else can supply them.
 *
 * # Any account, not administrators
 *
 * Pairing is administrative and looking at what a pairing produced is not, the
 * same split `docs/api.md` already draws: adding a peer opens a network
 * relationship for the whole server, while browsing what that peer chose to
 * share is closer to watching something. It also matches the ticket-minting
 * rule — a share is granted to a *server* (§1), so what this household may see
 * was decided about the household.
 */

// peerForBrowse resolves the peer named in the path and refuses one this
// server has not been introduced to.
func (s *Server) peerForBrowse(w http.ResponseWriter, r *http.Request) (store.Peer, bool) {
	fingerprint := identity.Normalize(r.PathValue("fingerprint"))
	if fingerprint == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "which server")
		return store.Peer{}, false
	}
	p, err := s.st.PeerByFingerprint(r.Context(), fingerprint)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such peer")
		return store.Peer{}, false
	}
	return p, true
}

// peerLibraries asks a paired server what it has shared with us.
func (s *Server) peerLibraries(w http.ResponseWriter, r *http.Request) {
	p, ok := s.peerForBrowse(w, r)
	if !ok {
		return
	}

	var body struct {
		Libraries []guestLibrary `json:"libraries"`
	}
	if err := s.callPeer(r.Context(), p, "/api/federation/libraries", &body); err != nil {
		s.peerUnreachable(w, p, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"libraries": body.Libraries})
}

/*
 * peerItems browses or searches one of their libraries.
 *
 * The query is forwarded rather than rebuilt, so a filter added to the far
 * server's browse endpoint works here the day it ships without this file
 * learning about it. Only the ones this server has an opinion about are
 * touched: none, today.
 */
func (s *Server) peerItems(w http.ResponseWriter, r *http.Request) {
	p, ok := s.peerForBrowse(w, r)
	if !ok {
		return
	}

	var body struct {
		Items []any `json:"items"`
		Total int   `json:"total"`
	}
	if err := s.callPeer(r.Context(), p, peerItemsPath(r.URL.RawQuery), &body); err != nil {
		s.peerUnreachable(w, p, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": body.Items, "total": body.Total})
}

/*
 * peerStream passes the film through.
 *
 * Copied rather than buffered, and the Range header goes both ways, because
 * that is what makes seeking work: a viewer dragging the scrubber here should
 * produce the same partial request on the far server that a viewer in that
 * house would. Holding the file first would turn a seek into a wait for a
 * whole film.
 *
 * This is the hop the amendment costs, and it costs this household's own
 * network rather than the link between the two houses — the correction that
 * changed the decision.
 */
func (s *Server) peerStream(w http.ResponseWriter, r *http.Request) {
	p, ok := s.peerForBrowse(w, r)
	if !ok {
		return
	}
	item := r.URL.Query().Get("item")
	if item == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "which item")
		return
	}

	resp, err := s.openPeerStream(r, p, "/api/federation/stream?item="+url.QueryEscape(item)+s.memberQuery(r))
	if err != nil {
		s.peerUnreachable(w, p, err)
		return
	}
	defer resp.Body.Close()

	/*
	 * Only the headers that describe the bytes. Anything else the far server
	 * sends is about *its* session with us, not about this one, and forwarding
	 * a Set-Cookie or a cache directive from another household's server is a
	 * way to be surprised later.
	 */
	copyStreamHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	//nolint:errcheck // A copy that stops early is a viewer who navigated away.
	io.Copy(w, resp.Body)
}

/*
 * openPeerStream is callPeer's shape without the JSON: it tries the recorded
 * addresses in the order that last answered and hands back the live response.
 *
 * No deadline on the whole call, unlike callPeer. That budget is right for a
 * question about *now* — "is anybody watching" is wrong if it is slow — and
 * wrong for a film, which is supposed to take an hour and a half.
 */
func (s *Server) openPeerStream(r *http.Request, p store.Peer, path string) (*http.Response, error) {
	// Not peer.Client: its eight-second budget covers reading the body, which
	// for a film means the stream is cut off eight seconds in. See StreamClient.
	client, err := peer.StreamClient(s.ident, p.Fingerprint)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, addr := range s.reachOrder(r.Context(), p) {
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://"+addr+path, nil)
		if err != nil {
			lastErr = err
			continue
		}
		// Forwarded so a seek here is a seek there.
		if rng := r.Header.Get("Range"); rng != "" {
			req.Header.Set("Range", rng)
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			// Their refusal is the answer. Reported as-is rather than
			// translated, so "they stopped sharing this" does not arrive here
			// as "their server is down".
			return nil, &peerRefusal{status: resp.StatusCode}
		}
		s.peerAnswered(p)
		return resp, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no usable address")
	}
	return nil, lastErr
}

/*
 * peerItemsPath is what this server asks the far one for, and it is a function
 * so that it can be tested without a peer.
 *
 * The query is forwarded whole rather than rebuilt field by field: a filter
 * added to the far server's browse endpoint then works the day it ships,
 * without this file learning about it. Nothing here has an opinion to impose —
 * what may be seen is the far server's decision, and a parameter this side
 * dropped would be this side quietly narrowing somebody else's library.
 */
func peerItemsPath(rawQuery string) string {
	path := "/api/federation/items"
	if rawQuery != "" {
		path += "?" + rawQuery
	}
	return path
}

// peerRefusal is the far server saying no, as opposed to not answering. The
// two are different sentences and a person reading the screen needs them apart.
type peerRefusal struct{ status int }

func (e *peerRefusal) Error() string { return http.StatusText(e.status) }

/*
 * peerUnreachable turns a failed call into an answer.
 *
 * A refusal from the far server is passed through, because "they no longer
 * share this with you" is information. Anything else is 502: this server is
 * fine, the other one is not answering, and saying so beats a 500 that reads
 * as a fault here.
 */
func (s *Server) peerUnreachable(w http.ResponseWriter, p store.Peer, err error) {
	var refusal *peerRefusal
	if errors.As(err, &refusal) {
		/*
		 * The sentence follows the status, because they are not all the same
		 * sentence.
		 *
		 * This said "that server did not share this with you" for everything,
		 * which was true of the only status the browse routes could produce.
		 * The playback routes can also answer 409 — *this file plays directly,
		 * converting it would be wasted CPU* — and 503 while an encode starts,
		 * and telling somebody they lack permission when the film is merely
		 * warming up sends them to ask a favour they already have.
		 */
		writeError(w, refusal.status, "peer_refused", peerRefusalText(refusal.status))
		return
	}
	s.peerSilent(p, err)
	writeError(w, http.StatusBadGateway, "peer_unreachable", p.Name+" is not answering")
}

/*
 * peerRefusalText says what the far server's status means, in this household's
 * words.
 *
 * Their own error body is deliberately not passed through. It is written for
 * somebody on that server, it may name their items or their paths, and a
 * message this server repeats is a message this server is vouching for.
 */
func peerRefusalText(status int) string {
	switch status {
	case http.StatusNotFound:
		return "that server did not share this with you"
	case http.StatusConflict:
		return "that server says this file can be played as it is"
	case http.StatusServiceUnavailable:
		return "that server could not start converting this"
	case http.StatusUnauthorized, http.StatusForbidden:
		// Their side refused *us*, the server, not the person reading this.
		return "that server refused this pairing"
	default:
		return "that server refused this"
	}
}
