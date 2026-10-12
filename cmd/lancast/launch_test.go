package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// The server is this same executable (ADR 0080), started as `serve`: there is
// no second program beside it to find, so none can be missing.
func TestTheServerIsThisExecutable(t *testing.T) {
	got, err := serverExePath()
	if err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	if got != self {
		t.Errorf("server exe = %q, want this executable %q", got, self)
	}
}

// When a server is already answering, ensureServer is a no-op — it does not try
// to locate or spawn a binary, so a launcher pointed at a running service just
// opens the UI.
func TestEnsureServerNoOpWhenRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	l := &launcher{addr: strings.TrimPrefix(srv.URL, "http://")}
	if err := l.ensureServer(); err != nil {
		t.Fatalf("ensureServer with a live server: %v", err)
	}
	if l.started != nil {
		t.Error("ensureServer started a process even though the server was already up")
	}
}
