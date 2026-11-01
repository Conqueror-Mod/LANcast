package mpv

import (
	"math"
	"testing"
)

func TestDisplayTimingTellsMPVTheMeasuredRate(t *testing.T) {
	got := opts(DisplayTiming(24000.0 / 1001))
	if got["display-fps-override"] != "23.976024" {
		t.Errorf("override = %q, want the measured 23.976024", got["display-fps-override"])
	}
	// Telling mpv the rate is not the same as pacing to it: that waits for
	// PacedByDisplay.
	if got["video-sync"] != "audio" {
		t.Errorf("video-sync = %q, want audio until pacing is measured", got["video-sync"])
	}
}

// Unknown or off puts mpv back to its own defaults rather than leaving the
// last film's rate on: a stale override is the bug this exists to fix.
func TestDisplayTimingUnknownIsMPVsDefaults(t *testing.T) {
	for _, hz := range []float64{0, -1, math.NaN()} {
		got := opts(DisplayTiming(hz))
		if got["display-fps-override"] != "0" || got["video-sync"] != "audio" {
			t.Errorf("hz %v: %v, want mpv's defaults", hz, got)
		}
	}
}

func TestResampleIsDisplayResample(t *testing.T) {
	if opts(Resample)["video-sync"] != "display-resample" {
		t.Fatal("Resample must pace to the display")
	}
}

// The cases measured on the theater PC, plus the edges.
func TestPacedByDisplay(t *testing.T) {
	const tv = 24000.0 / 1001
	cases := []struct {
		name              string
		display, measured float64
		want              bool
	}{
		{"TV is the only display (smooth)", tv, 23.977, true},
		{"desk monitor sets the clock (stutter)", tv, 71.91, false},
		{"near miss: 24 against 23.976 is not the same clock", 24, 23.86, false},
		{"not measured yet", tv, 0, false},
		{"display unknown", 0, 23.976, false},
		{"NaN", tv, math.NaN(), false},
	}
	for _, c := range cases {
		if got := PacedByDisplay(c.display, c.measured); got != c.want {
			t.Errorf("%s: PacedByDisplay(%v, %v) = %v, want %v", c.name, c.display, c.measured, got, c.want)
		}
	}
}

func opts(os []Option) map[string]string {
	m := map[string]string{}
	for _, o := range os {
		m[o.Name] = o.Value
	}
	return m
}
