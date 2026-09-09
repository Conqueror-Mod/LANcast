package api

import "net/http"

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
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.ResponseWriter.Write(p)
	c.n += n
	return n, err
}
