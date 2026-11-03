package main

import (
	"regexp"
	"testing"

	"lancast/internal/retro/libretro"
	"lancast/internal/retro/ra"
)

// The player's User-Agent names the core and its version as a core reports
// them, in a shape the server will send on (ADR 0112 §3).
func TestRAUserAgent(t *testing.T) {
	got := raUserAgent("0.10.2", libretro.SystemInfo{LibraryName: "Mupen64Plus-Next", LibraryVersion: "2.6-Vulkan 5d2ac21"}, "rcheevos/12.5")
	if !regexp.MustCompile(`^LANcast/0\.10\.2 \(Windows \d+\.\d+\) mupen64plus-next/2\.6-Vulkan-5d2ac21 rcheevos/12\.5$`).MatchString(got) {
		t.Fatalf("got %q", got)
	}
	if !ra.UserAgentOK(got) {
		t.Errorf("the server would not send %q", got)
	}
	if v := windowsVersion(); !regexp.MustCompile(`^\d+\.\d+$`).MatchString(v) || v == "6.2" {
		t.Errorf("windows version %q (6.2 is what GetVersionEx says without a manifest)", v)
	}
	// A core that names nothing still gives a token in each place.
	if got := raUserAgent("0.10.2", libretro.SystemInfo{LibraryName: "  ", LibraryVersion: "v1 (beta)"}, "rcheevos/12.5"); !ra.UserAgentOK(got) {
		t.Errorf("odd core: %q", got)
	}
}
