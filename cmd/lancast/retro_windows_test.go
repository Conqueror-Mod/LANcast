//go:build windows

package main

import (
	"path/filepath"
	"testing"
)

// The page can name only these slots; anything else is refused before it
// reaches the session.
func TestValidStateSlot(t *testing.T) {
	for _, s := range []string{"auto", "state-0", "state-9"} {
		if !validStateSlot(s) {
			t.Errorf("%s refused", s)
		}
	}
	for _, s := range []string{"", "sram", "state-10", "state-a", "../auto", "state-"} {
		if validStateSlot(s) {
			t.Errorf("%q accepted", s)
		}
	}
}

// A core is chosen by the full path of a DLL that exists, for a console
// LANcast knows. Every refusal here happens before anything is written, so
// this test never touches the real client directory.
func TestSetCoreOverrideRefuses(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope_libretro.dll")
	for _, c := range []struct{ platform, path string }{
		{"dreamcast", missing},
		{"gba", "mgba_libretro.dll"},                 // not a full path
		{"gba", filepath.Join(t.TempDir(), "x.exe")}, // not a DLL
		{"gba", missing},                             // not there
	} {
		if err := setCoreOverride(c.platform, c.path); err == nil {
			t.Errorf("%s -> %s accepted", c.platform, c.path)
		}
	}
}

// A console whose core draws through OpenGL says so, rather than offering a
// Play button that cannot work until stage 3.
func TestAvailabilityOfAGLConsole(t *testing.T) {
	r := &retroPlayer{}
	got := r.availability("n64")
	if got["available"] != false || got["needs_gl"] != true || got["reason"] == "" {
		t.Errorf("n64 = %v", got)
	}
	if got := r.availability("dreamcast"); got["available"] != false {
		t.Errorf("an unknown console = %v", got)
	}
}
