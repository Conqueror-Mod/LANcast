package main

import "testing"

func TestUIURL(t *testing.T) {
	cases := map[string]string{
		":8080":            "http://localhost:8080",
		"0.0.0.0:8080":     "http://localhost:8080",
		"[::]:8080":        "http://localhost:8080",
		"192.168.1.5:9000": "http://192.168.1.5:9000",
		"localhost:3000":   "http://localhost:3000",
	}
	for addr, want := range cases {
		if got := uiURL(addr); got != want {
			t.Errorf("uiURL(%q) = %q, want %q", addr, got, want)
		}
	}
}

func TestBrowserCommand(t *testing.T) {
	url := "http://localhost:8080/x?a=1&b=2"
	if got := browserCommand("windows", url); got[0] != "rundll32" || got[len(got)-1] != url {
		t.Errorf("windows = %v", got)
	}
	if got := browserCommand("darwin", url); got[0] != "open" {
		t.Errorf("darwin = %v", got)
	}
	if got := browserCommand("linux", url); got[0] != "xdg-open" {
		t.Errorf("linux = %v", got)
	}
}
