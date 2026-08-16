package api

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"lancast/internal/livetv"
	"lancast/internal/store"
)

/*
 * The guide.
 *
 * Reading it is not admin-gated, for the same reason watching a channel is not:
 * what is on television tonight is not a secret from the household. *Importing*
 * it is gated exactly as adding a channel list is, and for the identical
 * reason — the URL is fetched by this server, and that is server-side request
 * forgery in miniature.
 */

/*
 * maxGuideBytes bounds an XMLTV download.
 *
 * Larger than the playlist bound by two orders of magnitude, and it has to be:
 * a fortnight of listings for six hundred channels is a genuinely large XML
 * document, and 8MB — fine for an M3U — would truncate a real guide into a
 * schedule that stops on Tuesday.
 */
const maxGuideBytes = 256 << 20

// epgFetchTimeout is longer than the playlist's for the same reason: this is
// two hundred megabytes of XML over somebody's broadband, not 200KB.
const epgFetchTimeout = 5 * time.Minute

// guideWindowHours caps how much of one channel's schedule a single request can
// ask for. Two weeks is more than any guide publishes; the cap exists so a
// client cannot ask for a decade and be handed the whole table.
const guideWindowHours = 24 * 14

/*
 * importEPG fetches a source's XMLTV guide and replaces its listings.
 *
 * **Must run after the channel list has been imported, never before.** Replacing
 * channels deletes their rows, and `epg_program.channel_id` cascades — a guide
 * imported first is deleted moments later by the channel import, leaving an
 * empty schedule and no error anywhere to explain it. `store.ReplaceChannels`
 * documents the same constraint from the other side.
 *
 * Returns the number of listings stored, which is not the number in the file:
 * a guide covers channels this source does not carry, and those are dropped
 * here rather than stored against nothing.
 */
func (s *Server) importEPG(ctx context.Context, src *store.ChannelSource) (int, error) {
	if src.EPGURL == nil || *src.EPGURL == "" {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, epgFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", *src.EPGURL, nil)
	if err != nil {
		return 0, fmt.Errorf("could not request that guide URL: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("could not reach the guide: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("the guide answered %s", resp.Status)
	}

	body, err := decompressGuide(io.LimitReader(resp.Body, maxGuideBytes))
	if err != nil {
		return 0, err
	}
	defer body.Close()

	progs, err := livetv.ParseXMLTV(body)
	if err != nil {
		return 0, fmt.Errorf("that URL did not return an XMLTV guide")
	}

	byTvgID, err := s.st.ChannelTvgIDs(ctx, src.ID)
	if err != nil {
		return 0, err
	}
	if len(byTvgID) == 0 {
		// Worth its own sentence rather than reporting "0 listings": the guide
		// is fine and the *playlist* is the one missing information, which is
		// not a conclusion anybody would reach from a zero.
		return 0, fmt.Errorf("no channel in this list carries a tvg-id, so no listings can be attached")
	}

	rows := make([]store.Program, 0, len(progs))
	for _, p := range progs {
		id, ok := byTvgID[lowerTrim(p.ChannelID)]
		if !ok {
			continue
		}
		row := store.Program{
			ChannelID: id,
			StartAt:   p.Start.Unix(),
			StopAt:    p.Stop.Unix(),
			Title:     p.Title,
		}
		row.Description = optString(p.Desc)
		row.Category = optString(p.Category)
		row.IconURL = optString(p.IconURL)
		row.Season = optInt(p.Season)
		row.Episode = optInt(p.Episode)
		rows = append(rows, row)
	}

	return s.st.ReplaceProgramsForSource(ctx, src.ID, rows)
}

/*
 * RefreshGuides re-imports every configured guide and drops what has expired.
 *
 * A guide is the one thing in this system that goes wrong by *doing nothing*.
 * A library that is not rescanned still lists the films it listed yesterday; a
 * guide that is not refreshed says a programme from last Tuesday is on now,
 * which is worse than saying nothing, because it looks like an answer.
 *
 * Channel lists are deliberately **not** refreshed here. That is an operator's
 * decision with visible consequences — channels appearing, disappearing and
 * reordering under somebody who is watching one — and it stays a button. The
 * guide has no such consequence: it is the same channels, correctly labelled.
 *
 * Errors are logged and not returned. One provider being down at 4am must not
 * stop the others being refreshed, and there is nobody to report to.
 */
func (s *Server) RefreshGuides(ctx context.Context) {
	// Yesterday, not now: a programme that finished an hour ago is still what
	// somebody scrolling back wants to see, and "what was on" is a reasonable
	// question for the length of a day.
	if n, err := s.st.PruneExpiredPrograms(ctx, time.Now().Add(-24*time.Hour)); err != nil {
		s.log.Error("guide prune", "error", err)
	} else if n > 0 {
		s.log.Debug("guide prune", "removed", n)
	}

	sources, err := s.st.ListChannelSources(ctx)
	if err != nil {
		s.log.Error("guide refresh: list sources", "error", err)
		return
	}
	for i := range sources {
		src := sources[i]
		if src.EPGURL == nil || *src.EPGURL == "" {
			continue
		}
		n, err := s.importEPG(ctx, &src)
		if err != nil {
			s.log.Warn("guide refresh", "source", src.Name, "error", err)
			continue
		}
		s.log.Info("guide refreshed", "source", src.Name, "programs", n)
	}
}

/*
 * listGuide answers what is on now and next, across every channel.
 *
 * Keyed by channel id so a client can render the Live TV grid without joining
 * anything: it already holds the channels. Returning a flat list with the
 * channel repeated on every entry was the alternative, and it makes the client
 * build this map itself on every poll.
 */
func (s *Server) listGuide(w http.ResponseWriter, r *http.Request) {
	at := time.Now()
	if v := r.URL.Query().Get("at"); v != "" {
		secs, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "at must be a unix timestamp")
			return
		}
		at = time.Unix(secs, 0)
	}

	byChannel, err := s.st.NowNext(r.Context(), at)
	if err != nil {
		s.writeInternal(w, err, "guide")
		return
	}

	// Keys stringified because JSON object keys are strings, and an int64 key
	// marshalled by Go would be one anyway — being explicit keeps the client's
	// type honest about what it is indexing by.
	out := make(map[string]map[string]any, len(byChannel))
	for id, ps := range byChannel {
		entry := map[string]any{"now": ps[0]}
		if len(ps) > 1 {
			entry["next"] = ps[1]
		}
		out[strconv.FormatInt(id, 10)] = entry
	}
	writeJSON(w, http.StatusOK, map[string]any{"at": at.Unix(), "channels": out})
}

