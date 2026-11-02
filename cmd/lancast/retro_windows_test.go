//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/retro/cheevos"
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

// A console whose core draws through OpenGL says so, and since stage 3 that
// no longer stops it: with no core chosen it is unavailable for that reason,
// not for being N64.
func TestAvailabilityOfAGLConsole(t *testing.T) {
	r := &retroPlayer{}
	got := r.availability("n64")
	if got["needs_gl"] != true {
		t.Errorf("n64 = %v, want needs_gl", got)
	}
	if reason, _ := got["reason"].(string); got["available"] == false && strings.Contains(reason, "later release") {
		t.Errorf("n64 is still refused as a later release: %v", got)
	}
	if got := r.availability("dreamcast"); got["available"] != false {
		t.Errorf("an unknown console = %v", got)
	}
}

// What the page hears of an achievement (ADR 0112): only what was said, and
// ready always carries its counts, even when they are zero.
func TestAchievementEvent(t *testing.T) {
	got := achievementEvent(cheevos.Notice{Kind: "unlocked", ID: 5501, Title: "Ach1", Points: 5,
		Badge: "/api/retroachievements/media/Badge/00234.png"})
	if got["kind"] != "achievement" || got["event"] != "unlocked" || got["id"] != uint32(5501) ||
		got["points"] != uint32(5) || got["title"] != "Ach1" || got["badge"] == nil {
		t.Errorf("unlocked: %v", got)
	}
	if _, ok := got["description"]; ok {
		t.Errorf("an empty field was sent: %v", got)
	}
	ready := achievementEvent(cheevos.Notice{Kind: "ready", Title: "Sample"})
	if ready["total"] != uint32(0) || ready["unlocked"] != uint32(0) {
		t.Errorf("ready: %v", ready)
	}
}
