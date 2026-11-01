package main

import (
	"testing"

	"lancast/internal/clientwindow"
)

var (
	deskMon = clientwindow.Monitor{Device: `\\.\DISPLAY1`, Name: "C27F398", Work: clientwindow.Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1040}}
	sideMon = clientwindow.Monitor{Device: `\\.\DISPLAY2`, Name: "Display", Work: clientwindow.Rect{Left: 1920, Top: 0, Right: 4080, Bottom: 1400}}
	tvMon   = clientwindow.Monitor{Device: `\\.\DISPLAY3`, Name: "Roku 55R4AX", Work: clientwindow.Rect{Left: -3840, Top: 0, Right: 0, Bottom: 2112}}
)

// The theater PC's desk: three screens means another clock, so no trial.
func TestResampleTrialOnlyWithTheTVAlone(t *testing.T) {
	if resampleWorthTrying([]clientwindow.Monitor{deskMon, sideMon, tvMon}) {
		t.Error("three screens: another monitor sets the pace, the trial cannot win")
	}
	if resampleWorthTrying([]clientwindow.Monitor{sideMon, tvMon}) {
		t.Error("two screens: still another clock (White Chicks measured display 2's 59.7 Hz)")
	}
	if !resampleWorthTrying([]clientwindow.Monitor{tvMon}) {
		t.Error("the TV alone: the trial is worth running")
	}
}

func TestLayoutKeyIgnoresOrderButNotChanges(t *testing.T) {
	a := layoutKey([]clientwindow.Monitor{deskMon, sideMon, tvMon})
	if b := layoutKey([]clientwindow.Monitor{tvMon, deskMon, sideMon}); a != b {
		t.Fatalf("enumeration order changed the key:\n%s\n%s", a, b)
	}
	gone := layoutKey([]clientwindow.Monitor{sideMon, tvMon})
	if gone == a {
		t.Error("a disconnected monitor left the key unchanged")
	}
	rotated := sideMon
	rotated.Work = clientwindow.Rect{Left: 1920, Top: 0, Right: 3360, Bottom: 2160}
	if layoutKey([]clientwindow.Monitor{deskMon, rotated, tvMon}) == a {
		t.Error("a rotated monitor left the key unchanged")
	}
}

func TestMonitorList(t *testing.T) {
	noName := clientwindow.Monitor{Device: `\\.\DISPLAY9`}
	if got := monitorList([]clientwindow.Monitor{deskMon, noName}); got != `C27F398, \\.\DISPLAY9` {
		t.Errorf("got %q", got)
	}
}
