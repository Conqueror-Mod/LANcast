package main

import (
	"reflect"
	"testing"

	"lancast/internal/clientwindow"
)

// Every field of the window's placement survives the prefs file. A field added
// to clientwindow.Placement and forgotten in either copy (the monitor's path
// was one) fails here: the round trip is compared whole, and every field is set.
func TestThePlacementRoundTripsThroughThePrefsFile(t *testing.T) {
	want := clientwindow.Placement{
		Monitor: `\.\DISPLAY8`, MonitorPath: `\?\DISPLAY#RKU0000#tv`,
		X: 12, Y: 34, Width: 1280, Height: 720, Maximized: true,
	}
	v := reflect.ValueOf(want)
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).IsZero() {
			t.Fatalf("fixture leaves %s at its zero value; set it so the round trip checks it", v.Type().Field(i).Name)
		}
	}
	if got := placementFromPrefs(placementToPrefs(want)); !reflect.DeepEqual(got, want) {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}
