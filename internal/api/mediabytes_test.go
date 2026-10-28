package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Streaming a file adds exactly its size to the media total; an ordinary
// route adds nothing.
func TestMediaSentCountsWhatAStreamSends(t *testing.T) {
	h := newHarness(t)
	film := []byte(strings.Repeat("a film's bytes ", 4000))
	id := h.addFile(t, "film.mkv", film)

	before := h.srvAPI.counters.mediaBytes.Load()
	resp := h.do(t, "GET", "/api/items/"+itoa(id), nil)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if got := h.srvAPI.counters.mediaBytes.Load() - before; got != 0 {
		t.Fatalf("an item's details counted as %d media bytes", got)
	}

	resp = h.do(t, "GET", "/api/stream/"+itoa(id), nil)
	n, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || n != int64(len(film)) {
		t.Fatalf("stream: %d, %d bytes", resp.StatusCode, n)
	}
	if got := h.srvAPI.counters.mediaBytes.Load() - before; got != int64(len(film)) {
		t.Fatalf("media sent %d, want %d", got, len(film))
	}

	// A range is what was sent, not the file.
	req, _ := http.NewRequest("GET", h.srv.URL+"/api/stream/"+itoa(id), nil)
	req.Header.Set("Range", "bytes=0-99")
	if h.cookie != nil {
		req.AddCookie(h.cookie)
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if got := h.srvAPI.counters.mediaBytes.Load() - before; got != int64(len(film))+100 {
		t.Fatalf("after a 100-byte range: %d", got)
	}
}

// The event stream is never a media route: it needs its Flush, and a wrapper
// on it is how live updates would break quietly (ADR 0096).
func TestTheEventStreamIsNeverMetered(t *testing.T) {
	for _, p := range []string{"/api/events", "/api/items/7", "/api/libraries", "/api/items/7/previews/0"} {
		if isMediaPath(p) {
			t.Errorf("%s is metered", p)
		}
	}
	for _, p := range []string{"/api/stream/7", "/api/stream/7/hls/abc/seg-001.ts", "/api/items/7/download",
		"/api/federation/stream", "/api/peers/AAAA/hls/7/s/seg.ts", "/api/channels/3/live"} {
		if !isMediaPath(p) {
			t.Errorf("%s is not metered", p)
		}
	}
}

// The meter hides nothing a media route uses: Flush and ReadFrom pass through
// and are counted, and Unwrap reaches the real writer.
func TestTheMeterPassesThrough(t *testing.T) {
	var n atomic.Int64
	rec := httptest.NewRecorder()
	m := mediaMeter{ResponseWriter: rec, n: &n}
	m.Write([]byte("abc"))
	m.Flush()
	if !rec.Flushed {
		t.Fatal("Flush did not reach the writer")
	}
	m.ReadFrom(strings.NewReader("defgh"))
	if n.Load() != 8 || rec.Body.String() != "abcdefgh" {
		t.Fatalf("counted %d, body %q", n.Load(), rec.Body.String())
	}
	if m.Unwrap() != rec {
		t.Fatal("Unwrap does not reach the writer")
	}
	var _ http.Flusher = m
	var _ io.ReaderFrom = m
}
