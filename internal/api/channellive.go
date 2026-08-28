package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	"lancast/internal/livebuf"
	"lancast/internal/probe"
	"lancast/internal/transcode"
)

/*
 * A channel, delivered as something a browser can actually play.
 *
 * `/stream` relays a channel untouched, which works in Safari and nowhere else:
 * most IPTV is HLS carrying MPEG-TS, and Chromium decodes neither —
 * `canPlayType("video/mp2t")` is an empty string. ADR 0013 refuses to vendor
 * hls.js, so the remaining way to make a channel playable is the one this
 * project already owns: put it through ffmpeg and emit fragmented MP4, exactly
 * as the file path does for anything a client cannot take directly.
 *
 * Usually **not a transcode**. Nearly every channel is H.264 with AAC, which
 * fMP4 accepts as-is, so ffmpeg rewrites the container and copies both streams
 * — a few percent of a core rather than a whole one. That is what makes this
 * affordable per viewer, and it is why the decision comes from probe.Decide
 * rather than from a rule invented here.
 */

// probeTimeout bounds how long a channel is inspected before giving up and
// copying blind. A channel that has not identified itself in this long is one
// the viewer is already waiting on, and "copy and let the browser complain" is
// a better answer than a spinner.
const probeTimeout = 6 * time.Second

func (s *Server) channelLive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid channel id")
		return
	}
	ch, err := s.st.GetChannel(r.Context(), id)
	if s.notFoundOr(w, err, "get channel", "no such channel") {
		return
	}

	if !s.trans.Available() {
		// Named rather than a generic 500: the fix is installing ffmpeg, and a
		// message that does not say so sends somebody to the wrong place.
		writeError(w, http.StatusServiceUnavailable, "no_ffmpeg",
			"live channels need ffmpeg, which this server cannot find")
		return
	}

	/*
	 * Probe first, briefly, and tolerate failure.
	 *
	 * Knowing the codecs turns a guess into a decision — an AC-3 channel needs
	 * its audio re-encoded and copying it produces silence in a browser, which
	 * is the worst failure available because the picture works. But probing a
	 * live source costs a connection and a few seconds, so it is bounded and a
	 * failure falls through to copying rather than refusing.
	 */
	probed := s.probeChannel(r, ch.URL)
	decision := transcode.LiveDecision(probed, clientProfile(r), s.log)

	stream, err := s.trans.Live(r.Context(), id, transcode.LiveOptions{
		URL:      ch.URL,
		Decision: decision,
		// Where ffmpeg starts reading an HLS playlist. The probe is the only
		// thing that knows the container, and an unprobed channel deliberately
		// keeps the old behaviour rather than risking a refused input.
		HLS: transcode.IsHLS(probed),
	})
	if err != nil {
		switch {
		case errors.Is(err, transcode.ErrTooManySessions):
			// The running count travels with it: the same line read two ways is
			// "the ceiling is doing its job" or "sessions are leaking", and it
			// is unreadable without the number.
			s.log.Warn("refused a channel: at the session ceiling",
				"channel", id, "running", len(s.trans.Sessions()), "max", s.trans.MaxSessions)
			writeError(w, http.StatusServiceUnavailable, "busy",
				"too many streams are already running on this server")
		default:
			s.writeInternal(w, err, "start live transcode")
		}
		return
	}
	/*
	 * Closed when the handler returns, which is what stops ffmpeg.
	 *
	 * This is the most important line in the file. A live source never ends, so
	 * nothing else will ever stop the process: a leaked session does not sit
	 * idle, it pulls a stream at full rate for ever, for somebody who closed
	 * the tab an hour ago. The request context and this Close are the two
	 * things standing between a channel list and a server that slowly fills
	 * with ffmpeg.
	 */
	/*
	 * Smooth the provider's rhythm before it reaches the browser.
	 *
	 * Relayed untouched, a channel arrives in tight bursts separated by
	 * silences — measured at this endpoint, 98% of a 42-second window was
	 * silence and the longest hole was 9,850ms. Every cushion in the client
	 * exists to survive those holes, and each of its constants is a guess about
	 * a stranger's segment interval. `livebuf` reads ahead and hands the stream
	 * out at its own rate, so the hole is absorbed on the side that can see it.
	 *
	 * The cost is latency, deliberately: the lead is added to how far behind
	 * live every viewer is. Nobody can tell whether they are eight or fourteen
	 * seconds behind a broadcast; everybody can tell when it stops.
	 */
	smoothed := livebuf.New(stream, livebuf.Options{})
	defer smoothed.Close()

	/*
	 * Wait for the first byte before committing to 200.
	 *
	 * A channel list from a provider carries dead entries, and a source that
	 * 404s is ordinary rather than exceptional. Writing the header first meant
	 * the response was already a successful video stream by the time ffmpeg
	 * failed — so an empty body reached the browser, which reported
	 * `DEMUXER_ERROR_COULD_NOT_OPEN`, and a dead channel read as a broken
	 * application. The server knew the real answer the whole time.
	 *
	 * Once one byte exists there is a stream, and every later failure is an
	 * interruption of something that was working — which is a different thing,
	 * correctly reported by the connection ending rather than by a status code
	 * that can no longer be sent.
	 */
	buf := make([]byte, 64<<10)
	n, rerr := firstBytes(smoothed, buf)
	if n == 0 {
		reason := transcode.FailureReason(stderrOf(stream))
		// The upstream URL is in that stderr and never leaves this process:
		// channel URLs are routinely credentialed, and publishing one hands out
		// the subscription. Only the classification goes out.
		s.log.Warn("channel produced no video", "channel", id, "reason", reason)
		writeError(w, http.StatusBadGateway, "channel_unavailable", reason)
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	// No length, and explicitly not cacheable: this is a stream with no end,
	// and a cache holding "the channel" would serve one viewer's minute to
	// everybody who asked afterwards.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Flushed as it arrives. Without this a reverse proxy or Go's own buffering
	// delivers the stream in chunks, which on live content is latency the
	// viewer feels directly.
	flusher, _ := w.(http.Flusher)
	for {
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				// The client has gone. Ordinary, and the reason the deferred
				// Close above exists.
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			if rerr != io.EOF {
				s.log.Debug("live stream ended", "channel", id, "error", rerr)
			}
			return
		}
		n, rerr = smoothed.Read(buf)
	}
}

