package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"lancast/internal/store"
)

// liveChannel imports a channel and returns its id.
func liveChannel(t *testing.T, h *harness) int64 {
	t.Helper()
	provider := upstream(t, sampleList, "application/x-mpegurl")
	h.do(t, "POST", "/api/channel-sources",
		map[string]any{"name": "Provider", "url": provider.URL}).Body.Close()

	var list struct {
		Channels []store.Channel `json:"channels"`
	}
	decode(t, h.do(t, "GET", "/api/channels", nil), &list)
	if len(list.Channels) == 0 {
		t.Fatal("setup: no channels imported")
	}
	return list.Channels[0].ID
}

func TestLiveUnknownChannel(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "GET", "/api/channels/9999/live", nil),
		http.StatusNotFound, "not_found")
}

/*
 * Without ffmpeg the answer names the missing thing.
 *
 * The harness has no ffmpeg, which makes this the case it can actually assert —
 * and it is the one worth asserting, because a generic 500 sends somebody to
 * read logs when the fix is installing a program.
 */
func TestLiveWithoutFFmpegSaysSo(t *testing.T) {
	h := newHarness(t)
	id := liveChannel(t, h)

	if h.srvAPI.trans.Available() {
		t.Skip("ffmpeg is installed on this machine; the no-ffmpeg path cannot be exercised here")
	}
	wantError(t, h.do(t, "GET", "/api/channels/"+itoa(id)+"/live", nil),
		http.StatusServiceUnavailable, "no_ffmpeg")
}

// The raw relay stays available beside it: Safari plays HLS, and a client with
// its own demuxer should not be forced through an encode.
func TestTheRawRelayStillExists(t *testing.T) {
	h := newHarness(t)
	id := liveChannel(t, h)

	resp := h.do(t, "GET", "/api/channels/"+itoa(id)+"/stream", nil)
	defer resp.Body.Close()
	// The upstream in this harness serves a channel list rather than media, so
	// the status is not the point — the point is that the route is still routed
	// rather than 404, which is what removing it would look like.
	if resp.StatusCode == http.StatusNotFound {
		t.Error("the raw relay was removed; Safari and self-demuxing clients lose their path")
	}
}

/*
 * The live path, run for real.
 *
 * ffmpeg is present on this machine, so this exercises the whole chain rather
 * than a mock of it: an HTTP source, a probe, the copy-or-encode decision, and
 * fragmented MP4 arriving on the wire. The two properties asserted are the ones
 * that decide whether the feature works at all — that the body is fMP4 a
 * browser would accept, and that the process dies with the request.
 */
func TestLiveProducesFragmentedMP4(t *testing.T) {
	h := newHarness(t)
	if !h.srvAPI.trans.Available() {
		t.Skip("ffmpeg not installed")
	}

	source := ffmpegTestSource(t)
	provider := upstream(t, "#EXTM3U\n#EXTINF:-1,Test\n"+source+"\n", "application/x-mpegurl")
	h.do(t, "POST", "/api/channel-sources",
		map[string]any{"name": "Provider", "url": provider.URL}).Body.Close()

	var list struct {
		Channels []store.Channel `json:"channels"`
	}
	decode(t, h.do(t, "GET", "/api/channels", nil), &list)
	if len(list.Channels) == 0 {
		t.Fatal("setup: no channel imported")
	}

	resp := h.do(t, "GET", "/api/channels/"+itoa(list.Channels[0].ID)+"/live", nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); got != "video/mp4" {
		t.Errorf("content-type = %q, want video/mp4", got)
	}

	// An MP4 begins with an ftyp box: a four-byte length, then the tag. This is
	// the cheapest true statement about the output — it says the mux ran and
	// produced a container, rather than ffmpeg exiting and the handler
	// streaming nothing.
	head := make([]byte, 12)
	if _, err := io.ReadFull(resp.Body, head); err != nil {
		resp.Body.Close()
		t.Fatalf("reading the first bytes: %v", err)
	}
	if string(head[4:8]) != "ftyp" {
		resp.Body.Close()
		t.Errorf("stream does not start with an MP4 ftyp box: % x", head)
	}
	resp.Body.Close()
}

/*
 * The property this feature lives or dies on: ffmpeg stops when the viewer does.
 *
 * A live source never ends, so nothing else will ever stop the process. A leak
 * here does not sit idle — it pulls a stream at full rate for ever, for
 * somebody who closed the tab. One leaked session per abandoned channel would
 * take a home server down in an evening.
 */
func TestLiveStopsFFmpegWhenTheClientGoes(t *testing.T) {
	h := newHarness(t)
	if !h.srvAPI.trans.Available() {
		t.Skip("ffmpeg not installed")
	}

	source := ffmpegTestSource(t)
	provider := upstream(t, "#EXTM3U\n#EXTINF:-1,Test\n"+source+"\n", "application/x-mpegurl")
	h.do(t, "POST", "/api/channel-sources",
		map[string]any{"name": "Provider", "url": provider.URL}).Body.Close()

	var list struct {
		Channels []store.Channel `json:"channels"`
	}
	decode(t, h.do(t, "GET", "/api/channels", nil), &list)
	id := itoa(list.Channels[0].ID)

	resp := h.do(t, "GET", "/api/channels/"+id+"/live", nil)
	buf := make([]byte, 4096)
	_, _ = resp.Body.Read(buf)

	if n := len(h.srvAPI.trans.Sessions()); n == 0 {
		resp.Body.Close()
		t.Fatal("no transcode session while a channel is streaming")
	}

	// The viewer closes the tab.
	resp.Body.Close()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if len(h.srvAPI.trans.Sessions()) == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("ffmpeg is still running %v after the client went away",
		10*time.Second)
}

/*
 * ffmpegTestSource writes a two-second clip and serves it over HTTP.
 *
 * Generated rather than committed: a binary fixture in the repository is one
 * more thing to explain, and ffmpeg is already required for the test to mean
 * anything. Served over HTTP because that is what a channel is — reading a file
 * path would exercise a code path no channel ever takes.
 */
func ffmpegTestSource(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "source.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=160x120:rate=10:duration=2",
		"-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo:d=2",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest", "-y", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("could not generate a test clip: %v: %s", err, out)
	}

	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	t.Cleanup(srv.Close)
	return srv.URL + "/source.mp4"
}
