package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

/*
 * The byte count a session's whole lifetime is decided on.
 *
 * A session that has served no picture is reaped in thirty seconds instead of
 * ten minutes, which is the fix for a server that stopped playing anything
 * after three seeks. That rule is only as good as this number, and this number
 * did not exist on the HLS path: segments go out through `http.ServeContent`,
 * which writes the body itself, so nothing ever told the session and every HLS
 * session reported zero whether it was working or abandoned.
 *
 * Getting this wrong in the safe-looking direction is the dangerous one. A
 * count that stays zero while a film plays reaps the film.
 */

var segmentModTime = time.Date(2026, 9, 8, 23, 41, 34, 0, time.UTC)

func serveSegment(t *testing.T, r *http.Request) (*countingWriter, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	cw := &countingWriter{ResponseWriter: rec}
	body := bytes.NewReader(bytes.Repeat([]byte("x"), 4096))
	// A real modification time: net/http treats the Unix epoch as "no modtime"
	// and skips conditional handling entirely, which would quietly turn the
	// 304 case below into a 200 that proves nothing.
	http.ServeContent(cw, r, "1.m4s", segmentModTime, body)
	return cw, rec
}

func TestServingASegmentCountsWhatWentOut(t *testing.T) {
	cw, rec := serveSegment(t, httptest.NewRequest("GET", "/api/stream/7/hls/s/1.m4s", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if cw.n != 4096 {
		t.Errorf("counted %d bytes of 4096; a working HLS session that reports "+
			"zero is reaped as an abandoned one", cw.n)
	}
}

/*
 * A range request counts the range, not the file.
 *
 * A player asking for the tail of a segment has still been handed picture, and
 * this is the shape of request that actually arrives — so a count that only
 * understood whole files would read a seeking client as an idle one.
 */
func TestARangeCountsWhatTheRangeHeld(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/stream/7/hls/s/1.m4s", nil)
	r.Header.Set("Range", "bytes=0-99")
	cw, rec := serveSegment(t, r)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if cw.n != 100 {
		t.Errorf("counted %d bytes of 100", cw.n)
	}
}

/*
 * A 304 is not picture.
 *
 * The client already has the segment, so nothing was handed over and nothing
 * should be counted. It matters because the caller only reports a non-zero
 * count: a 304 that counted as delivery would keep a slot alive on the strength
 * of a response with no body in it.
 */
func TestANotModifiedCountsNothing(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/stream/7/hls/s/1.m4s", nil)
	r.Header.Set("If-Modified-Since", segmentModTime.UTC().Format(http.TimeFormat))
	cw, rec := serveSegment(t, r)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", rec.Code)
	}
	if cw.n != 0 {
		t.Errorf("counted %d bytes for a response with no body", cw.n)
	}
}

// Headers and status still reach the real writer: the wrapper is a counter, not
// a replacement, and a segment served through it has to still be a segment.
func TestTheWrapperDoesNotSwallowTheResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	cw := &countingWriter{ResponseWriter: rec}
	cw.Header().Set("Content-Type", "video/iso.segment")
	cw.WriteHeader(http.StatusOK)
	_, _ = cw.Write([]byte("abc"))

	if got := rec.Header().Get("Content-Type"); got != "video/iso.segment" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Body.String(); got != "abc" {
		t.Errorf("body = %q", got)
	}
	if cw.n != 3 {
		t.Errorf("counted %d, want 3", cw.n)
	}
}