// firstBytes reads until the stream produces something or gives up. A read
// returning (0, nil) is legal and means "nothing yet", which on a live source
// starting up is normal rather than a failure.
func firstBytes(r io.Reader, buf []byte) (int, error) {
	for {
		n, err := r.Read(buf)
		if n > 0 || err != nil {
			return n, err
		}
	}
}

// stderrOf asks a stream what ffmpeg complained about, when it can answer.
func stderrOf(stream io.ReadCloser) string {
	if s, ok := stream.(interface{ Stderr() string }); ok {
		return s.Stderr()
	}
	return ""
}

/*
 * probeChannel inspects a live source, briefly.
 *
 * Returns nil rather than an error on failure: every caller's response to "the
 * probe did not work" is the same — copy both streams and let the browser
 * complain if it cannot play them — so an error would be a value nobody
 * branches on.
 */
func (s *Server) probeChannel(r *http.Request, url string) *probe.Result {
	if s.prober == nil || !s.prober.Available() {
		return nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), probeTimeout)
	defer cancel()

	res, err := s.prober.Probe(ctx, url)
	if err != nil {
		s.log.Debug("channel probe failed; copying blind", "error", err)
		return nil
	}
	return res
}

/*
 * channelLiveHLS serves a channel as an HLS playlist.
 *
 * It exists because the progressive live path has no control surface, which is
 * the whole subject of the ADR 0013 amendment: a bare element cannot say how
 * much media it holds, cannot tell starved from stalled, and cannot be seeked
 * on a response that has no ranges. A playlist answers all three, for any
 * client able to consume one.
 *
 * There is no client for it in this build yet — hls.js is deliberately not
 * vendored (ADR 0013) — and that is the same position the file HLS endpoints
 * have been in since M3. The output is the thing being built here; the decision
 * about what consumes it is taken separately and is not taken by shipping this.
 */
func (s *Server) channelLiveHLS(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid channel id")
		return
	}
	ch, err := s.st.GetChannel(r.Context(), id)
	if s.notFoundOr(w, err, "get channel", "no such channel") {
		return
	}

	if !s.trans.Available() {
		writeError(w, http.StatusServiceUnavailable, "no_ffmpeg",
			"live channels need ffmpeg, which this server cannot find")
		return
	}

	probed := s.probeChannel(r, ch.URL)
	decision := transcode.LiveDecision(probed, clientProfile(r), s.log)

	/*
	 * context.WithoutCancel, and this is the difference that matters against
	 * the progressive path.
	 *
	 * There, the request context *is* the lifetime: a closed tab must kill
	 * ffmpeg, because nothing else ever will. Here the request is one poll of a
	 * playlist among many, and tying the encode to it would kill the channel
	 * between the playlist and its first segment. The session is instead ended
	 * by IdleTimeout, which is what already reaps file HLS sessions and what
	 * makes a shared session possible at all.
	 */
	sess, err := s.trans.LiveHLS(context.WithoutCancel(r.Context()), id, transcode.LiveOptions{
		URL:      ch.URL,
		Decision: decision,
		HLS:      transcode.IsHLS(probed),
	})
	if err != nil {
		switch {
		case errors.Is(err, transcode.ErrTooManySessions):
			// The running count travels with it: the same line read two ways is
			// "the ceiling is doing its job" or "sessions are leaking", and it
			// is unreadable without the number.
			s.log.Warn("refused a channel: at the session ceiling",
				"channel", id, "running", len(s.trans.Sessions()), "max", s.trans.MaxSessions)
			writeError(w, http.StatusServiceUnavailable, "busy",
				"too many streams are already running on this server")
		default:
			s.writeInternal(w, err, "start live hls")
		}
		return
	}

	path, err := s.trans.WaitForFile(r.Context(), sess, "index.m3u8", 30*time.Second)
	if err != nil {
		s.log.Warn("live hls playlist unavailable", "channel", id, "error", err)
		writeError(w, http.StatusServiceUnavailable, "unavailable", "could not start the channel")
		return
	}

	body, err := os.ReadFile(path)
	if err != nil {
		s.writeInternal(w, err, "read playlist")
		return
	}

	// Segments are served by the existing session-scoped endpoint, which
	// already validates the name and is not channel-specific.
	rewritten := rewritePlaylist(string(body), "/api/channels/"+itoa64(id)+"/hls/"+sess.ID+"/")

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(rewritten))
}
