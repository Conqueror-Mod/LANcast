package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestRewritePlaylist(t *testing.T) {
	in := strings.Join([]string{
		"#EXTM3U",
		"#EXT-X-VERSION:7",
		"#EXT-X-TARGETDURATION:6",
		`#EXT-X-MAP:URI="init.mp4"`,
		"#EXTINF:6.000,",
		"seg00000.m4s",
		"#EXTINF:6.000,",
		"seg00001.m4s",
		"#EXT-X-ENDLIST",
	}, "\n")

	out := rewritePlaylist(in, "/api/stream/7/hls/abc/")

	if !strings.Contains(out, "/api/stream/7/hls/abc/seg00000.m4s") {
		t.Error("segment reference was not rewritten to the API path")
	}
	if !strings.Contains(out, `#EXT-X-MAP:URI="/api/stream/7/hls/abc/init.mp4"`) {
		t.Error("the init segment URI was not rewritten")
	}
	// Directive lines must be untouched.
	if !strings.Contains(out, "#EXT-X-TARGETDURATION:6") {
		t.Error("a directive line was altered")
	}
	// A bare filename must not survive — that would point at nothing.
	if strings.Contains(out, "\nseg00000.m4s") {
		t.Error("a bare segment filename leaked through")
	}
}

// Segment names arrive in URLs and become filesystem paths, so only the exact
// shapes ffmpeg produces are allowed.
func TestValidSegmentName(t *testing.T) {
	good := []string{"init.mp4", "seg00000.m4s", "seg00001.m4s", "seg99999.m4s"}
	for _, n := range good {
		if !validSegmentName(n) {
			t.Errorf("validSegmentName(%q) = false, want true", n)
		}
	}

	bad := []string{
		"", "seg.m4s", "seg00000.mp4", "index.m3u8",
		"../seg00000.m4s", "..\\init.mp4", "seg00000.m4s/../../etc",
		"segABCDE.m4s", "seg000000000.m4s", "/etc/passwd",
	}
	for _, n := range bad {
		if validSegmentName(n) {
			t.Errorf("validSegmentName(%q) = true, want false", n)
		}
	}
}

// Transcode endpoints must require a session once a password is set, exactly
// like direct streaming — a public transcode URL would make the password
// decorative.
func TestTranscodeRequiresAuth(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "movie.mkv", []byte("data"))
	h.secure(t, "a good long password")

	for _, path := range []string{
		"/api/stream/" + itoa(id) + "/transcode",
		"/api/stream/" + itoa(id) + "/hls/index.m3u8",
		"/api/transcode",
	} {
		resp := h.do(t, "GET", path, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", path, resp.StatusCode)
		}
	}
}

// A file the client can already play must not transcode — that is pure wasted
// CPU. The unprobed test fixtures decide direct-play, so this is a conflict.
func TestTranscodeRefusesDirectPlayableFile(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "movie.mkv", []byte("data"))

	resp := h.do(t, "GET", "/api/stream/"+itoa(id)+"/transcode", nil)
	defer resp.Body.Close()
	// Unprobed → decision is direct play → transcode endpoint returns conflict.
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409 for a directly-playable file", resp.StatusCode)
	}
}

func TestTranscodeSessionsListed(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "GET", "/api/transcode", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Available bool `json:"available"`
		Sessions  []struct {
			ID string `json:"id"`
		} `json:"sessions"`
	}
	decode(t, resp, &body)
	if body.Sessions == nil {
		t.Error("sessions should be an empty array, not null")
	}
}

func TestTranscodeUnknownItem(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "GET", "/api/stream/9999/transcode", nil), 404, "not_found")
	wantError(t, h.do(t, "GET", "/api/stream/abc/transcode", nil), 400, "bad_request")
}
