package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"lancast/internal/config"
	"lancast/internal/subtitle"
)

func withBuiltinKeys(t *testing.T, tmdb, osub string) {
	t.Helper()
	prevT, prevO := config.BuiltinTMDBKey, config.BuiltinOpenSubtitlesKey
	config.BuiltinTMDBKey, config.BuiltinOpenSubtitlesKey = tmdb, osub
	t.Cleanup(func() { config.BuiltinTMDBKey, config.BuiltinOpenSubtitlesKey = prevT, prevO })
}

type keyView struct {
	Configured bool   `json:"configured"`
	Source     string `json:"source"`
	BuiltIn    bool   `json:"built_in"`
	Username   string `json:"username"`
}

func getKeyViews(t *testing.T, h *harness) (tmdb, osub keyView, off bool, raw string) {
	t.Helper()
	resp := h.do(t, "GET", "/api/settings", nil)
	defer resp.Body.Close()
	var buf strings.Builder
	var doc struct {
		TMDB           keyView `json:"tmdb"`
		OpenSubtitles  keyView `json:"opensubtitles"`
		BuiltinKeysOff bool    `json:"builtin_keys_off"`
	}
	if err := json.NewDecoder(io.TeeReader(resp.Body, &buf)).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	return doc.TMDB, doc.OpenSubtitles, doc.BuiltinKeysOff, buf.String()
}

// Settings says which key each provider is using, and the switch turns the
// built-in ones off without touching the person's own (ADR 0110).
func TestSettingsReportWhichKeyIsInUse(t *testing.T) {
	withBuiltinKeys(t, "shipped-tmdb", "shipped-osub")
	h := newHarness(t)

	tm, os, off, _ := getKeyViews(t, h)
	if tm.Source != config.KeySourceBuiltIn || !tm.BuiltIn || tm.Configured {
		t.Errorf("tmdb = %+v, want the built-in key in use and none of the person's own", tm)
	}
	if os.Source != config.KeySourceBuiltIn || off {
		t.Errorf("opensubtitles = %+v, off = %v, want the built-in key in use", os, off)
	}

	h.putSettings(t, map[string]any{"builtin_keys_off": true})
	tm, os, off, _ = getKeyViews(t, h)
	if !off || tm.Source != config.KeySourceNone || os.Source != config.KeySourceNone {
		t.Errorf("after switching off: tmdb %q, opensubtitles %q, off %v; want none, none, true", tm.Source, os.Source, off)
	}

	h.putSettings(t, map[string]any{"tmdb_key": "mine"})
	tm, _, _, _ = getKeyViews(t, h)
	if tm.Source != config.KeySourceOwn || !tm.Configured {
		t.Errorf("tmdb = %+v, want the person's own key in use with the switch off", tm)
	}
}

// The login's username is shown, its password never is.
func TestOpenSubtitlesLoginIsWriteOnly(t *testing.T) {
	h := newHarness(t)
	h.putSettings(t, map[string]any{
		"opensubtitles_username": "somebody",
		"opensubtitles_password": "hunter2-not-real",
	})
	_, os, _, raw := getKeyViews(t, h)
	if os.Username != "somebody" {
		t.Errorf("username = %q, want it reported", os.Username)
	}
	if strings.Contains(raw, "hunter2-not-real") {
		t.Fatal("GET /api/settings echoed the OpenSubtitles password")
	}

	// Clearing the name clears the password with it.
	h.putSettings(t, map[string]any{"opensubtitles_username": ""})
	if got := h.settings.Get(); got.OpenSubtitlesUser != "" || got.OpenSubtitlesPassword != "" {
		t.Errorf("after clearing the username: user %q, password set %v; want both empty",
			got.OpenSubtitlesUser, got.OpenSubtitlesPassword != "")
	}
}

func TestOpenSubtitlesLoginNeedsBoth(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "PUT", "/api/settings", map[string]any{"opensubtitles_username": "somebody"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a username alone: status %d, want 400", resp.StatusCode)
	}
	if got := h.settings.Get().OpenSubtitlesUser; got != "" {
		t.Errorf("a refused save stored the username %q", got)
	}
}

// One login serves many downloads: OpenSubtitles limits logins, and the token
// lasts a day.
func TestOpenSubtitlesLoginIsCached(t *testing.T) {
	var logins atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			logins.Add(1)
			_, _ = w.Write([]byte(`{"token":"tok-1","base_url":"api.opensubtitles.com"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer fake.Close()

	h := newHarness(t)
	h.putSettings(t, map[string]any{
		"opensubtitles_key":      "k",
		"opensubtitles_username": "somebody",
		"opensubtitles_password": "pw",
	})
	for i := 0; i < 3; i++ {
		c := subtitle.NewOpenSubtitles("k")
		c.SetBaseURL(fake.URL)
		if err := h.srvAPI.attachLogin(context.Background(), c, "k"); err != nil {
			t.Fatal(err)
		}
	}
	if n := logins.Load(); n != 1 {
		t.Errorf("logged in %d times for three downloads, want once", n)
	}

	// A changed password is a fresh login, not the old account's token.
	h.putSettings(t, map[string]any{"opensubtitles_password": "pw2"})
	c := subtitle.NewOpenSubtitles("k")
	c.SetBaseURL(fake.URL)
	if err := h.srvAPI.attachLogin(context.Background(), c, "k"); err != nil {
		t.Fatal(err)
	}
	if n := logins.Load(); n != 2 {
		t.Errorf("logins after a password change = %d, want 2", n)
	}
}

// A wrong password is not retried on every download.
func TestOpenSubtitlesRejectedLoginCoolsDown(t *testing.T) {
	var logins atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logins.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer fake.Close()

	h := newHarness(t)
	h.putSettings(t, map[string]any{
		"opensubtitles_key":      "k",
		"opensubtitles_username": "somebody",
		"opensubtitles_password": "wrong",
	})
	for i := 0; i < 3; i++ {
		c := subtitle.NewOpenSubtitles("k")
		c.SetBaseURL(fake.URL)
		if err := h.srvAPI.attachLogin(context.Background(), c, "k"); err == nil {
			t.Fatal("a rejected login reported success")
		}
	}
	if n := logins.Load(); n != 1 {
		t.Errorf("tried a rejected login %d times, want once", n)
	}
}

// With no key entered, subtitle search uses the one this release carries.
func TestSubtitleSearchUsesTheBuiltInKey(t *testing.T) {
	withBuiltinKeys(t, "", "shipped-osub")
	h := newHarness(t)
	c := h.srvAPI.subtitleClient()
	if c == nil {
		t.Fatal("no subtitle client with a built-in key and none entered")
	}

	var gotKey string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("Api-Key")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer fake.Close()
	c.SetBaseURL(fake.URL)
	if _, err := c.Search(context.Background(), subtitle.SearchQuery{Query: "x"}); err != nil {
		t.Fatal(err)
	}
	if gotKey != "shipped-osub" {
		t.Errorf("Api-Key = %q, want the built-in key", gotKey)
	}

	h.putSettings(t, map[string]any{"builtin_keys_off": true})
	if h.srvAPI.subtitleClient() != nil {
		t.Error("a subtitle client exists with the built-in keys switched off and none entered")
	}
}
