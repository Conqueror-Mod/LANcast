package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

/*
 * The ceiling on concurrent conversions (docs/api.md, `max_transcodes`).
 *
 * The number itself is the operator's business; what this pins is that the two
 * answers which would break a server are refused. Zero would convert nothing
 * at all — the setting failing in its most destructive direction — and it is
 * also what an older settings file holds, which is why config treats it as
 * "unset" rather than as a choice.
 */
func TestTranscodeCeilingIsSettableAndBounded(t *testing.T) {
	h := newHarness(t)

	read := func() int {
		t.Helper()
		resp := h.do(t, "GET", "/api/settings", nil)
		defer resp.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		n, ok := body["max_transcodes"].(float64)
		if !ok {
			t.Fatalf("max_transcodes missing from the settings response: %v", body)
		}
		return int(n)
	}

	if got := read(); got != 3 {
		t.Errorf("default = %d, want 3 — what the server did before the setting existed", got)
	}

	resp := h.do(t, "PUT", "/api/settings", map[string]any{"max_transcodes": 8})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := read(); got != 8 {
		t.Errorf("after setting 8, read %d", got)
	}

	for _, bad := range []int{0, -1, 65} {
		resp := h.do(t, "PUT", "/api/settings", map[string]any{"max_transcodes": bad})
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("max_transcodes=%d: status = %d, want 400", bad, resp.StatusCode)
		}
		if got := read(); got != 8 {
			t.Errorf("a refused value changed the setting to %d", got)
		}
	}
}
