package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

/*
 * The face endpoints, on a server with no face worker installed — which is the
 * ordinary case and the one most likely to be got wrong.
 *
 * The whole risk here is a client that cannot tell "nobody in your photographs"
 * from "nothing has looked at your photographs". Those are the same empty array
 * and completely different sentences, and this project has repeatedly paid for
 * showing the second when the first was true.
 */

func decodeMap(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

// Capabilities answers even when nothing is installed, and says why.
func TestFaceCapabilitiesAlwaysAnswers(t *testing.T) {
	h := newHarness(t)

	resp := h.do(t, "GET", "/api/faces/capabilities", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 — a missing worker is not an error",
			resp.StatusCode)
	}
	m := decodeMap(t, resp)
	if ready, _ := m["ready"].(bool); ready {
		t.Error("reported ready with no worker installed")
	}
	if reason, _ := m["reason"].(string); reason == "" {
		t.Error("reported not ready without saying why; a client cannot " +
			"explain a blank people page from that")
	}
}

/*
 * Starting a pass without a worker is refused, and refused with a reason.
 *
 * The alternative — accepting and quietly doing nothing — would leave somebody
 * waiting for a progress bar that never appears.
 */
func TestStartingAFacePassWithoutAWorkerIsRefused(t *testing.T) {
	h := newHarness(t)

	resp := h.do(t, "POST", "/api/libraries/"+itoa(h.lib.ID)+"/faces", nil)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		t.Fatal("a face pass was accepted with no worker installed")
	}
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 400 or 409", resp.StatusCode)
	}
}

/*
 * The people list is available on a library that has never been examined, and
 * carries `pending` so a client can say which of the two empty states it is in.
 */
func TestPeopleReportsPendingSoEmptyCanBeExplained(t *testing.T) {
	h := newHarness(t)

	resp := h.do(t, "GET", "/api/libraries/"+itoa(h.lib.ID)+"/people", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	m := decodeMap(t, resp)
	if _, ok := m["people"]; !ok {
		t.Error("no people key at all")
	}
	if _, ok := m["pending"]; !ok {
		t.Error("no pending count — an empty list is then unexplainable")
	}
}

// Naming a group that does not exist is a 404 rather than a silent success,
// so a client that has drifted out of date finds out.
func TestNamingAGroupThatIsNotThere(t *testing.T) {
	h := newHarness(t)

	resp := h.do(t, "PUT", "/api/faces/clusters/98765",
		map[string]any{"name": "Nobody"})
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("naming a group that does not exist reported success")
	}
}

// A name is required rather than defaulted: a request with no name at all is a
// client bug, and clearing a name has to be deliberate (an explicit "").
func TestNamingWithoutANameIsRefused(t *testing.T) {
	h := newHarness(t)

	resp := h.do(t, "PUT", "/api/faces/clusters/1", map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
