package api

import (
	"net/http"
	"strings"
	"testing"
)

// Settings › About describes the running build from what it carries: the
// notice, and this version's notes when they were committed.
func TestAboutDescribesTheRunningBuild(t *testing.T) {
	orig := Version
	defer func() { Version = orig }()

	Version = "0.9.79"
	h := newHarness(t)
	resp := h.do(t, "GET", "/api/about", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	decode(t, resp, &body)
	if body["version"] != "0.9.79" || body["licence"] != "AGPL-3.0-only" {
		t.Fatalf("about = %v", body)
	}
	if n, _ := body["notice"].(string); !strings.Contains(n, "Third-party components") {
		t.Fatal("about carries no notice")
	}
	if n, _ := body["notes"].(string); !strings.Contains(n, "LANcast.exe") {
		t.Fatalf("about carries no notes for 0.9.79: %q", n)
	}

	// A build with nothing committed for it says nothing, rather than
	// borrowing another version's notes.
	Version = "dev"
	var dev map[string]any
	decode(t, h.do(t, "GET", "/api/about", nil), &dev)
	if _, ok := dev["notes"]; ok {
		t.Fatalf("a dev build reported notes: %v", dev["notes"])
	}
}
