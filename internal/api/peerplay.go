package api

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"lancast/internal/store"
)

/*
 * Playing somebody else's film, from this side.
 *
 * The other half of [federationplay.go](federationplay.go). This household's
 * client cannot reach the other household's server at all — a window pins one
 * server's key (ADR 0070) — so it asks this server, and this server asks
 * theirs ([ADR 0071](../../docs/adr/0071-a-shared-library-is-a-standing-grant.md),
 * amended).
 *
 * # This server decides nothing about the film
 *
 * Same standing as `peerbrowse.go`: a pipe. Whether it may be played, at what
 * rating, and how it should be delivered are all the far server's answers,
 * made against its own shares and its own probe. What this side decides is
 * that the caller is signed in here and that the fingerprint names a server
 * this household paired with — facts nobody else can supply.
 *
 * # Everything is streamed, nothing is held
 *
 * A film is not buffered here on its way through, and `Range` travels in both
 * directions, so a viewer dragging the scrubber produces the same partial
 * request on the far server that a viewer in that house would. Holding the
 * file first would turn a seek into a wait for a whole film.
 *
 * The one exception is the playlist, which is read whole — because it has to
 * be rewritten, and because it is a few kilobytes of text.
 */

// peerPlayback asks the far server how it would deliver one of its items. The
// decision is theirs: they hold the file and they probed it.
func (s *Server) peerPlayback(w http.ResponseWriter, r *http.Request) {
	s.pipeFromPeer(w, r, "/api/federation/playback?item=")
}

// peerTranscode pulls a converted stream through. The encode runs on their
// CPU, because the file is on their disk.
func (s *Server) peerTranscode(w http.ResponseWriter, r *http.Request) {
	s.pipeFromPeer(w, r, "/api/federation/transcode?item=")
}

// peerSubtitles lists what tracks their item has.
func (s *Server) peerSubtitles(w http.ResponseWriter, r *http.Request) {
	s.pipeFromPeer(w, r, "/api/federation/subtitles?item=")
}

/*
 * pipeFromPeer is every flat route: resolve the peer, require an item, ask,
 * and copy the answer through.
 *
 * `item` is the only parameter reproduced by name; everything else on the
 * query is forwarded whole, for the reason `peerItemsPath` gives — a parameter
 * added to the far server's endpoint works the day it ships, and one this side
 * dropped would be this side quietly changing somebody else's playback.
 */
func (s *Server) pipeFromPeer(w http.ResponseWriter, r *http.Request, base string) {
	p, ok := s.peerForBrowse(w, r)
	if !ok {
		return
	}
	item := r.URL.Query().Get("item")
	if item == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "which item")
		return
	}

	resp, err := s.openPeerStream(r, p, base+url.QueryEscape(item)+forwardedQuery(r))
	if err != nil {
		s.peerUnreachable(w, p, err)
		return
	}
	defer resp.Body.Close()
	copyStreamHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	//nolint:errcheck // A copy that stops early is a viewer who navigated away.
	io.Copy(w, resp.Body)
}

/*
 * peerHLSPlaylist is the only route that reads a body rather than piping it.
 *
 * The far server writes segment URLs under `/api/federation/hls/{item}/…`,
 * which is what *this* server calls it and not what the player does. Rewritten
 * here because this is the only place that knows both names.
 *
 * Getting this wrong would not have produced an error. The player would have
 * asked this server for `/api/stream/{id}/hls/…`, this server holds its own
 * item at that id, and it would have **played the wrong film** — which is why
 * the far server names the federation route rather than leaving its own path
 * in and hoping.
 */
func (s *Server) peerHLSPlaylist(w http.ResponseWriter, r *http.Request) {
	p, ok := s.peerForBrowse(w, r)
	if !ok {
		return
	}
	item, ok := peerItemSegment(w, r)
	if !ok {
		return
	}

	resp, err := s.openPeerStream(r, p,
		"/api/federation/hls/"+item+"/index.m3u8"+strings.TrimPrefix(forwardedQuery(r), "&"))
	if err != nil {
		s.peerUnreachable(w, p, err)
		return
	}
	defer resp.Body.Close()

	/*
	 * Bounded, because this is the one response read into memory here. A
	 * playlist for a long film is tens of kilobytes; a megabyte is far past
	 * anything legitimate and still nothing to hold. A peer is a server this
	 * household chose to trust, which is a reason not to expect an enormous
	 * playlist and not a reason to be unable to survive one.
	 */
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		s.peerUnreachable(w, p, err)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(resp.StatusCode)
	//nolint:errcheck // As above.
	w.Write([]byte(repointPlaylistAtPeer(string(body), p.Fingerprint)))
}

