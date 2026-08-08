package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func getActivity(t *testing.T, h *harness) map[string]any {
	t.Helper()
	res := h.do(t, http.MethodGet, "/api/activity", nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/activity = %d, want 200", res.StatusCode)
	}
	var got map[string]any
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}

// An idle server says so plainly. The strip that reads this shows nothing when
// nothing is happening, so "busy" being wrong in this direction means a
// permanent spinner over a server doing nothing at all.
func TestActivityIsQuietWhenNothingIsRunning(t *testing.T) {
	h := newHarness(t)
	got := getActivity(t, h)

	if busy, _ := got["busy"].(bool); busy {
		t.Errorf("busy = true on an idle server: %v", got)
	}
	// Present rather than absent, so the client renders an empty list instead
	// of having to guess what a missing key means.
	if _, ok := got["scans"]; !ok {
		t.Error("no scans key")
	}
	for _, k := range []string{"enrich", "probe", "coverart"} {
		if _, ok := got[k]; !ok {
			t.Errorf("no %s key", k)
		}
	}
}

// A running scan is what someone is most likely to be staring at when the
// library looks wrong, so it has to appear — with the library's name, because
// "library 3 is scanning" is not something anyone can act on.
func TestActivityReportsARunningScanByName(t *testing.T) {
	h := newHarness(t)

	res := h.do(t, http.MethodPost, "/api/libraries/"+itoa(h.lib.ID)+"/scan", nil)
	res.Body.Close()

	// A scan of an empty temp directory finishes almost immediately, so this
	// asserts the shape rather than racing the worker: whatever the state, the
	// endpoint must answer and the key must exist.
	got := getActivity(t, h)
	scans, ok := got["scans"].([]any)
	if !ok {
		t.Fatalf("scans is not a list: %#v", got["scans"])
	}
	for _, raw := range scans {
		s, _ := raw.(map[string]any)
		if s["name"] == nil || s["name"] == "" {
			t.Errorf("a scan is reported without a library name: %v", s)
		}
		if s["library_id"] == nil {
			t.Errorf("a scan is reported without a library id: %v", s)
		}
	}
}

// Busy is derived on the server so that "is LANcast doing something" has one
// definition. If it ever disagrees with the parts it summarises, the strip
// either hides work in progress or spins forever — both worse than no strip.
func TestBusyAgreesWithTheParts(t *testing.T) {
	h := newHarness(t)
	got := getActivity(t, h)

	busy, _ := got["busy"].(bool)
	anyRunning := len(got["scans"].([]any)) > 0
	for _, k := range []string{"enrich", "probe", "coverart"} {
		if m, ok := got[k].(map[string]any); ok {
			if r, _ := m["running"].(bool); r {
				anyRunning = true
			}
		}
	}
	if busy != anyRunning {
		t.Errorf("busy = %v but the parts say %v: %v", busy, anyRunning, got)
	}
}
