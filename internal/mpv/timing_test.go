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
	if got["video-sync"] != "display-resample" {
		t.Errorf("video-sync = %q, want display-resample", got["video-sync"])
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

func opts(os []Option) map[string]string {
	m := map[string]string{}
	for _, o := range os {
		m[o.Name] = o.Value
	}
	return m
}
