//go:build windows

package clientwindow

import "testing"

// callsPastTheLimit is more calls than Go has callbacks to give (about 2,000,
// never freed). A function that made one per call ends the test binary with
// "fatal error: too many callback functions" well before the loop finishes.
const callsPastTheLimit = 2500

/*
 * Monitors is polled every 2 s while a film plays (cmd/lancast watchLayout),
 * and it made a callback per call: 66 minutes of film killed the window. A
 * film is the only place it is called that often, so only a loop finds it.
 */
func TestMonitorsCanBeCalledForAsLongAsAFilmPlays(t *testing.T) {
	first := Monitors()
	for i := 0; i < callsPastTheLimit; i++ {
		if got := Monitors(); len(got) != len(first) {
			t.Fatalf("call %d saw %d monitors, the first saw %d", i, len(got), len(first))
		}
	}
}
