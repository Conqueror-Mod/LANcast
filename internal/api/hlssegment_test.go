package api

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lancast/internal/transcode"
)

/*
 * A segment nobody can serve says so.
 *
 * A `<video>` element cannot tell "this is not media" from "I could not fetch
 * the media": either way it reports MEDIA_ERR_SRC_NOT_SUPPORTED. The client
 * reads that as this device being unable to play a playlist at all and writes
 * it down — so a refused segment does not look like a refused segment. It looks
 * like a broken engine, on a machine whose engine is fine, and it retires the
 * segmented path for that device.
 *
 * That happened, twice. A film fell back from segments to the progressive
 * stream in under a second, with the playlist served correctly at 150ms and
 * **nothing whatsoever in the log** — while the same playlist, handed to the
 * same engine directly, played to readyState 4. The playlist route logs its own
 * failures; this one recorded nothing, which made it the last place a failure
 * on this path could hide.
 */

func segmentServer(t *testing.T) (*Server, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return &Server{
		log:   slog.New(slog.NewTextHandler(&buf, nil)),
		trans: transcode.NewManager(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil))),
	}, &buf
}

func segmentRequest(target string) *http.Request {
	r := httptest.NewRequest("GET", "/api/stream/7459/hls/"+target, nil)
	parts := strings.SplitN(target, "/", 2)
	r.SetPathValue("id", "7459")
	r.SetPathValue("session", parts[0])
	if len(parts) > 1 {
		r.SetPathValue("name", parts[1])
	}
	return r
}

func TestASegmentForAnUnknownSessionIsRefusedAndSaysSo(t *testing.T) {
	s, buf := segmentServer(t)

	rec := httptest.NewRecorder()
	s.hlsSegment(rec, segmentRequest("no-such-session/init.mp4"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	out := buf.String()
	if !strings.Contains(out, "hls segment refused") {
		t.Fatalf("nothing logged for a refused segment, so the failure is "+
			"invisible from both ends; got %q", out)
	}
	/*
	 * The name and the session travel with it. Which segment was asked for is
	 * the difference between "the init segment never arrived" — which kills
	 * playback outright — and "segment 400 of a long film went missing", which
	 * is a stutter. They are different faults and the line has to tell them
	 * apart.
	 */
	if !strings.Contains(out, "init.mp4") {
		t.Errorf("the line does not say which segment was refused: %q", out)
	}
	if !strings.Contains(out, "no-such-session") {
		t.Errorf("the line does not name the session: %q", out)
	}
	// How many are running distinguishes "reaped under us" from "never
	// existed", the same way the ceiling refusal carries its count.
	if !strings.Contains(out, "running=") {
		t.Errorf("the line does not say how many sessions are alive: %q", out)
	}
}

/*
 * A name that is not a shape ffmpeg produces is refused before the session is
 * looked up, and that refusal is about the request rather than the session — so
 * it answers 400, and does not add a line about a missing session that was
 * never the problem.
 */
func TestASegmentNameIsValidatedBeforeTheSessionIsLookedUp(t *testing.T) {
	s, buf := segmentServer(t)

	rec := httptest.NewRecorder()
	s.hlsSegment(rec, segmentRequest("no-such-session/..%2f..%2fsecret"))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a name ffmpeg would never produce", rec.Code)
	}
	if strings.Contains(buf.String(), "hls segment refused") {
		t.Errorf("a rejected name was reported as a missing session: %q", buf.String())
	}
}
