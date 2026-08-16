package api

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

/*
 * Playing a channel.
 *
 * The provider's URL never reaches a browser. Channel lists are routinely
 * credentialed — a token in the path, or a username and password in the query —
 * so handing the URL to every device on the LAN would be handing out the
 * subscription. The server fetches, the client plays what the server relays.
 *
 * **This is a proxy that cannot be pointed anywhere.** It takes a channel id,
 * not a URL. The only address it will ever fetch is the one stored against that
 * channel, and for HLS the only *other* addresses are paths resolved against
 * that channel's own base. There is no parameter a caller can supply that
 * changes the host, which is the property that keeps this from being an open
 * relay sitting inside somebody's network.
 */

// hlsContentTypes are the types that mean "this body is a playlist of further
// URLs", and therefore the bodies that need rewriting rather than relaying.
var hlsContentTypes = []string{
	"application/vnd.apple.mpegurl",
	"application/x-mpegurl",
	"audio/mpegurl",
	"audio/x-mpegurl",
}

func (s *Server) channelStream(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid channel id")
		return
	}
	ch, err := s.st.GetChannel(r.Context(), id)
	if s.notFoundOr(w, err, "get channel", "no such channel") {
		return
	}

	base, err := url.Parse(ch.URL)
	if err != nil {
		s.writeInternal(w, err, "parse channel url")
		return
	}

	/*
	 * A relative path from a rewritten playlist, resolved against the channel's
	 * own URL.
	 *
	 * Resolution is what makes this safe: `ResolveReference` on a *relative*
	 * reference cannot change the host, so a caller sending `../../etc` or
	 * `//evil.example/x` gets something under the provider's own origin or
	 * nothing at all. An absolute reference is refused outright rather than
	 * resolved, because accepting one is exactly the open-proxy hole this is
	 * built to avoid.
	 */
	target := base
	if rel := r.URL.Query().Get("path"); rel != "" {
		ref, err := url.Parse(rel)
		if err != nil || ref.IsAbs() || ref.Host != "" {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid segment path")
			return
		}
		target = base.ResolveReference(ref)
		if target.Host != base.Host || target.Scheme != base.Scheme {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid segment path")
			return
		}
	}

	req, err := http.NewRequestWithContext(r.Context(), "GET", target.String(), nil)
	if err != nil {
		s.writeInternal(w, err, "build channel request")
		return
	}
	// Range is forwarded so seeking within a segment works; nothing else is,
	// because a provider has no business seeing this household's headers.
	if rng := r.Header.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream", "could not reach that channel")
		return
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if isHLS(ct, target.Path) {
		s.relayPlaylist(w, resp, id, target)
		return
	}

	// Everything else is relayed as-is: a transport stream, an fMP4 segment, a
	// progressive body. No transcode — a live stream re-encoded per viewer is a
	// CPU bill this project has not agreed to, and most providers already serve
	// something a browser can play.
	for _, h := range []string{"Content-Type", "Content-Length", "Accept-Ranges", "Content-Range"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

/*
 * relayPlaylist rewrites an HLS playlist so its URIs come back through here.
 *
 * Without this the playlist works and the credentials leak: the browser would
 * fetch each segment directly from the provider, using the signed URL the
 * server was trying not to publish. Rewriting keeps every request on this
 * server, which is also what makes the channel playable from a device that
 * cannot reach the provider at all.
 *
 * Bounded, because a playlist is small and a body claiming otherwise is not a
 * playlist.
 */
func (s *Server) relayPlaylist(w http.ResponseWriter, resp *http.Response, channelID int64, target *url.URL) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream", "could not read that channel's playlist")
		return
	}

	var out strings.Builder
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimRight(line, "\r")
		switch {
		case trimmed == "":
			out.WriteString("\n")
		case strings.HasPrefix(trimmed, "#"):
			// Directives carrying a URI attribute (EXT-X-KEY, EXT-X-MAP) are
			// left alone deliberately rather than half-rewritten: a key URI
			// pointing at the provider still works for a client that can reach
			// it, and a wrong rewrite would break playback silently. Stated as
			// a known limit rather than guessed at.
			out.WriteString(trimmed)
			out.WriteString("\n")
		default:
			out.WriteString(s.rewriteURI(trimmed, channelID, target))
			out.WriteString("\n")
		}
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, out.String())
}

// rewriteURI turns one playlist entry into a path back through this server,
// expressed relative to the channel's own base so the proxy never needs to be
// told a host.
func (s *Server) rewriteURI(line string, channelID int64, target *url.URL) string {
	ref, err := url.Parse(line)
	if err != nil {
		return line
	}
	abs := target.ResolveReference(ref)
	// A segment on another host cannot be relayed without accepting an
	// arbitrary destination, so it is left pointing where it points. Rare, and
	// better than becoming an open proxy for it.
	if abs.Host != target.Host {
		return line
	}
	rel := abs.Path
	if abs.RawQuery != "" {
		rel += "?" + abs.RawQuery
	}
	return fmt.Sprintf("/api/channels/%d/stream?path=%s", channelID, url.QueryEscape(rel))
}

func isHLS(contentType, path string) bool {
	lower := strings.ToLower(contentType)
	for _, t := range hlsContentTypes {
		if strings.Contains(lower, t) {
			return true
		}
	}
	// Providers frequently serve a playlist as text/plain or with no type at
	// all, so the extension is the fallback rather than the primary signal.
	return strings.HasSuffix(strings.ToLower(path), ".m3u8")
}
