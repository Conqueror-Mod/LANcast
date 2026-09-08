package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/*
 * The runtime's own fetcher is guarded.
 *
 * The manifest allowlist matches a hostname, and a hostname is not a
 * destination: whoever owns `api.example.com` owns what it resolves to and may
 * point it at 127.0.0.1 after the grant was agreed. The allowlist would still
 * match the string.
 *
 * Every other test in this package injects a fetcher with WithHTTPGetter, so
 * none of them would notice if this wiring were dropped.
 */
func TestTheDefaultFetcherRefusesLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("the server itself"))
	}))
	defer srv.Close()

	_, err := defaultHTTPGet(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("a plugin fetch reached loopback; an allowlisted hostname " +
			"resolving to 127.0.0.1 would reach the server itself")
	}
	if !strings.Contains(err.Error(), "private and local addresses") {
		t.Errorf("err = %v, want the guard's refusal", err)
	}
}
