package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"lancast/internal/store"
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

// probeAsHEVCWithTwoAudioTracks stores a file that exercises both the profile
// and the track-selection paths: HEVC video the default profile cannot decode,
// a default TrueHD track, and an AAC alternate.
func (h *harness) probeAsHEVCWithTwoAudioTracks(t *testing.T, id int64) {
	t.Helper()
	err := h.st.SaveProbe(context.Background(), id, store.ProbeResult{
		DurationMS: 7_200_000, Container: "matroska",
		VideoCodec: "hevc", Width: 3840, Height: 2160,
		AudioCodec: "truehd", AudioChannels: 8,
		Streams: []store.MediaStream{
			{Index: 0, Kind: "video", Codec: "hevc", Width: 3840, Height: 2160, Default: true},
			{Index: 1, Kind: "audio", Codec: "truehd", Channels: 8, Default: true},
			{Index: 2, Kind: "audio", Codec: "aac", Channels: 6},
		},
	})
	if err != nil {
		t.Fatalf("SaveProbe: %v", err)
	}
}

// The decision and ffmpeg's -map must be about the same stream. Asking for the
// AAC track on a file whose default is TrueHD must change the answer.
func TestPlaybackDecisionFollowsTheChosenAudioTrack(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "movie.mkv", []byte("data"))
	h.probeAsHEVCWithTwoAudioTracks(t, id)

	get := func(query string) string {
		resp := h.do(t, "GET", "/api/items/"+itoa(id)+"/playback"+query, nil)
		defer resp.Body.Close()
		var body struct {
			Decision struct {
				AudioAction string `json:"audio_action"`
			} `json:"decision"`
		}
		decode(t, resp, &body)
		return body.Decision.AudioAction
	}

	if got := get(""); got != "encode" {
		t.Errorf("audio_action = %q for the default TrueHD track, want encode", got)
	}
	if got := get("?audio=2"); got != "copy" {
		t.Errorf("audio_action = %q for the chosen AAC track, want copy", got)
	}
}

// A track that does not exist must be refused, not silently swapped for
// another one — the caller would have no way to know it got different audio.
func TestUnknownAudioTrackIsRefused(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "movie.mkv", []byte("data"))
	h.probeAsHEVCWithTwoAudioTracks(t, id)

	for _, path := range []string{
		"/api/items/" + itoa(id) + "/playback?audio=99",
		"/api/stream/" + itoa(id) + "/transcode?audio=99",
		"/api/stream/" + itoa(id) + "/hls/index.m3u8?audio=99",
	} {
		wantError(t, h.do(t, "GET", path, nil), 400, "bad_request")
	}
	// Index 0 is the video stream, not an audio track.
	wantError(t, h.do(t, "GET", "/api/items/"+itoa(id)+"/playback?audio=0", nil),
		400, "bad_request")
}

// The profile is what makes an HEVC file a remux rather than a full re-encode.
func TestPlaybackProfileChangesTheDecision(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "movie.mkv", []byte("data"))
	h.probeAsHEVCWithTwoAudioTracks(t, id)

	get := func(query string) (string, string, string) {
		resp := h.do(t, "GET", "/api/items/"+itoa(id)+"/playback"+query, nil)
		defer resp.Body.Close()
		var body struct {
			Profile  string `json:"profile"`
			Decision struct {
				Method      string `json:"method"`
				VideoAction string `json:"video_action"`
			} `json:"decision"`
		}
		decode(t, resp, &body)
		return body.Profile, body.Decision.Method, body.Decision.VideoAction
	}

	if name, _, action := get(""); name != "browser" || action != "encode" {
		t.Errorf("default: profile = %q, video_action = %q; want browser/encode", name, action)
	}
	if name, _, action := get("?profile=safari"); name != "safari" || action != "copy" {
		t.Errorf("safari: profile = %q, video_action = %q; want safari/copy", name, action)
	}
	if name, method, _ := get("?profile=tv&audio=2"); name != "tv" || method != "direct" {
		t.Errorf("tv: profile = %q, method = %q; want tv/direct", name, method)
	}
	// An unrecognised profile falls back to the conservative default rather
	// than erroring: a client naming a profile this build does not know should
	// still play something.
	if name, _, _ := get("?profile=nonsense"); name != "browser" {
		t.Errorf("unknown profile resolved to %q, want the browser fallback", name)
	}
}

