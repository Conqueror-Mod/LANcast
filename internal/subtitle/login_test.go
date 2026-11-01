package subtitle

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoginSetsTheToken(t *testing.T) {
	var auth string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			if r.Method != http.MethodPost || r.Header.Get("Api-Key") != "k" {
				t.Errorf("login: %s with key %q", r.Method, r.Header.Get("Api-Key"))
			}
			_, _ = w.Write([]byte(`{"token":"tok","base_url":"vip-api.opensubtitles.com"}`))
		case "/download":
			auth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"link":"http://example.test/x.srt"}`))
		}
	}))
	defer fake.Close()

	c := NewOpenSubtitles("k")
	c.SetBaseURL(fake.URL)
	if _, err := c.Login(context.Background(), "u", "p"); err != nil {
		t.Fatal(err)
	}
	if c.AccountHost() != "vip-api.opensubtitles.com" {
		t.Errorf("AccountHost = %q", c.AccountHost())
	}
	// Pointed at a test server, the client stays there.
	if _, _, err := c.DownloadLink(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer tok" {
		t.Errorf("Authorization = %q, want the login token", auth)
	}
}

func TestLoginRejected(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer fake.Close()
	c := NewOpenSubtitles("k")
	c.SetBaseURL(fake.URL)
	if _, err := c.Login(context.Background(), "u", "wrong"); !errors.Is(err, ErrLoginRejected) {
		t.Errorf("err = %v, want ErrLoginRejected", err)
	}
}

// The host a login answers with moves the client only when it is an
// opensubtitles.com host name: anything else would send the key elsewhere.
func TestAccountHostIsOnlyAnOpenSubtitlesHost(t *testing.T) {
	cases := map[string]bool{
		"vip-api.opensubtitles.com":        true,
		"api.opensubtitles.com":            true,
		"evil.example":                     false,
		"opensubtitles.com.evil.example":   false,
		"evil.example/x.opensubtitles.com": false,
		"user@vip-api.opensubtitles.com":   false,
		"vip-api.opensubtitles.com:8443":   false,
		"":                                 false,
	}
	for host, follow := range cases {
		c := NewOpenSubtitles("k")
		c.SetAccountHost(host)
		want := osBaseURL
		if follow {
			want = "https://" + host + "/api/v1"
		}
		if c.base != want {
			t.Errorf("host %q: base = %q, want %q", host, c.base, want)
		}
	}
}