// peerHLSSegment pulls one segment through, named by the rewritten playlist.
func (s *Server) peerHLSSegment(w http.ResponseWriter, r *http.Request) {
	p, ok := s.peerForBrowse(w, r)
	if !ok {
		return
	}
	item, ok := peerItemSegment(w, r)
	if !ok {
		return
	}
	session := r.PathValue("session")
	name := r.PathValue("name")
	if session == "" || name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "which segment")
		return
	}

	resp, err := s.openPeerStream(r, p, "/api/federation/hls/"+item+"/"+
		url.PathEscape(session)+"/"+url.PathEscape(name))
	if err != nil {
		s.peerUnreachable(w, p, err)
		return
	}
	defer resp.Body.Close()
	copyStreamHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	//nolint:errcheck // As above.
	io.Copy(w, resp.Body)
}

// peerSubtitleFile pulls one track through, as the far server converted it.
func (s *Server) peerSubtitleFile(w http.ResponseWriter, r *http.Request) {
	p, ok := s.peerForBrowse(w, r)
	if !ok {
		return
	}
	item, ok := peerItemSegment(w, r)
	if !ok {
		return
	}
	key := r.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "which track")
		return
	}

	resp, err := s.openPeerStream(r, p,
		"/api/federation/subtitles/"+item+"/"+url.PathEscape(key))
	if err != nil {
		s.peerUnreachable(w, p, err)
		return
	}
	defer resp.Body.Close()
	copyStreamHeaders(w, resp)
	w.WriteHeader(resp.StatusCode)
	//nolint:errcheck // As above.
	io.Copy(w, resp.Body)
}

/*
 * repointPlaylistAtPeer points a peer's segment URLs at this server's proxy.
 *
 * A plain prefix replacement rather than a parse. The only thing being changed
 * is the part of each URL this server owns, and a playlist parser here would
 * be a second implementation of a format whose remaining lines — durations,
 * discontinuities, the endlist marker — this server has no business having an
 * opinion about.
 *
 * A function, so it can be tested without two servers. That matters: this is
 * the piece whose failure plays the wrong film rather than failing.
 */
func repointPlaylistAtPeer(body, fingerprint string) string {
	return strings.ReplaceAll(body,
		"/api/federation/hls/",
		"/api/peers/"+url.PathEscape(fingerprint)+"/hls/")
}

/*
 * peerItemSegment reads the item from the path, where the HLS and subtitle
 * routes carry it.
 *
 * Validated as a number here even though this server never looks the item up:
 * it is about to become a path segment in a request to somebody else, and a
 * caller-supplied string that goes into a URL is worth confining to what it
 * claims to be, whoever is going to refuse it at the other end.
 */
func peerItemSegment(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := r.PathValue("item")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "which item")
		return "", false
	}
	return raw, true
}

/*
 * forwardedQuery is everything on this request's query except `item`, which
 * the caller has already put in the path or the base.
 *
 * Forwarded whole for the same reason browsing is: `t`, `audio` and the
 * quality ceiling all participate in the far server's delivery decision, and
 * one dropped here would produce an answer about a different stream than the
 * one that was asked for — which is a 409 on a film the parameter was the
 * only reason to touch.
 */
func forwardedQuery(r *http.Request) string {
	q := r.URL.Query()
	q.Del("item")
	if len(q) == 0 {
		return ""
	}
	return "&" + q.Encode()
}

/*
 * copyStreamHeaders forwards the headers that describe the bytes, and only
 * those.
 *
 * Anything else the far server sends is about *its* exchange with us rather
 * than about this one, and passing a Set-Cookie or a caching directive from
 * another household's server into this household's client is a way to be
 * surprised much later.
 */
