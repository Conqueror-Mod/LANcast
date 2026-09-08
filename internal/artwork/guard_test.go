package artwork

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/*
 * The guard is wired into the cache the server actually builds.
 *
 * internal/netguard proves the guard refuses private addresses. This proves
 * New() uses it — which is the half that can silently come undone, because the
 * other tests in this package replace c.http with the test server's own client
 * and would go on passing if the wiring were removed tomorrow.
 */
func TestNewBuildsAGuardedClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("internal"))
	}))
	defer srv.Close()

	// No client substitution: this is the cache the server runs with.
	c := New(t.TempDir())

	_, _, _, _, err := c.Download(context.Background(), srv.URL+"/poster.jpg")
	if err == nil {
		t.Fatal("the cache fetched a loopback URL — a provider that can name " +
			"a URL could have the host fetch an internal address and serve the " +
			"bytes back as a poster")
	}
	if !strings.Contains(err.Error(), "private and local addresses") {
		t.Errorf("err = %v, want the guard's refusal — failing for another "+
			"reason would leave this passing after the guard was removed", err)
	}
}
