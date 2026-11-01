package mpv

import (
	"strings"
	"testing"
)

func TestHDRHint(t *testing.T) {
	if HDRHint(true) != "yes" || HDRHint(false) != "no" {
		t.Fatal("hint values are mpv's yes/no")
	}
}

// One case per answer to "why isn't my TV in HDR?", each with its own fix.
func TestVideoReportVerdict(t *testing.T) {
	hdr10 := VideoReport{FilmGamma: "pq", FilmPrimaries: "bt.2020"}
	sentHDR := hdr10
	sentHDR.TargetGamma, sentHDR.TargetPrimaries = "pq", "bt.2020"
	mapped := hdr10
	mapped.TargetGamma, mapped.TargetPrimaries = "srgb", "bt.709"
	sdr := VideoReport{FilmGamma: "bt.1886", FilmPrimaries: "bt.709", TargetGamma: "srgb"}
	hlg := VideoReport{FilmGamma: "hlg", TargetGamma: "hlg"}

	cases := []struct {
		name             string
		r                VideoReport
		hint, displayHDR bool
		want             string
	}{
		{"sdr film", sdr, true, true, "SDR film, shown as SDR"},
		{"hdr reaches the display", sentHDR, true, true, "sent to the display as HDR"},
		{"hlg counts as hdr", hlg, true, true, "sent to the display as HDR"},
		{"setting off", mapped, false, true, "Send HDR to the display is off"},
		{"windows hdr off", mapped, true, false, "Use HDR is off"},
		{"everything on, still mapped", mapped, true, true, "though the hint is on"},
	}
	for _, c := range cases {
		if got := c.r.Verdict(c.hint, c.displayHDR); !strings.Contains(got, c.want) {
			t.Errorf("%s: %q, want it to contain %q", c.name, got, c.want)
		}
	}
}