func copyStreamHeaders(w http.ResponseWriter, resp *http.Response) {
	for _, h := range []string{
		"Content-Type", "Content-Length", "Content-Range",
		"Accept-Ranges", "Last-Modified", "Cache-Control",
	} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
}

/*
 * peerWatching records that the caller is watching a film on a paired server
 * (ADR 0045's second amendment, §10).
 *
 * # Why this route exists at all
 *
 * Locally, presence is recorded as a side effect of the progress write. A peer
 * item writes no progress — ADR 0071 §4 leaves a friend's progress genuinely
 * undecided — so the moment presence rode on never arrived, and watching a
 * friend's film disclosed nothing while the People screen said *idle*.
 *
 * ADR 0045 had already rejected coupling presence to the record, in as many
 * words, and this is the other half of that rejection: what makes somebody
 * visible as watching is a **beat that says so**, not a row being written.
 *
 * # The title is theirs, not ours and not the client's
 *
 * §3's reductions are `presenceTitle`, and it must stay one implementation. We
 * hold neither the item nor its kind, so we ask the server that does and record
 * what it says. A title taken from the client would put a rule this ADR argues
 * for into software the rule cannot reach — and a client that could name its
 * own presence could name an episode, which §3 forbids by name.
 *
 * A peer that will not answer produces silence, not a guess: `Stopped` rather
 * than a fallback to anything we happen to know.
 */
func (s *Server) peerWatching(w http.ResponseWriter, r *http.Request) {
	p, ok := s.peerForBrowse(w, r)
	if !ok {
		return
	}
	item := r.URL.Query().Get("item")
	if item == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "which item")
		return
	}
	user := s.userID(r)

	title, ok := s.peerTitle(r, p, item)
	if !ok {
		/*
		 * Their server did not say. Nothing is recorded and nothing is
		 * guessed — but the *previous* beat is not left standing either,
		 * because presence lingering after the truth changed is the one false
		 * statement about the present this whole ADR exists not to make.
		 */
		s.presence.Stopped(user)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if title == "" {
		// A complete answer, and the common one: music, a photograph, an
		// episode whose series is unknown.
		s.presence.Stopped(user)
	} else {
		s.presence.Watching(user, title)
	}
	w.WriteHeader(http.StatusNoContent)
}

/*
 * peerTitle asks a peer what somebody is watching, and remembers the answer for
 * a little while.
 *
 * The client beats every few seconds, exactly as local playback does, and a
 * federated round trip per beat would spend somebody else's connection to
 * re-learn a film's name that cannot change. The memo is small, per peer and
 * item, and short — a title is not worth holding once the film is over.
 *
 * Not persisted, and deliberately: it is derived from presence, and ADR 0045 §4
 * makes "nothing about this is written down" a property of the whole feature
 * rather than of one table.
 */
func (s *Server) peerTitle(r *http.Request, p store.Peer, item string) (string, bool) {
	key := p.Fingerprint + "\x00" + item

	s.peerTitleMu.Lock()
	if e, ok := s.peerTitles[key]; ok && time.Now().Before(e.until) {
		s.peerTitleMu.Unlock()
		return e.title, true
	}
	s.peerTitleMu.Unlock()

	var body struct {
		// The *reduced* title, not the display one. What may be said about
		// somebody to a third party is ADR 0045 §3's answer, computed by the
		// server that owns the item.
		PresenceTitle string `json:"presence_title"`
	}
	if err := s.callPeer(r.Context(), p, peerItemPath(item), &body); err != nil {
		return "", false
	}

	s.peerTitleMu.Lock()
	if s.peerTitles == nil {
		s.peerTitles = map[string]peerTitleEntry{}
	}
	/*
	 * Bounded by forgetting everything rather than by evicting one thing.
	 *
	 * The map is keyed by peer and item, so it only grows as fast as somebody
	 * starts films, and a household will not reach this. Clearing beats an
	 * eviction policy nobody will ever watch run: the cost of being wrong is
	 * one extra request to a server that is already answering.
	 */
	if len(s.peerTitles) > 512 {
		s.peerTitles = map[string]peerTitleEntry{}
	}
	s.peerTitles[key] = peerTitleEntry{
		title: body.PresenceTitle,
		until: time.Now().Add(peerTitleTTL),
	}
	s.peerTitleMu.Unlock()
	return body.PresenceTitle, true
}

