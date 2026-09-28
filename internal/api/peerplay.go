package api

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
