package mpv

import (
	"math"
	"testing"
)

// The gain mpv applies for Volume(v) is v: the slider means amplitude, as the
// media element's volume does.
func TestVolumeMakesMPVsGainTheSlider(t *testing.T) {
	for _, v := range []float64{0.05, 0.2, 0.25, 0.5, 0.75, 1} {
		gain := math.Pow(Volume(v)/100, 3) // what mpv does with it
		if math.Abs(gain-v) > 1e-9 {
			t.Errorf("slider %.2f: mpv gain %.4f, want %.4f", v, gain, v)
		}
	}
	// A quarter of the way up is 12 dB down, not 36.
	if db := 20 * math.Log10(math.Pow(Volume(0.25)/100, 3)); math.Abs(db+12.04) > 0.01 {
		t.Errorf("slider 0.25 is %.2f dB, want -12.04", db)
	}
}

func TestVolumeClamps(t *testing.T) {
	for in, want := range map[float64]float64{-1: 0, 0: 0, 1: 100, 2: 100, math.NaN(): 0} {
		if got := Volume(in); got != want {
			t.Errorf("Volume(%v) = %v, want %v", in, got, want)
		}
	}
}