// peerTitleTTL is how long a peer's answer about one item is reused. Long
// enough that a five-second beat does not cross the network, short enough that
// nothing is held after a film ends.
const peerTitleTTL = 5 * time.Minute

type peerTitleEntry struct {
	title string
	until time.Time
}

/*
 * peerItemPath is what this server asks a peer about one of their items — its
 * title, how long it is, and what may be said somebody is watching. A function
 * so it can be asserted without a peer.
 *
 * **It carries the item and nothing else**, which is the whole of ADR 0045 §10's
 * rule about who may name a disclosure. The client's request is not forwarded
 * and its query is not merged — unlike the browse and stream paths, which
 * deliberately pass everything through. A title arriving from a client would
 * move §3's reductions into software the ADR cannot reach, and an episode title
 * is exactly what §3 forbids by name.
 */
func peerItemPath(item string) string {
	return "/api/federation/item/" + url.PathEscape(item)
}

/*
 * peerItem passes through what a peer says about one of their items.
 *
 * The client needs two things it cannot work out for itself: the film's name,
 * for the heading, and **how long it is**, without which a converted stream has
 * a scrubber with no scale — a transcode is a sequence of sessions each
 * starting at zero, so the element's own clock is not the film's.
 *
 * This replaced carrying the title in router state, which was lost the moment
 * somebody arrived at the address directly or reloaded the page.
 */
func (s *Server) peerItem(w http.ResponseWriter, r *http.Request) {
	p, ok := s.peerForBrowse(w, r)
	if !ok {
		return
	}
	item, ok := peerItemSegment(w, r)
	if !ok {
		return
	}
	var body map[string]any
	if err := s.callPeer(r.Context(), p, peerItemPath(item), &body); err != nil {
		s.peerUnreachable(w, p, err)
		return
	}
	/*
	 * `presence_title` is dropped rather than forwarded. It is the far server's
	 * answer to *what may be said about this person to somebody else*, and it
	 * is consumed by this server when it records presence. A client has no use
	 * for it and no business holding it — and a field a client holds is a field
	 * that eventually gets rendered.
	 */
	delete(body, "presence_title")
	writeJSON(w, http.StatusOK, body)
}

/*
 * peerArtwork pulls one of their images through.
 *
 * Without it a peer's library renders as placeholders, which is what it did:
 * `artworkURL` builds `/api/artwork/{hash}`, a **local** route, so a client
 * holding a hash from another server asked *this* one for it and got nothing.
 *
 * Content addressing made that harmless rather than wrong — identical bytes
 * give an identical hash, so a coincidental hit would have been the correct
 * image — but two households download their own artwork, so in practice it was
 * a miss every time.
 *
 * The item travels in the route because the far server needs it: a hash is not
 * an item, and asking for one without saying which item it belongs to is asking
 * to be allowed every image on their disk.
 */
func (s *Server) peerArtwork(w http.ResponseWriter, r *http.Request) {
	p, ok := s.peerForBrowse(w, r)
	if !ok {
		return
	}
	item, ok := peerItemSegment(w, r)
	if !ok {
		return
	}
	hash := r.PathValue("hash")
	if hash == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "which image")
		return
	}

	resp, err := s.openPeerStream(r, p, "/api/federation/artwork/"+item+"/"+
		url.PathEscape(hash)+forwardedQuery(r))
	if err != nil {
		s.peerUnreachable(w, p, err)
		return
	}
	defer resp.Body.Close()
	copyStreamHeaders(w, resp)
	/*
	 * Their caching headers are forwarded and are safe to trust *because* the
	 * content is addressed by its own bytes: an immutable answer cannot become
	 * a stale one. This is the one place a peer's cache directive is kept
	 * rather than dropped, and the reason is a property of the data rather than
	 * trust in the server.
	 */
	w.WriteHeader(resp.StatusCode)
	//nolint:errcheck // A copy that stops early is a viewer who navigated away.
	io.Copy(w, resp.Body)
}
