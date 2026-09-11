package api

import (
	"fmt"
	"net/http"
)

/*
 * How many bytes of media a response actually handed over.
 *
 * `http.ServeContent` writes the body itself — it handles ranges, conditional
 * requests and the 304 that answers them — so the only way to know what left
 * the building is to count it on the way past.
 *
 * It exists because a session's byte count was the one thing that could tell a
 * stream somebody is watching from a stream nobody ever attached to, and on the
 * HLS path it was always zero. Segments were served by ServeContent and nothing
 * told the session, so a session delivering a film segment by segment and a
 * session that was abandoned half a second after it started looked identical
 * from the manager, from the reaper and from the log.
 */
type countingWriter struct {
	http.ResponseWriter
	n int
	/*
	 * The status, kept so a short body can be told from a legitimate one.
	 *
	 * A 206 is *supposed* to be shorter than the file; a 200 that stops early
	 * is a delivery that was cut off. Without the code the two are one number
	 * and the interesting case cannot be seen.
	 */
	status int
	/** Whatever stopped the write, if anything did. */
	err error
}

func (c *countingWriter) WriteHeader(code int) {
	if c.status == 0 {
		c.status = code
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *countingWriter) Write(p []byte) (int, error) {
	// An implicit 200: ServeContent writes a body without calling WriteHeader
	// when there is nothing to negotiate.
	if c.status == 0 {
		c.status = http.StatusOK
	}
	n, err := c.ResponseWriter.Write(p)
	c.n += n
	if err != nil && c.err == nil {
		c.err = err
	}
	return n, err
}

/*
 * shortDelivery reports whether a response sent fewer bytes than it promised,
 * and what it promised.
 *
 * The first version judged only a 200, on the reasoning that a 206 is short on
 * purpose. That made it blind in exactly the place it was built for: WebView2
 * requests every playlist and segment with `Range: bytes=0-`, so every segment
 * the desktop client ever received was a 206 and the check could not fire.
 * Found by logging a real WebView2's requests, not by reading the code.
 *
 * A 206 is not short on purpose — it is short *by its own Content-Range*. So the
 * promise is read from that header, and a response is judged against it.
 */
func shortDelivery(status, sent int, size int64, contentRange string) (promised int64, short bool) {
	switch status {
	case http.StatusOK:
		promised = size
	case http.StatusPartialContent:
		var first, last, total int64
		if _, err := fmt.Sscanf(contentRange, "bytes %d-%d/%d", &first, &last, &total); err != nil || last < first {
			return 0, false
		}
		promised = last - first + 1
	default:
		return 0, false
	}
	return promised, int64(sent) < promised
}
