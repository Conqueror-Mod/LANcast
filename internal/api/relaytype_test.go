package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"lancast/internal/identity"
	"lancast/internal/peer"
	"lancast/internal/store"
)

/*
 * A paired server that has turned hostile does not get to name the type of the
 * bytes this server relays for it (R02-1).
 *
 * The far end is a real peer TLS listener with its own identity, reached
 * through the pinned client the shipped relay uses, so what is asserted is the
 * response a browser in this household would actually receive.
 */

// hostilePeer pairs h with a server that answers every route with whatever
// Content-Type and body the test chose, and returns its fingerprint.
func hostilePeer(t *testing.T, h *harness, contentType string, body []byte) string {
	t.Helper()
	id, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := peer.ServerConfig(id)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Set-Cookie", "session=theirs")
		_, _ = w.Write(body)
	}))
	srv.TLS = cfg
	srv.StartTLS()
	t.Cleanup(srv.Close)

	inv, err := peer.Encode(id, "Mallory's", []string{srv.Listener.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	h.authed(t, "POST", "/api/peers", map[string]any{"invite": inv}).Body.Close()
	fp := identity.Normalize(id.Fingerprint())
	if err := h.st.SetPeerState(context.Background(), fp, store.PeerPaired); err != nil {
		t.Fatal(err)
	}
	return fp
}

func assertHardened(t *testing.T, resp *http.Response) {
	t.Helper()
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := resp.Header.Get("Content-Security-Policy"); got != "sandbox; default-src 'none'" {
		t.Errorf("Content-Security-Policy = %q", got)
	}
	if got := resp.Header.Get("Set-Cookie"); got != "" {
		t.Errorf("their Set-Cookie came through: %q", got)
	}
}

func TestAPeersScriptIsNeverServedAsAPage(t *testing.T) {
	script := []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="fetch('/api/keys',{method:'POST'})"/>`)
	for i, declared := range []string{
		"text/html; charset=utf-8",
		"image/svg+xml",
		"application/xhtml+xml",
		"IMAGE/SVG+XML; charset=utf-8",
		"",
		"not a type at all",
	} {
		// Named by index: an empty name becomes "#00", and a '#' in the data
		// directory's path truncates the store's file: DSN.
		t.Run(fmt.Sprintf("case%d", i), func(t *testing.T) {
			h := newHarness(t)
			h.secure(t, "a good long password")
			fp := hostilePeer(t, h, declared, script)

			resp := h.authed(t, "GET", "/api/peers/"+fp+"/artwork/7/abc123", nil)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d", resp.StatusCode)
			}
			if got := resp.Header.Get("Content-Type"); got != "application/octet-stream" {
				t.Errorf("Content-Type = %q, want application/octet-stream", got)
			}
			assertHardened(t, resp)
		})
	}
}

func TestAPeersPosterStillArrivesAsAPoster(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	jpeg := []byte("\xff\xd8\xff\xe0 a poster")
	fp := hostilePeer(t, h, "image/jpeg", jpeg)

	resp := h.authed(t, "GET", "/api/peers/"+fp+"/artwork/7/abc123?size=w342", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", got)
	}
	assertHardened(t, resp)
	if got, _ := io.ReadAll(resp.Body); string(got) != string(jpeg) {
		t.Errorf("body = %q, want the poster's bytes", got)
	}
}

// Every relay route keeps the types it legitimately carries and refuses the
// rest, and every one of them carries both headers.
func TestEveryPeerRelayChoosesItsOwnType(t *testing.T) {
	cases := []struct {
		route, declared, want string
	}{
		{"/artwork/7/abc", "image/webp", "image/webp"},
		{"/artwork/7/abc", "image/png", "image/png"},
		{"/artwork/7/abc", "image/gif", "image/gif"},
		{"/hls/7/s1/seg0.m4s", "video/iso.segment", "video/iso.segment"},
		{"/hls/7/s1/init.mp4", "video/mp4", "video/mp4"},
		{"/hls/7/s1/seg0.ts", "video/mp2t", "video/mp2t"},
		{"/hls/7/s1/seg0.m4s", "text/html", "application/octet-stream"},
		{"/subtitles/7/s3", "text/vtt; charset=utf-8", "text/vtt; charset=utf-8"},
		{"/subtitles/7/s3", "text/html", "application/octet-stream"},
		{"/stream?item=7", "video/x-matroska", "video/x-matroska"},
		{"/stream?item=7", "audio/flac", "audio/flac"},
		{"/stream?item=7", "text/html", "application/octet-stream"},
		{"/transcode?item=7", "video/mp4", "video/mp4"},
		{"/transcode?item=7", "image/svg+xml", "application/octet-stream"},
		{"/playback?item=7", "application/json", "application/json"},
		{"/playback?item=7", "text/html", "application/octet-stream"},
		{"/subtitles?item=7", "application/json", "application/json"},
	}
	for _, c := range cases {
		t.Run(c.route+" "+c.declared, func(t *testing.T) {
			h := newHarness(t)
			h.secure(t, "a good long password")
			fp := hostilePeer(t, h, c.declared, []byte("x"))

			resp := h.authed(t, "GET", "/api/peers/"+fp+c.route, nil)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d", resp.StatusCode)
			}
			if got := resp.Header.Get("Content-Type"); got != c.want {
				t.Errorf("Content-Type = %q, want %q", got, c.want)
			}
			assertHardened(t, resp)
		})
	}
}

// The playlist always had its type set here; its body is still their text.
func TestAPeersPlaylistIsHardenedToo(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	fp := hostilePeer(t, h, "text/html", []byte("#EXTM3U\n"))

	resp := h.authed(t, "GET", "/api/peers/"+fp+"/hls/7/index.m3u8", nil)
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); got != "application/vnd.apple.mpegurl" {
		t.Errorf("Content-Type = %q", got)
	}
	assertHardened(t, resp)
}

func TestRelayContentType(t *testing.T) {
	cases := []struct {
		declared string
		allow    relayAllow
		want     string
	}{
		{"image/jpeg; foo=bar", relayArtwork, "image/jpeg"},
		{"image/svg+xml", relayArtwork, "application/octet-stream"},
		{"text/vtt", relayVTT, "text/vtt; charset=utf-8"},
		{"text/vtt; charset=iso-8859-1", relayVTT, "text/vtt; charset=utf-8"},
		{"video/webm", relayMedia, "video/webm"},
		{"videos/evil", relayMedia, "application/octet-stream"},
		{"", relayMedia, "application/octet-stream"},
	}
	for _, c := range cases {
		if got := relayContentType(c.declared, c.allow); got != c.want {
			t.Errorf("relayContentType(%q) = %q, want %q", c.declared, got, c.want)
		}
	}
}

// A live channel's provider is a third party too: its label is not forwarded.
func TestAChannelProvidersTypeIsNotForwarded(t *testing.T) {
	media := upstream(t, "<script>fetch('/api/keys',{method:'POST'})</script>", "text/html")
	list := upstream(t, "#EXTM3U\n#EXTINF:-1,Evil\n"+media.URL+"/live.ts\n", "application/x-mpegurl")
	h := newHarness(t)
	h.do(t, "POST", "/api/channel-sources",
		map[string]any{"name": "Provider", "url": list.URL}).Body.Close()
	var got struct {
		Channels []store.Channel `json:"channels"`
	}
	decode(t, h.do(t, "GET", "/api/channels", nil), &got)
	if len(got.Channels) != 1 {
		t.Fatalf("setup: %d channels", len(got.Channels))
	}

	resp := h.do(t, "GET", "/api/channels/"+itoa(got.Channels[0].ID)+"/stream", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", ct)
	}
	assertHardened(t, resp)
}
