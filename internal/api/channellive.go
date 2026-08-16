package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

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
	decision := transcode.LiveDecision(s.probeChannel(r, ch.URL), clientProfile(r), s.log)

	stream, err := s.trans.Live(r.Context(), id, transcode.LiveOptions{
		URL:      ch.URL,
		Decision: decision,
	})
	if err != nil {
		switch {
		case errors.Is(err, transcode.ErrTooManySessions):
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
	defer stream.Close()

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
	buf := make([]byte, 64<<10)
	for {
		n, err := stream.Read(buf)
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
		if err != nil {
			if err != io.EOF {
				s.log.Debug("live stream ended", "channel", id, "error", err)
			}
			return
		}
	}
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
