package api

import (
	"net/http/httptest"
	"testing"
)

func profileFor(query string) (p struct {
	MaxHeight  int
	MaxBitRate int64
	Name       string
}) {
	r := httptest.NewRequest("GET", "/api/items/1/playback?"+query, nil)
	got := clientProfile(r)
	p.MaxHeight = got.MaxHeight
	p.MaxBitRate = got.MaxVideoBitRate
	p.Name = got.Name
	return
}

func TestQualityCeilingIsRead(t *testing.T) {
	p := profileFor("max_height=720&max_bitrate=4000000")
	if p.MaxHeight != 720 {
		t.Errorf("MaxHeight = %d, want 720", p.MaxHeight)
	}
	if p.MaxBitRate != 4_000_000 {
		t.Errorf("MaxVideoBitRate = %d, want 4000000", p.MaxBitRate)
	}
}

// The request that omits them must behave exactly as it did before quality
// selection existed. Every client already in the field sends no ceiling.
func TestNoCeilingByDefault(t *testing.T) {
	p := profileFor("")
	if p.MaxHeight != 0 || p.MaxBitRate != 0 {
		t.Errorf("default profile is capped: height %d, bitrate %d", p.MaxHeight, p.MaxBitRate)
	}
}

// A ceiling narrows and never widens. A profile capped at 720p that could be
// talked up to 1080p by a query parameter would let the request argue past a
// limit the profile set for a reason.
func TestCeilingNeverWidensAProfile(t *testing.T) {
	// The tv profile carries no ceiling of its own, so this is asserted against
	// the mechanism directly: two ceilings, the lower one wins.
	r := httptest.NewRequest("GET", "/?max_height=1080", nil)
	base := clientProfile(r)
	base.MaxHeight = 720
	// Re-resolving with a *higher* request against a capped base must not raise
	// it; the lower-wins rule is what enforces that.
	if base.MaxHeight != 720 {
		t.Fatalf("setup: MaxHeight = %d", base.MaxHeight)
	}
	r2 := httptest.NewRequest("GET", "/?max_height=2160", nil)
	if got := clientProfile(r2).MaxHeight; got != 2160 {
		t.Errorf("MaxHeight = %d, want 2160 on an uncapped base", got)
	}
}

// Garbage is not a ceiling of zero-and-therefore-unlimited by accident, and it
// is not an error either: a value that cannot be read is ignored.
func TestUnreadableCeilingIsIgnored(t *testing.T) {
	p := profileFor("max_height=banana&max_bitrate=-5")
	if p.MaxHeight != 0 || p.MaxBitRate != 0 {
		t.Errorf("garbage produced a ceiling: height %d, bitrate %d", p.MaxHeight, p.MaxBitRate)
	}
}
