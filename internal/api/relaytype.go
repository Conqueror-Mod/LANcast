package api

import (
	"mime"
	"net/http"
	"strings"
)

/*
 * What a relayed response is called, decided here and not by whoever sent it.
 *
 * Several routes pass along bytes that came from somewhere else: a paired
 * server's artwork, segments, subtitles and films, and a live channel's
 * provider. Those bytes are served from **this** household's origin, signed in
 * as whoever asked. If the far end could choose the Content-Type it could
 * answer a poster with `text/html` (or an SVG) carrying script, and anyone who
 * opened that URL as a page would run it here, where `fetch` is same-origin,
 * the `SameSite=Strict` cookie rides along and the Origin check passes (R02-1).
 * A pairing trusts the far server about its own people; it was never meant to
 * hand it script on this one.
 *
 * # An allow-list on the declared type, not a sniff of the bytes
 *
 * Each route already knows what it can legitimately carry, so the far end's
 * header is parsed, its parameters dropped, and kept only if it is on that
 * route's list; anything else becomes `application/octet-stream`. Sniffing the
 * first bytes was the alternative and is worse here: it means buffering the
 * head of every segment and film on the hot path, it is a second guesser that
 * can disagree with the browser's own, and it buys nothing once the label is
 * constrained — a body that is really HTML, labelled `image/jpeg` and served
 * with `nosniff`, is rendered as a broken image and never as a page.
 *
 * # Two headers on every relay, whatever the type
 *
 * `X-Content-Type-Options: nosniff` stops the browser second-guessing the label
 * this file chose. `Content-Security-Policy: sandbox; default-src 'none'` is the
 * belt to that: if a relayed response is ever opened as a document anyway, it is
 * an opaque origin with no script and nothing it may load. Neither changes how
 * an <img>, <video> or <track> uses the bytes.
 *
 * These are per relay, deliberately. A server-wide nosniff and a CSP for the
 * app itself would close the class, and belong to the hardening slice that can
 * test them against the client.
 */

// relayAllow decides whether a far end's media type (lower-case, no
// parameters) may be served as-is on one route.
type relayAllow func(mediaType string) bool

func oneOf(types ...string) relayAllow {
	return func(mt string) bool {
		for _, t := range types {
			if mt == t {
				return true
			}
		}
		return false
	}
}

var (
	// Raster images only. SVG is a document that can carry script, which is
	// the whole reason this file exists.
	relayArtwork = oneOf("image/jpeg", "image/png", "image/webp", "image/gif")
	// What the far server's own HLS segments are: fMP4 init and media
	// segments, or a transport stream.
	relaySegment = oneOf("video/mp4", "video/iso.segment", "video/mp2t")
	relayVTT     = oneOf("text/vtt")
	// The flat answers that are JSON on the far server: how it would deliver
	// an item, and what subtitle tracks it has. A browser shows JSON as text
	// and never as a page.
	relayJSON = oneOf("application/json")
)

// relayMedia is a film or a track: anything a media element plays.
func relayMedia(mt string) bool {
	return strings.HasPrefix(mt, "video/") || strings.HasPrefix(mt, "audio/")
}

// relayContentType is the Content-Type this server serves for a far end's
// declared one on a route that allows `allow`.
func relayContentType(declared string, allow relayAllow) string {
	mt, _, err := mime.ParseMediaType(declared)
	if err != nil || !allow(mt) {
		return "application/octet-stream"
	}
	// Text types carry their charset, chosen here; the far end's parameters
	// are never forwarded, so nothing it adds can change how this is read.
	if mt == "text/vtt" {
		return "text/vtt; charset=utf-8"
	}
	return mt
}

// hardenRelay sets the two headers every relayed response carries.
func hardenRelay(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
}
