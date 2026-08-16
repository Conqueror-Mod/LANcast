package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lancast/internal/store"
)

const sampleList = `#EXTM3U
#EXTINF:-1 tvg-logo="https://logos.example/one.png" group-title="UK",Channel One
https://provider.example/one/index.m3u8
#EXTINF:-1 group-title="Sports",Channel Two
https://provider.example/two/index.m3u8
`

// upstream stands in for a provider. Real network calls in a test are a test
// that fails when somebody's wifi does.
func upstream(t *testing.T, body, contentType string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAddingASourceImportsItsChannels(t *testing.T) {
	h := newHarness(t)
	provider := upstream(t, sampleList, "application/x-mpegurl")

	var body struct {
		Source      store.ChannelSource `json:"source"`
		Channels    int                 `json:"channels"`
		ImportError string              `json:"import_error"`
	}
	resp := h.do(t, "POST", "/api/channel-sources",
		map[string]any{"name": "Provider", "url": provider.URL})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	decode(t, resp, &body)
	if body.ImportError != "" {
		t.Fatalf("import error: %s", body.ImportError)
	}
	if body.Channels != 2 {
		t.Errorf("channels = %d, want 2", body.Channels)
	}

	var list struct {
		Channels []store.Channel `json:"channels"`
	}
	decode(t, h.do(t, "GET", "/api/channels", nil), &list)
	if len(list.Channels) != 2 {
		t.Fatalf("listed %d channels, want 2", len(list.Channels))
	}
	// Source order is preserved: it is the order somebody curated, and
	// alphabetical is not an improvement on the order on a remote control.
	if list.Channels[0].Name != "Channel One" {
		t.Errorf("first channel = %q, want Channel One", list.Channels[0].Name)
	}
	if list.Channels[0].Group == nil || *list.Channels[0].Group != "UK" {
		t.Errorf("group = %v, want UK", list.Channels[0].Group)
	}
}

/*
 * The provider URL must never reach a client.
 *
 * Channel lists are routinely credentialed — a token in the path, a password in
 * the query — so publishing the URL to every browser on the LAN would be
 * publishing the subscription.
 */
func TestChannelURLIsNeverSerialised(t *testing.T) {
	h := newHarness(t)
	provider := upstream(t, sampleList, "application/x-mpegurl")
	h.do(t, "POST", "/api/channel-sources",
		map[string]any{"name": "Provider", "url": provider.URL}).Body.Close()

	resp := h.do(t, "GET", "/api/channels", nil)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "provider.example") {
		t.Error("the channel listing leaked the upstream URL to the client")
	}
}

/*
 * Refreshing replaces rather than merges.
 *
 * A channel list is a snapshot published by somebody else, not a collection
 * curated here — and the file carries no id worth trusting across versions, so
 * merging means guessing at identity and duplicating every channel each time
 * the guess is wrong.
 */
func TestRefreshReplacesRatherThanDuplicating(t *testing.T) {
	h := newHarness(t)
	provider := upstream(t, sampleList, "application/x-mpegurl")

	var created struct {
		Source store.ChannelSource `json:"source"`
	}
	decode(t, h.do(t, "POST", "/api/channel-sources",
		map[string]any{"name": "Provider", "url": provider.URL}), &created)

	for i := 0; i < 3; i++ {
		resp := h.do(t, "POST", "/api/channel-sources/"+itoa(created.Source.ID)+"/refresh", nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("refresh %d = %d, want 200", i, resp.StatusCode)
		}
	}

	var list struct {
		Channels []store.Channel `json:"channels"`
	}
	decode(t, h.do(t, "GET", "/api/channels", nil), &list)
	if len(list.Channels) != 2 {
		t.Errorf("channels = %d after three refreshes, want 2", len(list.Channels))
	}
}

// A lapsed subscription serves an HTML error page with a 200. Importing that as
// one channel called "<!DOCTYPE html>" would look like it worked.
func TestAnHTMLErrorPageIsNotAChannelList(t *testing.T) {
	h := newHarness(t)
	provider := upstream(t, "<html><body>403</body></html>", "text/html")

	var body struct {
		ImportError string `json:"import_error"`
	}
	decode(t, h.do(t, "POST", "/api/channel-sources",
		map[string]any{"name": "Broken", "url": provider.URL}), &body)
	if body.ImportError == "" {
		t.Error("an HTML page was accepted as a channel list")
	}
}

/*
 * Adding a source makes the server fetch a URL the caller chose, which is
 * server-side request forgery in miniature. The route is admin-gated; this is
 * the second line, because "admin" on a household server is not the same
 * standard as "trusted to reach the loopback interface".
 */
func TestSourceURLsAreChecked(t *testing.T) {
	h := newHarness(t)
	for _, bad := range []string{
		"file:///etc/passwd",
		"not a url at all",
		// This server's own API, which is the address the guard exists for.
		h.srv.URL + "/api/settings",
	} {
		wantError(t, h.do(t, "POST", "/api/channel-sources",
			map[string]any{"name": "Bad", "url": bad}),
			http.StatusBadRequest, "bad_request")
	}
}

/*
 * A tuner on the same machine is allowed, and that is deliberate.
 *
 * The first version of the guard banned loopback outright, which reads as
 * cautious and is wrong: a tvheadend or a local transcoder on the same box is
 * one of the most ordinary sources Live TV has, and refusing it would make the
 * feature useless on the setup it suits best. What needs protecting is one
 * origin — this API — not one interface.
 */
func TestALocalTunerOnAnotherPortIsAllowed(t *testing.T) {
	h := newHarness(t)
	tuner := upstream(t, sampleList, "application/x-mpegurl")

	resp := h.do(t, "POST", "/api/channel-sources",
		map[string]any{"name": "Local tuner", "url": tuner.URL})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201 — a tuner on this machine is a legitimate source",
			resp.StatusCode)
	}
}

/*
 * The proxy takes a channel id, not a URL — and a relative path is resolved
 * against that channel's own base, so nothing a caller sends can change the
 * host. That property is what keeps this from being an open relay inside
 * somebody's network.
 */
func TestStreamProxyCannotBePointedElsewhere(t *testing.T) {
	h := newHarness(t)
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
	id := itoa(list.Channels[0].ID)

	for _, bad := range []string{
		"https://evil.example/steal",
		"//evil.example/steal",
	} {
		wantError(t, h.do(t, "GET", "/api/channels/"+id+"/stream?path="+bad, nil),
			http.StatusBadRequest, "bad_request")
	}
}

func TestStreamUnknownChannel(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "GET", "/api/channels/9999/stream", nil),
		http.StatusNotFound, "not_found")
}
