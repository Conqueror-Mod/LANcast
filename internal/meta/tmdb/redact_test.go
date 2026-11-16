package tmdb

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"lancast/internal/meta"
)

// failingTransport fails every request the way a dropped network does, so
// http.Client.Do returns the *url.Error that carries the request URL.
type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection reset")
}

// R08-6: a network failure must not carry the API key into the error, which
// the server logs and Report an issue can attach.
func TestNetworkErrorDoesNotCarryTheKey(t *testing.T) {
	c := New("SECRET-TMDB-KEY",
		WithBaseURL("https://api.example.invalid/3"),
		WithHTTPClient(&http.Client{Transport: failingTransport{}}),
		WithLimiter(meta.NewLimiter(1000, 1000)))
	c.MaxRetries = 0

	_, err := c.Fetch(context.Background(), meta.Ref{Kind: meta.KindMovie, ExternalID: "603"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "SECRET-TMDB-KEY") || strings.Contains(err.Error(), "api_key") {
		t.Errorf("the key reached the error: %q", err)
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("the cause was lost: %q", err)
	}
}

// R08-8: an endless 200 is refused at the cap rather than read until timeout.
func TestOversizedResponseIsRefused(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		chunk := make([]byte, 1<<20)
		for i := 0; i < 9; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	})
	c := newClient(t, srv)
	c.MaxRetries = 0
	_, err := c.Fetch(context.Background(), meta.Ref{Kind: meta.KindMovie, ExternalID: "603"})
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("a 9 MiB answer was not refused at the cap: %v", err)
	}
}