// requireProber skips a test that needs ffprobe on a machine without it.
// LANcast runs fine without ffprobe, so this is a supported configuration
// rather than a broken one.
func (h *harness) requireProber(t *testing.T) {
	t.Helper()
	resp := h.do(t, "GET", "/api/probe", nil)
	defer resp.Body.Close()
	var body struct {
		Available bool `json:"available"`
	}
	decode(t, resp, &body)
	if !body.Available {
		t.Skip("ffprobe is not installed")
	}
}

// A probe is only as good as the build that made it. Nothing in the normal
// queue revisits an already-probed item — the pending query is
// "probed_at IS NULL" — so a field the prober learned to record later needs an
// explicit re-probe.
func TestReprobeRequeuesItems(t *testing.T) {
	h := newHarness(t)
	h.requireProber(t)
	id := h.addFile(t, "movie.mkv", []byte("data"))
	h.probeAsHEVCWithTwoAudioTracks(t, id) // stored without pix_fmt

	queued := func(query string) int64 {
		resp := h.do(t, "POST", "/api/probe/refresh"+query, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		var body struct {
			Scope  string `json:"scope"`
			Queued int64  `json:"queued"`
		}
		decode(t, resp, &body)
		return body.Queued
	}

	// The default is the narrow scope: only what a current build would learn
	// something from.
	if n := queued(""); n != 1 {
		t.Errorf("default scope queued %d, want 1", n)
	}
	// Already queued, so nothing further to do.
	if n := queued("?scope=incomplete"); n != 0 {
		t.Errorf("second pass queued %d, want 0", n)
	}
}

func TestReprobeAllScopeIsExplicit(t *testing.T) {
	h := newHarness(t)
	h.requireProber(t)
	id := h.addFile(t, "movie.mkv", []byte("data"))

	// Probed complete: the narrow scope has nothing to do, but "all" still
	// requeues it. That difference is the whole point of the parameter.
	err := h.st.SaveProbe(context.Background(), id, store.ProbeResult{
		DurationMS: 100, Container: "matroska",
		VideoCodec: "h264", Width: 1920, Height: 1080,
		AudioCodec: "aac", AudioChannels: 2,
		Streams: []store.MediaStream{
			{Index: 0, Kind: "video", Codec: "h264", PixFmt: "yuv420p", Default: true},
			{Index: 1, Kind: "audio", Codec: "aac", Channels: 2, Default: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	get := func(query string) int64 {
		resp := h.do(t, "POST", "/api/probe/refresh"+query, nil)
		defer resp.Body.Close()
		var body struct {
			Queued int64 `json:"queued"`
		}
		decode(t, resp, &body)
		return body.Queued
	}

	if n := get("?scope=incomplete"); n != 0 {
		t.Errorf("incomplete scope queued %d, want 0 — pix_fmt is already stored", n)
	}
	if n := get("?scope=all"); n != 1 {
		t.Errorf("all scope queued %d, want 1", n)
	}
}

func TestReprobeRejectsBadInput(t *testing.T) {
	h := newHarness(t)
	h.requireProber(t)

	wantError(t, h.do(t, "POST", "/api/probe/refresh?scope=nonsense", nil), 400, "bad_request")
	wantError(t, h.do(t, "POST", "/api/probe/refresh?scope=all&library=abc", nil), 400, "bad_request")
	wantError(t, h.do(t, "POST", "/api/probe/refresh?scope=all&library=9999", nil), 404, "not_found")
}
