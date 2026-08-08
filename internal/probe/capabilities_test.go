package probe

import (
	"slices"
	"testing"
)

// The case that prompted this: a browser that decodes HEVC natively was still
// served a full re-encode, because the floor cannot know what one machine's GPU
// can do (docs/client-capabilities-plan.md).
func TestHevcClaimTurnsAReEncodeIntoDirectPlay(t *testing.T) {
	file := &Result{
		Container: "mp4",
		Streams: []Stream{
			{Kind: KindVideo, Codec: "hevc", Index: 0},
			{Kind: KindAudio, Codec: "aac", Index: 1},
		},
	}

	base := Decide(file, BrowserProfile())
	if base.Method != Transcode {
		t.Fatalf("precondition: HEVC on the floor = %s, want transcode", base.Method)
	}

	claimed := Decide(file, WithCapabilities(BrowserProfile(), []string{"hevc"}))
	if claimed.Method != DirectPlay {
		t.Errorf("HEVC with the claim = %s (%s), want direct play",
			claimed.Method, claimed.Reason)
	}
}

// A claim only ever widens. A client that reports nonsense, or detects badly,
// must be no worse off than one that says nothing — that asymmetry is what
// makes trusting the parameter safe at all.
func TestCapabilitiesOnlyWiden(t *testing.T) {
	base := BrowserProfile()
	widened := WithCapabilities(base, []string{"hevc", "ac3", "matroska"})

	for _, c := range base.VideoCodecs {
		if !slices.Contains(widened.VideoCodecs, c) {
			t.Errorf("video codec %q was lost", c)
		}
	}
	for _, c := range base.AudioCodecs {
		if !slices.Contains(widened.AudioCodecs, c) {
			t.Errorf("audio codec %q was lost", c)
		}
	}
	for _, c := range base.Containers {
		if !slices.Contains(widened.Containers, c) {
			t.Errorf("container %q was lost", c)
		}
	}
}

// An unrecognised claim is ignored, not honoured and not fatal. An older server
// meeting a newer client should serve the file rather than refuse it, and a
// typo must not become a codec the server thinks it can copy.
func TestUnknownClaimsAreIgnored(t *testing.T) {
	base := BrowserProfile()
	got := WithCapabilities(base, []string{"hvec", "definitely-not-a-codec", ""})

	if len(got.VideoCodecs) != len(base.VideoCodecs) ||
		len(got.AudioCodecs) != len(base.AudioCodecs) ||
		len(got.Containers) != len(base.Containers) {
		t.Errorf("an unknown claim changed the profile: %+v", got)
	}
}

// The floor is untouched by widening a copy of it — the profile functions
// return fresh values, and a request that claims HEVC must not leave HEVC in
// the profile served to the next caller.
func TestWideningDoesNotMutateTheFloor(t *testing.T) {
	_ = WithCapabilities(BrowserProfile(), []string{"hevc"})

	if slices.Contains(BrowserProfile().VideoCodecs, "hevc") {
		t.Error("the browser floor gained HEVC — one client's claim leaked into everyone's")
	}
}

// Claiming what the profile already has changes nothing, so a client that
// reports its whole capability list does not produce a profile full of
// duplicates.
func TestClaimingSomethingAlreadySupportedIsANoOp(t *testing.T) {
	base := BrowserProfile()
	got := WithCapabilities(base, []string{"hevc", "hevc"})

	count := 0
	for _, c := range got.VideoCodecs {
		if c == "hevc" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("hevc appears %d times, want 1", count)
	}
}

// The codec claim brings its container with it. Allowing HEVC while still
// failing the container check would swap a full re-encode for a remux rather
// than for direct play — most of the win, quietly lost.
func TestHevcClaimAllowsItsUsualContainer(t *testing.T) {
	got := WithCapabilities(BrowserProfile(), []string{"hevc"})
	if !slices.Contains(got.Containers, "matroska") {
		t.Error("claiming hevc did not allow matroska; an .mkv would still remux")
	}
}

func TestParseCapabilities(t *testing.T) {
	cases := map[string][]string{
		"":               nil,
		"   ":            nil,
		"hevc":           {"hevc"},
		"hevc,ac3":       {"hevc", "ac3"},
		" hevc , ac3 ,,": {"hevc", "ac3"},
	}
	for in, want := range cases {
		got := ParseCapabilities(in)
		if len(got) != len(want) {
			t.Errorf("ParseCapabilities(%q) = %v, want %v", in, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("ParseCapabilities(%q) = %v, want %v", in, got, want)
				break
			}
		}
	}
}
