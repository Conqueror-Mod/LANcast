package displaymode

import (
	"math"
	"strings"
	"testing"
)

const (
	ntscFilm  = 24000.0 / 1001 // 23.976…
	ntscVideo = 30000.0 / 1001 // 29.97…
)

// tv is a typical HDMI TV's mode list at 1080p and 4K: what a Roku TV offers.
var tv = []Mode{
	{3840, 2160, 32, 23}, {3840, 2160, 32, 24}, {3840, 2160, 32, 25}, {3840, 2160, 32, 29},
	{3840, 2160, 32, 30}, {3840, 2160, 32, 50}, {3840, 2160, 32, 59}, {3840, 2160, 32, 60},
	{1920, 1080, 32, 23}, {1920, 1080, 32, 24}, {1920, 1080, 32, 50}, {1920, 1080, 32, 60},
}

var tv4k60 = Mode{3840, 2160, 32, 60}

// One named test per film rate, asserting the mode *and* the reason, as
// probe's decision tests do.
func TestChooseNTSCFilmOnTV(t *testing.T) {
	d := Choose(ntscFilm, tv4k60, tv)
	want(t, d, true, Mode{3840, 2160, 32, 23}, "23.976 Hz is 1×")
}

func TestChooseTrueTwentyFourIsNotNTSC(t *testing.T) {
	// The pair the tolerance exists for: a 24.000 film must get 24, not 23.
	d := Choose(24, tv4k60, tv)
	want(t, d, true, Mode{3840, 2160, 32, 24}, "24 Hz is 1×")
}

func TestChoosePALFilmLeavesFiftyAlone(t *testing.T) {
	d := Choose(25, Mode{3840, 2160, 32, 50}, tv)
	want(t, d, false, Mode{}, "already at 50 Hz, 2×")
}

func TestChoosePALFilmFromSixty(t *testing.T) {
	d := Choose(25, tv4k60, tv)
	want(t, d, true, Mode{3840, 2160, 32, 25}, "25 Hz is 1×")
}

func TestChooseNTSCVideoLeavesFiftyNineAlone(t *testing.T) {
	d := Choose(ntscVideo, Mode{3840, 2160, 32, 59}, tv)
	want(t, d, false, Mode{}, "already at 59.940 Hz, 2×")
}

func TestChooseNTSCVideoOnTrueSixty(t *testing.T) {
	// 29.97 on a real 60 Hz mode drops a frame every 33 s: switch to 29.97.
	d := Choose(ntscVideo, tv4k60, tv)
	want(t, d, true, Mode{3840, 2160, 32, 29}, "29.970 Hz is 1×")
}

func TestChooseSixtyFrameVideoLeavesSixtyAlone(t *testing.T) {
	d := Choose(60, tv4k60, tv)
	want(t, d, false, Mode{}, "already at 60 Hz, 1×")
}

func TestChooseLowestMultipleWins(t *testing.T) {
	// A desktop monitor with 48 and 120 but no 24: 48 is the lower multiple.
	mon := []Mode{{2560, 1440, 32, 47}, {2560, 1440, 32, 60}, {2560, 1440, 32, 119}}
	d := Choose(ntscFilm, Mode{2560, 1440, 32, 60}, mon)
	want(t, d, true, Mode{2560, 1440, 32, 47}, "47.952 Hz is 2×")
}

func TestChooseNeverChangesResolution(t *testing.T) {
	// 23 Hz exists, but only at 1080p while the desktop is 4K.
	only1080 := []Mode{{1920, 1080, 32, 23}, {3840, 2160, 32, 60}}
	d := Choose(ntscFilm, tv4k60, only1080)
	want(t, d, false, Mode{}, "no mode at 3840x2160")
}

func TestChooseNothingOffered(t *testing.T) {
	d := Choose(ntscFilm, tv4k60, []Mode{tv4k60})
	want(t, d, false, Mode{}, "no mode at 3840x2160 is a multiple of 23.976")
}

func TestChooseIgnoresUnusableRates(t *testing.T) {
	for _, f := range []float64{0, 1, 1000, math.NaN(), -24} {
		d := Choose(f, tv4k60, tv)
		if d.Switch || !strings.Contains(d.Reason, "no usable film rate") {
			t.Errorf("film %v: got %+v", f, d)
		}
	}
}

func TestRate(t *testing.T) {
	cases := map[int]float64{23: 23.976, 24: 24, 29: 29.970, 59: 59.940, 60: 60, 119: 119.880}
	for h, w := range cases {
		if got := Rate(h); math.Abs(got-w) > 0.001 {
			t.Errorf("Rate(%d) = %.4f, want %.3f", h, got, w)
		}
	}
}

func want(t *testing.T, d Decision, sw bool, to Mode, reason string) {
	t.Helper()
	if d.Switch != sw || d.To != to {
		t.Fatalf("got switch=%v to=%v (%s), want switch=%v to=%v", d.Switch, d.To, d.Reason, sw, to)
	}
	if !strings.Contains(d.Reason, reason) {
		t.Fatalf("reason %q, want it to contain %q", d.Reason, reason)
	}
}
