package api

import (
	"bytes"
	"errors"
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

/*
 * A segment that did not go out whole says so.
 *
 * The player reports DEMUXER_ERROR_COULD_NOT_PARSE *after* reaching
 * `readyState 4` — it starts, plays, and then a piece will not parse. A segment
 * truncated in flight produces exactly that shape, and it was invisible from
 * both ends: the server wrote what it could and moved on, the element only said
 * the result would not parse, and neither could see the other half.
 */

// A writer that gives up partway, the way a connection does.
type breakingWriter struct {
	http.ResponseWriter
	after int
	wrote int
}

func (b *breakingWriter) Write(p []byte) (int, error) {
	if b.wrote >= b.after {
		return 0, errors.New("connection reset by peer")
	}
	n := len(p)
	short := false
	if b.wrote+n > b.after {
		n, short = b.after-b.wrote, true
	}
	b.wrote += n
	written, err := b.ResponseWriter.Write(p[:n])
	if err == nil && short {
		// An io.Writer that returns fewer bytes than it was given must say why,
		// and a connection that dies mid-body is exactly that.
		err = errors.New("connection reset by peer")
	}
	return written, err
}

func TestAShortWriteIsCountedAndItsErrorKept(t *testing.T) {
	rec := httptest.NewRecorder()
	cw := &countingWriter{ResponseWriter: &breakingWriter{ResponseWriter: rec, after: 50}}
	cw.WriteHeader(http.StatusOK)
	_, err := cw.Write(bytes.Repeat([]byte("x"), 4096))

	if err == nil {
		t.Fatal("the write error did not reach the caller")
	}
	if cw.n != 50 {
		t.Errorf("counted %d bytes, want the 50 that actually went out", cw.n)
	}
	if cw.err == nil {
		t.Error("the reason delivery stopped was not kept, so the log line " +
			"cannot say why a segment was cut off")
	}
	if cw.status != http.StatusOK {
		t.Errorf("status = %d; without it a truncated 200 cannot be told from "+
			"a 206, which is short on purpose", cw.status)
	}
}

// ServeContent writes a body without calling WriteHeader when there is nothing
// to negotiate, and a status of zero would read as "not a 200" and hide every
// truncation on the ordinary path.
func TestAnImplicitTwoHundredIsStillATwoHundred(t *testing.T) {
	rec := httptest.NewRecorder()
	cw := &countingWriter{ResponseWriter: rec}
	_, _ = cw.Write([]byte("abc"))

	if cw.status != http.StatusOK {
		t.Errorf("status = %d, want 200", cw.status)
	}
}

// A range is meant to be shorter than the file. Judging one as truncated would
// bury the real case under every ordinary seek.
func TestARangeIsNotMistakenForATruncation(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/stream/7/hls/s/1.m4s", nil)
	r.Header.Set("Range", "bytes=0-99")
	cw, rec := serveSegment(t, r)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if cw.status != http.StatusPartialContent {
		t.Errorf("countingWriter recorded %d rather than the 206 it sent", cw.status)
	}
}

/*
 * The check sees the responses the desktop client actually gets.
 *
 * WebView2 asks for every segment with `Range: bytes=0-`, so every segment is a
 * 206 — and a check that judged only 200s could never fire in the one client it
 * was written for.
 */
func TestAWholeFileRangeThatStopsEarlyIsShort(t *testing.T) {
	// ServeContent's Content-Range for bytes=0- of a 10,233,627-byte segment.
	promised, short := shortDelivery(http.StatusPartialContent, 4096, 10_233_627, "bytes 0-10233626/10233627")
	if !short {
		t.Error("a bytes=0- response cut off after 4KB was not reported — the check is blind to what WebView2 requests")
	}
	if promised != 10_233_627 {
		t.Errorf("promised = %d, want the whole segment", promised)
	}
}

func TestACompleteRangeIsNotShort(t *testing.T) {
	if _, short := shortDelivery(http.StatusPartialContent, 100, 4096, "bytes 0-99/4096"); short {
		t.Error("a range that delivered everything it promised was reported short")
	}
}

// A partial range is judged against its own promise, not the file's size — the
// original reason 206s were excluded, kept.
func TestAPartialRangeIsJudgedAgainstItsOwnLength(t *testing.T) {
	if _, short := shortDelivery(http.StatusPartialContent, 100, 1_000_000, "bytes 500-599/1000000"); short {
		t.Error("a 100-byte range was judged against the whole file")
	}
	if _, short := shortDelivery(http.StatusPartialContent, 60, 1_000_000, "bytes 500-599/1000000"); !short {
		t.Error("a 100-byte range that sent 60 was not reported")
	}
}

func TestAShortTwoHundredIsStillShort(t *testing.T) {
	if _, short := shortDelivery(http.StatusOK, 10, 4096, ""); !short {
		t.Error("a 200 that stopped early is no longer reported")
	}
}

func TestOtherStatusesAreNotJudged(t *testing.T) {
	for _, st := range []int{http.StatusNotModified, http.StatusNotFound, http.StatusRequestedRangeNotSatisfiable} {
		if _, short := shortDelivery(st, 0, 4096, ""); short {
			t.Errorf("status %d was judged as a delivery", st)
		}
	}
	if _, short := shortDelivery(http.StatusPartialContent, 0, 4096, "garbage"); short {
		t.Error("an unreadable Content-Range was reported short")
	}
}
