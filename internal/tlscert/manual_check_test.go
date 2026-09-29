package tlscert

import (
	"os"
	"testing"
)

// A hand-run look at this machine's real addresses, so the classification can
// be seen rather than trusted. Skipped unless asked for: it asserts nothing
// about a machine whose addresses nobody here chose.
func TestShowThisMachinesAddresses(t *testing.T) {
	if os.Getenv("LANCAST_SHOW_IPS") == "" {
		t.Skip("set LANCAST_SHOW_IPS=1 to see this machine's addresses")
	}
	keep := map[string]bool{}
	for _, ip := range StableIPs() {
		keep[ip] = true
	}
	for _, ip := range LocalIPs() {
		state := "kept"
		if !keep[ip] {
			state = "DROPPED"
		}
		t.Logf("%-44s %s", ip, state)
	}
}
