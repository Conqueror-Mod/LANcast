package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"lancast/internal/config"
	"lancast/internal/webhook"
)

// hookReceiver is a stand-in for Home Assistant: it records what it is sent.
type hookReceiver struct {
	mu  sync.Mutex
	got []webhook.Payload
	sig []string
	raw [][]byte
	srv *httptest.Server
}

func newHookReceiver(t *testing.T) *hookReceiver {
	r := &hookReceiver{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		var p webhook.Payload
		json.Unmarshal(b, &p)
		r.mu.Lock()
		r.got = append(r.got, p)
		r.sig = append(r.sig, req.Header.Get("X-LANcast-Signature"))
		r.raw = append(r.raw, b)
		r.mu.Unlock()
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *hookReceiver) events() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, p := range r.got {
		out = append(out, p.Type)
	}
	return out
}

func (r *hookReceiver) waitFor(t *testing.T, event string) webhook.Payload {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		for _, p := range r.got {
			if p.Type == event {
				r.mu.Unlock()
				return p
			}
		}
		r.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %s arrived; got %v", event, r.events())
	return webhook.Payload{}
}

// withWebhooks gives the harness a running dispatcher wired as serverapp does.
func withWebhooks(t *testing.T, h *harness) {
	t.Helper()
	d := webhook.New(func() []config.Webhook { return h.settings.Get().Webhooks }, "test",
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.Run(ctx)
	h.srvAPI.webhooks = d
	h.srvAPI.presence.OnChange(h.srvAPI.playbackChanged)
	h.srvAPI.scanner.OnResult(h.srvAPI.scanFinished)
}

func addHook(t *testing.T, h *harness, url string, events ...string) (id, secret string) {
	t.Helper()
	resp := h.authed(t, "POST", "/api/webhooks", map[string]any{"url": url, "events": events})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("POST /api/webhooks = %d %s", resp.StatusCode, b)
	}
	var body struct {
		Webhook webhookView `json:"webhook"`
		Secret  string      `json:"secret"`
	}
	decode(t, resp, &body)
	if body.Secret == "" || body.Webhook.ID == "" || !body.Webhook.Enabled {
		t.Fatalf("created %+v", body)
	}
	return body.Webhook.ID, body.Secret
}

func TestWebhooksAreAdministratorOnly(t *testing.T) {
	a := newAccessHarness(t)
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/webhooks"},
		{"POST", "/api/webhooks"},
		{"PATCH", "/api/webhooks/wh_x"},
		{"DELETE", "/api/webhooks/wh_x"},
		{"POST", "/api/webhooks/wh_x/test"},
	} {
		resp := a.doAs(t, a.member, c.method, c.path, map[string]any{"url": "http://x/", "events": []string{"scan.finished"}})
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("member %s %s = %d, want 403", c.method, c.path, resp.StatusCode)
		}
	}
	// And an administrator's API key is not an administrator (ADR 0061).
	secret, _ := mint(t, a.harness, "bot")
	resp := a.doKey(t, "GET", "/api/webhooks", secret, nil)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("an API key listed webhooks")
	}
}

func TestAWebhookIsMadeListedChangedAndRemoved(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")
	id, secret := addHook(t, h, "http://192.168.1.10:8123/api/webhook/abc", "scan.finished")

	var list struct {
		Webhooks []webhookView `json:"webhooks"`
		Events   []string      `json:"events"`
	}
	resp := h.authed(t, "GET", "/api/webhooks", nil)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(raw), secret) {
		t.Fatal("the list returned the secret")
	}
	json.Unmarshal(raw, &list)
	if len(list.Webhooks) != 1 || list.Webhooks[0].ID != id || len(list.Events) != len(webhook.Events) {
		t.Fatalf("list = %+v", list)
	}

	resp = h.authed(t, "PATCH", "/api/webhooks/"+id, map[string]any{"enabled": false, "events": []string{"backup.taken"}})
	var changed webhookView
	decode(t, resp, &changed)
	if changed.Enabled || len(changed.Events) != 1 || changed.Events[0] != "backup.taken" {
		t.Fatalf("patched = %+v", changed)
	}

	for _, bad := range []map[string]any{
		{"url": "ftp://x/"},
		{"events": []string{}},
		{"events": []string{"ping"}},
	} {
		r := h.authed(t, "PATCH", "/api/webhooks/"+id, bad)
		r.Body.Close()
		if r.StatusCode != http.StatusBadRequest {
			t.Errorf("%v: %d, want 400", bad, r.StatusCode)
		}
	}

	del := h.authed(t, "DELETE", "/api/webhooks/"+id, nil)
	del.Body.Close()
	if del.StatusCode != http.StatusNoContent || len(h.settings.Get().Webhooks) != 0 {
		t.Fatalf("delete = %d, left %d", del.StatusCode, len(h.settings.Get().Webhooks))
	}
	missing := h.authed(t, "DELETE", "/api/webhooks/"+id, nil)
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("second delete = %d", missing.StatusCode)
	}
}

