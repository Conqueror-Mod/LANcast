package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// setLiveTV flips the live_tv setting on the harness's server.
func setLiveTV(t *testing.T, h *harness, on bool) {
	t.Helper()
	next := h.settings.Get()
	next.LiveTV = on
	if err := h.settings.Set(next); err != nil {
		t.Fatal(err)
	}
}

// Every live TV route, as registered in server.go.
var liveTVRoutes = []struct{ method, path string }{
	{"GET", "/api/channels"},
	{"GET", "/api/channels/1/stream"},
	{"GET", "/api/channels/1/live"},
	{"GET", "/api/channels/1/hls/index.m3u8"},
	{"GET", "/api/channels/1/hls/s/seg.m4s"},
	{"POST", "/api/channels/1/stop"},
	{"GET", "/api/guide"},
	{"GET", "/api/channels/1/guide"},
	{"GET", "/api/channel-sources"},
	{"POST", "/api/channel-sources"},
	{"POST", "/api/channel-sources/1/refresh"},
	{"PATCH", "/api/channel-sources/1"},
	{"DELETE", "/api/channel-sources/1"},
}

/*
 * Off, live TV answers nothing: every route refuses by name, so a client can
 * tell "turned off" from "broken".
 */
func TestLiveTVOffRefusesEveryRoute(t *testing.T) {
	h := newHarness(t)
	setLiveTV(t, h, false)
	for _, rt := range liveTVRoutes {
		var body any
		if rt.method == "POST" || rt.method == "PATCH" {
			body = map[string]any{}
		}
		resp := h.do(t, rt.method, rt.path, body)
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("%s %s: status %d, want 409", rt.method, rt.path, resp.StatusCode)
		}
		wantError(t, resp, http.StatusConflict, "live_tv_off")
	}
}

/*
 * The status every client reads says whether live TV is on, so the app can
 * leave it out of the nav for everybody, members included.
 */
func TestAuthStatusSaysWhetherLiveTVIsOn(t *testing.T) {
	h := newHarness(t)
	for _, on := range []bool{false, true} {
		setLiveTV(t, h, on)
		var st struct {
			LiveTV *bool `json:"live_tv"`
		}
		decode(t, h.do(t, "GET", "/api/auth/status", nil), &st)
		if st.LiveTV == nil || *st.LiveTV != on {
			t.Errorf("live_tv = %v with the setting %v", st.LiveTV, on)
		}
	}
}

/*
 * Off removes nothing. A channel source added while it was on is there again
 * when it comes back on, with its channels.
 */
func TestLiveTVOffKeepsWhatWasThere(t *testing.T) {
	h := newHarness(t)
	liveChannel(t, h) // adds a source and imports its channels

	setLiveTV(t, h, false)
	setLiveTV(t, h, true)

	var list struct {
		Channels []any `json:"channels"`
	}
	decode(t, h.do(t, "GET", "/api/channels", nil), &list)
	if len(list.Channels) == 0 {
		t.Error("channels gone after live TV was turned off and on again")
	}
}

/*
 * The guide's own fetch makes no request while live TV is off: a parked
 * feature does not reach out to a guide provider every twelve hours.
 */
func TestGuideRefreshAsksNothingWhileOff(t *testing.T) {
	h := newHarness(t)
	var asked atomic.Int32
	guide := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<tv></tv>`))
	}))
	t.Cleanup(guide.Close)
	provider := upstream(t, sampleList, "application/x-mpegurl")
	h.do(t, "POST", "/api/channel-sources",
		map[string]any{"name": "Provider", "url": provider.URL, "epg_url": guide.URL}).Body.Close()

	// On, it does ask: without this the test below would pass for a guide
	// that was never wired up at all.
	h.srvAPI.RefreshGuides(context.Background())
	if asked.Load() == 0 {
		t.Fatal("setup: the guide was not fetched with live TV on")
	}

	setLiveTV(t, h, false)
	before := asked.Load()
	h.srvAPI.RefreshGuides(context.Background())
	if got := asked.Load(); got != before {
		t.Errorf("the guide was fetched %d time(s) while live TV was off", got-before)
	}
}
