package api

import (
	"net/http"
	"testing"
)

/*
 * Turning marker detection on has to start it.
 *
 * Reported from a real install: the setting was on, and hours later not one
 * item had been examined. The pass was kicked at startup and after a scan, and
 * flipping the switch on a running server matched neither — so it waited for
 * the nightly scan. A switch whose effect waits on something unrelated cannot
 * be told apart from a broken one.
 */

func TestTurningDetectionOnStartsAPass(t *testing.T) {
	h := newHarness(t)
	kicked := 0
	h.srvAPI.detectMarkers = func() { kicked++ }

	resp := h.do(t, "PUT", "/api/settings", map[string]any{"detect_markers": true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if kicked != 1 {
		t.Errorf("started %d passes, want 1 — the switch must start the work", kicked)
	}
}

// Saving while it is already on must not start another: otherwise every
// settings save queues a full decode of the library.
func TestSavingWhileDetectionIsOnStartsNothingMore(t *testing.T) {
	h := newHarness(t)
	h.srvAPI.detectMarkers = func() {}
	resp := h.do(t, "PUT", "/api/settings", map[string]any{"detect_markers": true})
	resp.Body.Close()

	kicked := 0
	h.srvAPI.detectMarkers = func() { kicked++ }
	resp = h.do(t, "PUT", "/api/settings",
		map[string]any{"detect_markers": true, "auto_enrich": false})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if kicked != 0 {
		t.Errorf("started %d passes, want 0 — it was already on", kicked)
	}
}

// Turning it off starts nothing, and never discards what was already found.
func TestTurningDetectionOffStartsNothing(t *testing.T) {
	h := newHarness(t)
	h.srvAPI.detectMarkers = func() {}
	resp := h.do(t, "PUT", "/api/settings", map[string]any{"detect_markers": true})
	resp.Body.Close()

	kicked := 0
	h.srvAPI.detectMarkers = func() { kicked++ }
	resp = h.do(t, "PUT", "/api/settings", map[string]any{"detect_markers": false})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if kicked != 0 {
		t.Errorf("started %d passes, want 0", kicked)
	}
}
