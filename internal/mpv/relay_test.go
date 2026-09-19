package mpv

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A server that behaves like /api/stream/{id} under a ticket.
func fakeServer(t *testing.T, seen *http.Header) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = r.Header.Clone()
		if r.URL.Path != "/api/stream/42" || r.Header.Get("Authorization") != "Ticket secret-ticket" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		http.ServeContent(w, r, "f.mkv", timeZero, strings.NewReader("0123456789"))
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(srv.Certificate().RawSubjectPublicKeyInfo)
	return srv, base64.StdEncoding.EncodeToString(sum[:])
}

func TestRelayStreamsThroughThePinWithTheTicket(t *testing.T) {
	var seen http.Header
	srv, pin := fakeServer(t, &seen)
	r, err := NewRelay(srv.URL, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	u, err := r.Register(42, "secret-ticket")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(u, "http://127.0.0.1:") {
		t.Errorf("url = %s, want loopback", u)
	}
	if strings.Contains(u, "secret-ticket") {
		t.Fatal("the ticket is in mpv's URL, and so in mpv's log")
	}

	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Range", "bytes=4-6")
	req.Header.Set("Cookie", "should-not-travel=1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusPartialContent || string(body) != "456" {
		t.Fatalf("status = %d body = %q, want 206 \"456\"", resp.StatusCode, body)
	}
	if resp.Header.Get("Content-Range") != "bytes 4-6/10" {
		t.Errorf("Content-Range = %q", resp.Header.Get("Content-Range"))
	}
	if seen.Get("Cookie") != "" {
		t.Error("a header from mpv other than Range reached the server")
	}
}

func TestRelayRefusesAServerWithAnotherKey(t *testing.T) {
	var seen http.Header
	srv, _ := fakeServer(t, &seen)
	wrong := base64.StdEncoding.EncodeToString(make([]byte, 32))
	r, err := NewRelay(srv.URL, wrong)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	u, _ := r.Register(42, "secret-ticket")
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 for a key that does not match the pin", resp.StatusCode)
	}
	if seen != nil {
		t.Error("the request reached a server whose key did not match")
	}
}

func TestRelayUnknownPathAndForget(t *testing.T) {
	var seen http.Header
	srv, pin := fakeServer(t, &seen)
	r, _ := NewRelay(srv.URL, pin)
	defer r.Close()
	u, _ := r.Register(42, "secret-ticket")

	guess := u[:strings.LastIndex(u, "/")+1] + "guessed"
	if resp, _ := http.Get(guess); resp.StatusCode != http.StatusNotFound {
		t.Errorf("guessed key = %d, want 404", resp.StatusCode)
	}
	r.Forget()
	if resp, _ := http.Get(u); resp.StatusCode != http.StatusNotFound {
		t.Errorf("after Forget = %d, want 404", resp.StatusCode)
	}
}

func TestRelayRefusesUnpinnedTLSAndMixedConfig(t *testing.T) {
	if _, err := NewRelay("https://10.0.0.1:8443", ""); err == nil {
		t.Error("https with no pin was accepted")
	}
	if _, err := NewRelay("http://127.0.0.1:8080", "abc"); err == nil {
		t.Error("a pin for plain http was accepted")
	}
	if _, err := NewRelay("https://10.0.0.1:8443/api", "abc"); err == nil {
		t.Error("an origin with a path was accepted")
	}
}

var timeZero time.Time
