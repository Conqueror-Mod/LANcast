package api

import (
	"io"
	"net/http"
	"sync/atomic"
)

/*
 * Bytes of media sent (ADR 0106, amended): what the dashboard's "Media sent"
 * chart and lancast_http_media_bytes_total count.
 *
 * Only the routes that serve media: films, episodes and music streamed
 * directly, converted and segmented; downloads; extras; pre-roll clips; live
 * channels; and the same through and for a paired server. Everything else is
 * small (pages, JSON, artwork) and, more to the point, includes the event
 * stream, which ADR 0096 kept unwrapped because it needs its Flush. A wrapper
 * here would be a second place that can break live updates for the sake of a
 * number, so the event stream never reaches one.
 */
var mediaRoutes = []string{
	"/api/stream/{id}",
	"/api/stream/{id}/files",
	"/api/stream/{id}/transcode",
	"/api/stream/{id}/hls/index.m3u8",
	"/api/stream/{id}/hls/{session}/{name}",
	"/api/items/{id}/download",
	"/api/items/{id}/extras/{key}",
	"/api/preroll/{key}",
	"/api/channels/{id}/stream",
	"/api/channels/{id}/live",
	"/api/channels/{id}/hls/index.m3u8",
	"/api/channels/{id}/hls/{session}/{name}",
	"/api/peers/{fingerprint}/stream",
	"/api/peers/{fingerprint}/hls/{item}/index.m3u8",
	"/api/peers/{fingerprint}/hls/{item}/{session}/{name}",
	"/api/federation/stream",
	"/api/federation/transcode",
	"/api/federation/hls/{item}/index.m3u8",
	"/api/federation/hls/{item}/{session}/{name}",
}

func isMediaPath(p string) bool {
	for _, pattern := range mediaRoutes {
		if segmentsMatch(pattern, p) {
			return true
		}
	}
	return false
}

/*
 * mediaMeter counts what a media route writes, for the dashboard. Not
 * served.go's countingWriter, which counts one HLS response to judge a single
 * delivery; this one adds every media byte to the server's running total.
 *
 * It hides nothing the route relies on: Flush is passed through (conversions flush as they go),
 * ReadFrom is passed through (so a file served by http.ServeContent still
 * takes the fast copy), and Unwrap gives http.ResponseController the real
 * writer for deadlines.
 */
type mediaMeter struct {
	http.ResponseWriter
	n *atomic.Int64
}

func (c mediaMeter) Write(b []byte) (int, error) {
	n, err := c.ResponseWriter.Write(b)
	c.n.Add(int64(n))
	return n, err
}

func (c mediaMeter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c mediaMeter) ReadFrom(r io.Reader) (int64, error) {
	if rf, ok := c.ResponseWriter.(io.ReaderFrom); ok {
		n, err := rf.ReadFrom(r)
		c.n.Add(n)
		return n, err
	}
	n, err := io.Copy(writerOnly{c.ResponseWriter}, r)
	c.n.Add(n)
	return n, err
}

func (c mediaMeter) Unwrap() http.ResponseWriter { return c.ResponseWriter }

// writerOnly hides ReadFrom, so io.Copy above cannot recurse into it.
type writerOnly struct{ io.Writer }