func TestTestSendsASignedPing(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")
	withWebhooks(t, h)
	rcv := newHookReceiver(t)
	id, secret := addHook(t, h, rcv.srv.URL, "scan.finished")

	var st webhook.Status
	decode(t, h.authed(t, "POST", "/api/webhooks/"+id+"/test", nil), &st)
	if !st.OK || st.Event != webhook.Ping {
		t.Fatalf("test = %+v", st)
	}
	if rcv.sig[0] != webhook.Sign(secret, rcv.raw[0]) {
		t.Fatal("the ping is not signed with the secret the server returned")
	}
}

func TestABackupIsReported(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")
	withWebhooks(t, h)
	rcv := newHookReceiver(t)
	addHook(t, h, rcv.srv.URL, "backup.taken")

	resp := h.authed(t, "POST", "/api/backups", nil)
	resp.Body.Close()
	p := rcv.waitFor(t, webhook.BackupTaken)
	data := p.Data.(map[string]any)
	if data["taken_by"] != "person" || !strings.HasPrefix(data["name"].(string), "lancast-backup-") {
		t.Fatalf("payload = %+v", data)
	}
}

/*
 * The consent rule. Playback is reported only for an account that turned it
 * on itself; the administrator who made the webhook gets nothing for anyone
 * else, and nothing for themself until they say so.
 */
func TestPlaybackIsReportedOnlyForAccountsThatAgreed(t *testing.T) {
	a := newAccessHarness(t)
	withWebhooks(t, a.harness)
	rcv := newHookReceiver(t)
	addHook(t, a.harness, rcv.srv.URL, "playback.started", "playback.stopped", "backup.taken")

	kid := a.memberID(t)
	a.srvAPI.presence.WatchingHere(kid, "Paddington")
	a.srvAPI.presence.Stopped(kid)

	// A marker event, so "nothing arrived" is a fact rather than impatience.
	resp := a.authed(t, "POST", "/api/backups", nil)
	resp.Body.Close()
	rcv.waitFor(t, webhook.BackupTaken)
	for _, e := range rcv.events() {
		if strings.HasPrefix(e, "playback.") {
			t.Fatalf("playback reported without consent: %v", rcv.events())
		}
	}

	// The account says yes, on its own session. There is no admin route.
	on := a.doAs(t, a.member, "PUT", "/api/profile/webhooks", map[string]any{"share": true})
	on.Body.Close()
	if on.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/profile/webhooks = %d", on.StatusCode)
	}
	a.srvAPI.presence.WatchingHere(kid, "Paddington")
	started := rcv.waitFor(t, webhook.PlaybackStarted)
	data := started.Data.(map[string]any)
	if data["title"] != "Paddington" || data["account"].(map[string]any)["name"] != "kid" {
		t.Fatalf("payload = %+v", data)
	}
	a.srvAPI.presence.Stopped(kid)
	rcv.waitFor(t, webhook.PlaybackStopped)

	// Auth status reports the account's own switch back.
	var status struct {
		User struct {
			WebhookPlayback bool `json:"webhook_playback"`
		} `json:"user"`
	}
	decode(t, a.doAs(t, a.member, "GET", "/api/auth/status", nil), &status)
	if !status.User.WebhookPlayback {
		t.Fatal("auth status does not report the switch")
	}
}

func TestAtMostTenWebhooks(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")
	for i := 0; i < webhook.MaxHooks; i++ {
		addHook(t, h, "http://example.invalid/", "scan.finished")
	}
	resp := h.authed(t, "POST", "/api/webhooks", map[string]any{"url": "http://example.invalid/", "events": []string{"scan.finished"}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("eleventh = %d, want 409", resp.StatusCode)
	}
}

func TestAFinishedScanIsReported(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")
	withWebhooks(t, h)
	rcv := newHookReceiver(t)
	addHook(t, h, rcv.srv.URL, "scan.finished")

	resp := h.authed(t, "POST", "/api/libraries/scan", nil)
	resp.Body.Close()
	p := rcv.waitFor(t, webhook.ScanFinished)
	data := p.Data.(map[string]any)
	lib := data["library"].(map[string]any)
	if data["outcome"] != "completed" || lib["name"] != "Media" {
		t.Fatalf("payload = %+v", data)
	}
	if _, ok := data["duration_ms"]; !ok {
		t.Fatal("no duration")
	}
}
