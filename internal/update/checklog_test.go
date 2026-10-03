package update

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/*
 * The update checker says what changed, not what it repeated.
 *
 * A background check that failed was visible only in Settings, and only while
 * somebody looked. Now it is logged once when it starts failing, once when it
 * recovers, and once when an update appears.
 */
func TestUpdateCheckLogsChangesNotRepeats(t *testing.T) {
	failing := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing {
			http.Error(w, "nope", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9","html_url":"https://example.invalid/r"}`))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	c := New("0.9.47")
	c.client = srv.Client()
	c.SetLogger(slog.New(slog.NewTextHandler(&buf, nil)))
	count := func(s string) int { return strings.Count(buf.String(), s) }

	for range 3 {
		c.checkAt(context.Background(), srv.URL)
	}
	if n := count(`msg="update check failed"`); n != 1 {
		t.Fatalf("three failing checks logged %d failures, want 1:\n%s", n, buf.String())
	}

	failing = false
	c.checkAt(context.Background(), srv.URL)
	c.checkAt(context.Background(), srv.URL)
	if n := count(`msg="update check working again"`); n != 1 {
		t.Errorf("recovery logged %d times, want 1:\n%s", n, buf.String())
	}
	if n := count(`msg="update available"`); n != 1 {
		t.Errorf("an update appearing logged %d times over two checks, want 1:\n%s", n, buf.String())
	}
	if !strings.Contains(buf.String(), "latest=v9.9.9") {
		t.Errorf("the availability line does not say which version:\n%s", buf.String())
	}
}

// A checker with no logger stays silent, as tests and the zero value expect.
func TestUpdateCheckWithoutALogger(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer srv.Close()
	c := New("0.9.47")
	c.client = srv.Client()
	c.checkAt(context.Background(), srv.URL) // must not panic
}