// channelGuide answers one channel's schedule. `from` defaults to now and
// `hours` to twelve — an evening's television, which is what a guide opened at
// 7pm is being asked for.
func (s *Server) channelGuide(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid channel id")
		return
	}
	if _, err := s.st.GetChannel(r.Context(), id); s.notFoundOr(w, err, "get channel", "no such channel") {
		return
	}

	from := time.Now()
	if v := r.URL.Query().Get("from"); v != "" {
		secs, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "from must be a unix timestamp")
			return
		}
		from = time.Unix(secs, 0)
	}
	hours := 12
	if v := r.URL.Query().Get("hours"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > guideWindowHours {
			writeError(w, http.StatusBadRequest, "bad_request",
				fmt.Sprintf("hours must be between 1 and %d", guideWindowHours))
			return
		}
		hours = n
	}

	progs, err := s.st.ChannelSchedule(r.Context(), id, from, from.Add(time.Duration(hours)*time.Hour))
	if err != nil {
		s.writeInternal(w, err, "channel guide")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"programs": progs})
}

/*
 * decompressGuide unwraps a gzipped guide.
 *
 * Not an optional nicety: the majority of published XMLTV is `.xml.gz`, because
 * the format compresses about tenfold and nobody wants to serve 200MB. Go's
 * HTTP client transparently handles `Content-Encoding: gzip`, but this is a
 * gzipped *file* served as a body — the compression is the payload, not the
 * transport — and the client does not touch it.
 *
 * Sniffed on the gzip magic number rather than trusting the URL suffix or the
 * content type. Providers serve `.gz` files as `text/plain` and plain XML from
 * a URL ending `.gz` about equally often, so the bytes are the only reliable
 * witness — neither label is checked, because a label that is wrong half the
 * time is worse than no label.
 */
func decompressGuide(r io.Reader) (io.ReadCloser, error) {
	br := bufio.NewReader(r)
	magic, err := br.Peek(2)
	if err != nil && len(magic) < 2 {
		// Too short to be either. Hand it on; the parser reports it as not
		// being XMLTV, which is the accurate complaint.
		return io.NopCloser(br), nil
	}
	if magic[0] != 0x1f || magic[1] != 0x8b {
		return io.NopCloser(br), nil
	}
	zr, err := gzip.NewReader(br)
	if err != nil {
		return nil, fmt.Errorf("that guide is gzipped but could not be decompressed: %w", err)
	}
	return zr, nil
}

func lowerTrim(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func optInt(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}
