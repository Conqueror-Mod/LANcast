package api

import (
	"testing"
	"time"

	"lancast/internal/probe"
)

/*
 * The wait before a copied session's playlist goes out growing.
 *
 * Each case below is a film that behaved differently, and the numbers are the
 * point: a wait that ends while ffmpeg is still writing segments hands the
 * client a growing playlist, which the desktop engine reads as a live stream
 * and joins at the edge.
 */
func TestRemuxPatience(t *testing.T) {
	remux := probe.Decision{Method: "remux", VideoAction: "copy", AudioAction: "copy"}
	audioEncode := probe.Decision{Method: "remux", VideoAction: "copy", AudioAction: "encode"}

	cases := []struct {
		name     string
		decision probe.Decision
		seconds  float64
		want     time.Duration
	}{
		{
			// Scream (2022), 6.1GB: 1,095 of 1,124 segments inside twenty
			// seconds and finished in about twenty-one. The floor covers this
			// three times over, and nothing about a pure copy scales with the
			// film's length at the rate this wait cares about.
			name: "a pure remux keeps the floor", decision: remux,
			seconds: 6850, want: remuxCapFloor,
		},
		{
			// Jay and Silent Bob Reboot, 1h45 of H.264 + DTS: still writing
			// segments at sixty seconds, served growing, froze within thirty.
			name: "an audio encode gets the film's own length", decision: audioEncode,
			seconds: 6300, want: 210 * time.Second,
		},
		{
			// A half-hour episode's audio encode is quick; the floor is longer
			// than the work and stays.
			name: "a short item keeps the floor", decision: audioEncode,
			seconds: 1350, want: remuxCapFloor,
		},
		{
			// Lawrence of Arabia territory. Past the ceiling the honest answer
			// is that this is not a quick start.
			name: "a very long film stops at the ceiling", decision: audioEncode,
			seconds: 20000, want: remuxCapCeiling,
		},
		{
			// Nothing known about the film: the floor, rather than a number
			// derived from zero.
			name: "an unknown duration keeps the floor", decision: audioEncode,
			seconds: 0, want: remuxCapFloor,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := remuxPatienceFor(tc.decision, tc.seconds)
			if got.Cap != tc.want {
				t.Errorf("cap = %v, want %v", got.Cap, tc.want)
			}
			if got.Stall != remuxStall {
				t.Errorf("stall = %v, want %v — progress is the rule, whatever the cap is",
					got.Stall, remuxStall)
			}
		})
	}
}

// The fault in one line: the film that froze must be waited for longer than
// the wait that failed it.
func TestTheFilmThatFrozeWouldNowBeWaitedFor(t *testing.T) {
	p := remuxPatienceFor(probe.Decision{VideoAction: "copy", AudioAction: "encode"}, 6300)
	if p.Cap <= 60*time.Second {
		t.Fatalf("cap = %v; sixty seconds is what served it growing", p.Cap)
	}
}
