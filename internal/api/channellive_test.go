package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

	sessions := h.srvAPI.trans.Sessions()
	if len(sessions) == 0 {
		resp.Body.Close()
		t.Fatal("no transcode session while a channel is streaming")
	}
	/*
	 * The probe has to have worked, and this is where that is checked.
	 *
	 * A source that never ends is exactly what defeats a probe, so making the
	 * test source realistic risks quietly moving it onto the *unprobed*
	 * fallback — which copies video and re-encodes audio, and would keep this
	 * test passing while testing something else. H.264 + AAC over MPEG-TS is a
	 * remux, so `Encoding` must be false; if this ever fails, the probe is
	 * timing out rather than answering.
	 */
	if sessions[0].Encoding {
		resp.Body.Close()
		t.Fatalf("session is re-encoding: the probe did not answer, so this is "+
			"the unprobed fallback rather than the remux it should be: %+v", sessions[0])
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
 * ffmpegTestSource serves a channel that behaves like a channel: bytes arrive
 * steadily and the stream does not end.
 *
 * It used to be a two-second MP4 behind an `http.FileServer`, and that is a
 * *file*, not a live source. ffmpeg read all of it at disk speed, finished, and
 * exited — usually before the test that had just started it could look. So
 * `TestLiveStopsFFmpegWhenTheClientGoes` asserted "a session is running" against
 * a session that had frequently already ended, and passed only when it won the
 * race. Measured on a developer machine with ffmpeg present: **three failures
 * in four runs**. CI never saw it, because CI has no ffmpeg and the test skips —
 * so it cost local time and taught people to ignore red from `go test ./...`.
 *
 * The property under test cannot survive a source that stops on its own. "ffmpeg
 * stops when the viewer does" is unfalsifiable if ffmpeg had already stopped for
 * its own reasons.
 *
 * Two changes make it a stream. **MPEG-TS rather than MP4**, because a live
 * channel is a transport stream and because MP4 keeps its `moov` at the end —
 * ffmpeg cannot begin on a partial one, so a trickled MP4 would produce nothing
 * at all. And the handler **writes in chunks and then holds the connection
 * open** rather than closing it, which is what an endless source looks like from
 * the reading end.
 *
 * Generated rather than committed: a binary fixture in the repository is one
 * more thing to explain, and ffmpeg is already required for the test to mean
 * anything. Served over HTTP because that is what a channel is — reading a file
 * path would exercise a code path no channel ever takes.
 */
func ffmpegTestSource(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "source.ts")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=160x120:rate=10:duration=4",
		"-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo:d=4",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest", "-f", "mpegts", "-y", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("could not generate a test clip: %v: %s", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the generated clip: %v", err)
	}

	// Closed before the server is, so a held request lets go and Close does not
	// block on it. Cleanups run last-registered-first, which is why this one is
	// registered second.
	held := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		const chunk = 8 << 10
		/*
		 * Loop the payload rather than sending it once and holding the
		 * connection open.
		 *
		 * Holding an idle connection is not what a channel does, and the probe
		 * proves it: `probeChannel` reads until it has enough or until its
		 * timeout, so a source that goes silent after 57KB makes every request
		 * wait out the clock. Bytes that keep arriving let it finish at once,
		 * which is also the honest imitation — a channel is never quiet.
		 */
		for off := 0; ; off += chunk {
			if off >= len(data) {
				off = 0
			}
			end := off + chunk
			if end > len(data) {
				end = len(data)
			}
			if _, err := w.Write(data[off:end]); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			select {
			case <-held:
				return
			case <-r.Context().Done():
				return
			case <-time.After(2 * time.Millisecond):
			}
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(held) })
	return srv.URL + "/source.ts"
}
