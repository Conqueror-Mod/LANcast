//go:build windows

package vsync

import (
	"os"
	"sort"
	"testing"
	"time"
)

// A real monitor's blanks, measured. Skipped where there is no desktop to
// wait on (CI, a service's session 0), because that is not this code failing.
// LANCAST_VSYNC_DISPLAY names one ("\\.\DISPLAY3"); otherwise the first that opens.
func TestWaitMatchesARealMonitorsRefresh(t *testing.T) {
	names := []string{os.Getenv("LANCAST_VSYNC_DISPLAY")}
	if names[0] == "" {
		names = []string{`\\.\DISPLAY1`, `\\.\DISPLAY2`, `\\.\DISPLAY3`, `\\.\DISPLAY4`}
	}
	for _, name := range names {
		w, err := Open(name)
		if err != nil {
			continue
		}
		defer w.Close()
		var gaps []float64
		last := time.Now()
		for i := 0; i < 40; i++ {
			if err := w.Wait(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			now := time.Now()
			if i > 0 {
				gaps = append(gaps, float64(now.Sub(last))/float64(time.Millisecond))
			}
			last = now
		}
		sort.Float64s(gaps)
		med := gaps[len(gaps)/2]
		t.Logf("%s: median blank-to-blank %.2f ms (%.2f Hz), p10 %.2f, p90 %.2f", name, med, 1000/med,
			gaps[len(gaps)/10], gaps[len(gaps)*9/10])
		// Anything from 24 Hz to 360 Hz is a monitor; outside that the wait is not waiting.
		if med < 2.7 || med > 42 {
			t.Errorf("%s: median %.2f ms is no refresh rate", name, med)
		}
		return
	}
	t.Skip("no display to wait on here")
}

// A name that is no display is refused, not waited on for ever.
func TestOpenRefusesAMissingDisplay(t *testing.T) {
	if _, err := Open(`\\.\DISPLAY99`); err == nil {
		t.Error("opened a display that does not exist")
	}
}
