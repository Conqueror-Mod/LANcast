package main

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestServerExeName(t *testing.T) {
	got := serverExeName()
	if runtime.GOOS == "windows" && got != "lancastd.exe" {
		t.Errorf("windows server exe = %q, want lancastd.exe", got)
	}
	if runtime.GOOS != "windows" && got != "lancastd" {
		t.Errorf("non-windows server exe = %q, want lancastd", got)
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
