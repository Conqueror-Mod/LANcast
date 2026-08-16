package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"lancast/internal/livetv"
	"lancast/internal/store"
)

/*
 * Live TV.
 *
 * A channel source is an M3U published by somebody else — an IPTV provider, a
 * tvheadend instance, a local transcoder. Importing one is **admin-gated**, and
 * for a stronger reason than most admin gates in this system: the URL is
 * fetched by the server, so anybody who can add a source can make the server
 * issue an HTTP request to an address of their choosing. That is the shape of a
 * server-side request forgery, and it is not a power a member should have.
 *
 * Watching is not gated. A channel list is for the household.
 */

// maxPlaylistBytes bounds what will be read from a source. A provider list of
// six hundred channels is perhaps 200KB; anything past this is not a playlist,
// and reading it whole to find that out is how a fetch becomes a memory
// problem.
const maxPlaylistBytes = 8 << 20

// fetchTimeout keeps a slow or hanging provider from holding the request open.
const fetchTimeout = 30 * time.Second

func (s *Server) listChannelSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.st.ListChannelSources(r.Context())
	if err != nil {
		s.writeInternal(w, err, "list channel sources")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": sources})
}

func (s *Server) createChannelSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	name := strings.TrimSpace(req.Name)
	raw := strings.TrimSpace(req.URL)
	if name == "" || raw == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "a name and a playlist URL are required")
		return
	}
	if err := checkSourceURL(raw, r.Host); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	src, err := s.st.CreateChannelSource(r.Context(), name, raw)
	if err != nil {
		s.writeInternal(w, err, "create channel source")
		return
	}

	// Imported immediately, because a source with no channels in it is
	// indistinguishable from a broken one, and the moment somebody added it is
	// the moment they are watching to see whether it worked.
	n, err := s.importSource(r.Context(), src)
	if err != nil {
		// The source is kept rather than rolled back: the URL may be right and
		// the provider down, and deleting it would make the operator retype it.
		// The error says which of those it was.
		writeJSON(w, http.StatusCreated, map[string]any{
			"source":       src,
			"import_error": err.Error(),
		})
		return
	}
	src.ChannelCount = n
	s.audit(r, "channels.source_add", "channel_source", fmt.Sprint(src.ID),
		fmt.Sprintf("added channel source %q with %d channels", name, n), nil)
	writeJSON(w, http.StatusCreated, map[string]any{"source": src, "channels": n})
}

func (s *Server) refreshChannelSource(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid source id")
		return
	}
	src, err := s.st.GetChannelSource(r.Context(), id)
	if s.notFoundOr(w, err, "get channel source", "no such channel source") {
		return
	}

	n, err := s.importSource(r.Context(), src)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream", err.Error())
		return
	}
	s.audit(r, "channels.source_refresh", "channel_source", fmt.Sprint(id),
		fmt.Sprintf("refreshed %q — %d channels", src.Name, n), nil)
	writeJSON(w, http.StatusOK, map[string]any{"channels": n})
}

func (s *Server) deleteChannelSource(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid source id")
		return
	}
	src, err := s.st.GetChannelSource(r.Context(), id)
	if s.notFoundOr(w, err, "get channel source", "no such channel source") {
		return
	}
	if err := s.st.DeleteChannelSource(r.Context(), id); err != nil {
		s.writeInternal(w, err, "delete channel source")
		return
	}
	s.audit(r, "channels.source_remove", "channel_source", fmt.Sprint(id),
		fmt.Sprintf("removed channel source %q", src.Name), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	var sourceID int64
	if v := r.URL.Query().Get("source_id"); v != "" {
		if _, err := fmt.Sscan(v, &sourceID); err != nil || sourceID < 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid source_id")
			return
		}
	}
	chans, err := s.st.ListChannels(r.Context(), sourceID)
	if err != nil {
		s.writeInternal(w, err, "list channels")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": chans})
}

/*
 * importSource fetches a playlist and replaces that source's channels.
 *
 * Shared by add and refresh so the two cannot drift — a refresh that parsed
 * differently from an import would produce a channel list that changed shape
 * depending on how it got there.
 */
func (s *Server) importSource(ctx context.Context, src *store.ChannelSource) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", src.URL, nil)
	if err != nil {
		return 0, fmt.Errorf("could not request that URL: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("could not reach the playlist: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Named rather than wrapped: "403" from a provider means the
		// subscription lapsed, and that is the sentence worth showing.
		return 0, fmt.Errorf("the playlist answered %s", resp.Status)
	}

	parsed, err := livetv.Parse(io.LimitReader(resp.Body, maxPlaylistBytes))
	if err != nil {
		return 0, fmt.Errorf("that URL did not return an M3U channel list")
	}

	chans := make([]store.Channel, 0, len(parsed))
	for _, c := range parsed {
		ch := store.Channel{Name: c.Name, URL: c.URL}
		if c.LogoURL != "" {
			logo := c.LogoURL
			ch.LogoURL = &logo
		}
		if c.Group != "" {
			g := c.Group
			ch.Group = &g
		}
		chans = append(chans, ch)
	}
	if err := s.st.ReplaceChannels(ctx, src.ID, chans); err != nil {
		return 0, err
	}
	return len(chans), nil
}

/*
 * checkSourceURL refuses what the server should not be asked to fetch.
 *
 * Adding a source makes the server issue a request to an address the caller
 * chose, which is server-side request forgery in miniature. The route is
 * admin-gated, so this is a second line rather than the only one — but "admin"
 * on a household server is not the same standard as "trusted to make this
 * process call its own API".
 *
 * The rule is deliberately narrow: **this server's own address is refused, and
 * nothing else on loopback is.** A blanket ban on localhost was the first
 * attempt and it was wrong — a tvheadend or a local transcoder on the same
 * machine is one of the most ordinary sources this feature has, and refusing it
 * would make Live TV useless on the exact setup it suits best. What actually
 * needs protecting is one origin: ours.
 */
func checkSourceURL(raw, ownHost string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("that is not a valid URL")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("a channel list must be an http or https URL")
	}
	if u.Host == "" {
		return fmt.Errorf("that URL has no host")
	}

	// Compared on host *and* port, because the thing being protected is this
	// API rather than the machine. 127.0.0.1:9981 is a tuner; 127.0.0.1:8080 is
	// us.
	if sameOrigin(u.Host, ownHost) {
		return fmt.Errorf("a channel list cannot point back at this server")
	}
	return nil
}

// sameOrigin compares two host:port pairs, treating the loopback spellings as
// equivalent — "localhost", "127.0.0.1" and "[::1]" are one machine, and a
// check that only knew one of them would be trivially stepped around.
func sameOrigin(a, b string) bool {
	ah, ap := splitHostPort(a)
	bh, bp := splitHostPort(b)
	if ap != bp {
		return false
	}
	return isLoopback(ah) && isLoopback(bh) || ah == bh
}

func splitHostPort(s string) (host, port string) {
	host = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndex(host, ":"); i > 0 && !strings.HasSuffix(host, "]") {
		// Not a bare IPv6 literal: everything after the last colon is a port.
		if !strings.Contains(host[i+1:], "]") {
			host, port = host[:i], host[i+1:]
		}
	}
	host = strings.Trim(host, "[]")
	return host, port
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "::1" || strings.HasPrefix(host, "127.")
}
