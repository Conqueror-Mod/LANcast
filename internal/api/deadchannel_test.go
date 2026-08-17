package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lancast/internal/store"
)

/*
 * A dead channel says it is dead.
 *
 * A provider's list carries entries whose source has gone — a real one had 1,862
 * channels, and a 404 among them is ordinary. The response used to commit to
 * `200 OK` before any bytes existed, so when ffmpeg failed the browser received
 * an empty video stream and reported `DEMUXER_ERROR_COULD_NOT_OPEN`. The viewer
 * saw a broken application; the server had known the real answer since the first
 * 400 milliseconds.
 */
func deadSourceChannel(t *testing.T, h *harness) int64 {
	t.Helper()

	// An upstream that answers the channel list, then 404s the media itself —
	// which is exactly the shape of a stale provider entry.
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	t.Cleanup(dead.Close)

	list := "#EXTM3U\n#EXTINF:-1,Dead Channel\n" + dead.URL + "/channel.m3u8\n"
	provider := upstream(t, list, "application/x-mpegurl")
	h.do(t, "POST", "/api/channel-sources",
		map[string]any{"name": "Provider", "url": provider.URL}).Body.Close()

	var got struct {
		Channels []store.Channel `json:"channels"`
	}
	decode(t, h.do(t, "GET", "/api/channels", nil), &got)
	if len(got.Channels) == 0 {
		t.Fatal("setup: no channels imported")
	}
	return got.Channels[0].ID
}

func TestADeadChannelAnswersWithAReason(t *testing.T) {
	h := newHarness(t)
	if !h.srvAPI.trans.Available() {
		t.Skip("ffmpeg not installed")
	}
	id := deadSourceChannel(t, h)

	resp := h.do(t, "GET", "/api/channels/"+itoa(id)+"/live", nil)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("a dead channel answered 200 — the browser gets an empty video stream " +
			"and reports a demuxer error instead of the reason")
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding the error: %v", err)
	}
	if body.Error.Code != "channel_unavailable" {
		t.Errorf("code = %q, want channel_unavailable", body.Error.Code)
	}
	if !strings.Contains(body.Error.Message, "404") {
		t.Errorf("message = %q, want it to name the upstream status", body.Error.Message)
	}
}

/*
 * The message must never carry the upstream URL.
 *
 * ffmpeg writes the full URL into its stderr, and channel URLs from a provider
 * are routinely credentialed — publishing one hands out the subscription. Only a
 * classification derived from that text may leave the server, which is why the
 * handler passes stderr through FailureReason rather than forwarding it.
 */
func TestAFailureNeverLeaksTheUpstreamURL(t *testing.T) {
	h := newHarness(t)
	if !h.srvAPI.trans.Available() {
		t.Skip("ffmpeg not installed")
	}
	id := deadSourceChannel(t, h)

	var ch store.Channel
	decode(t, h.do(t, "GET", "/api/channels/"+itoa(id), nil), &ch)

	resp := h.do(t, "GET", "/api/channels/"+itoa(id)+"/live", nil)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)

	for _, leak := range []string{"http://", "https://", ".m3u8", "127.0.0.1"} {
		if strings.Contains(body, leak) {
			t.Errorf("the response contains %q — an upstream URL reached a client:\n%s",
				leak, body)
		}
	}
}
