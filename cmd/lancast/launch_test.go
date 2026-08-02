package main

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

// The launcher finds the server by name next to itself, so this name has to
// match what the release archives and the installer lay down.
func TestServerExeName(t *testing.T) {
	got := serverExeName()
	if runtime.GOOS == "windows" && got != "LANcast-Server.exe" {
		t.Errorf("windows server exe = %q, want LANcast-Server.exe", got)
	}
	if runtime.GOOS != "windows" && got != "LANcast-Server" {
		t.Errorf("non-windows server exe = %q, want LANcast-Server", got)
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
