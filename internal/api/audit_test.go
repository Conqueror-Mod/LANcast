package api

import (
	"net/http"
	"strings"
	"testing"
)

type auditBody struct {
	Events []struct {
		Action     string `json:"action"`
		ActorName  string `json:"actor_name"`
		Summary    string `json:"summary"`
		TargetKind string `json:"target_kind"`
		TargetID   string `json:"target_id"`
		Detail     string `json:"detail"`
	} `json:"events"`
	Total   int      `json:"total"`
	Actions []string `json:"actions"`
}

func readAudit(t *testing.T, h *harness, query string) auditBody {
	t.Helper()
	var body auditBody
	decode(t, h.do(t, "GET", "/api/audit"+query, nil), &body)
	return body
}

// An empty log is an empty array, not null.
func TestAuditEmpty(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "GET", "/api/audit", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body auditBody
	decode(t, resp, &body)
	if body.Events == nil {
		t.Error("events is null; want an empty array")
	}
	if body.Total != 0 {
		t.Errorf("total = %d, want 0", body.Total)
	}
}

// Creating and removing a library is the exact question that had no answer
// during v0.4.x testing: what emptied this library.
func TestAuditRecordsLibraryLifecycle(t *testing.T) {
	h := newHarness(t)

	resp := h.do(t, "POST", "/api/libraries", map[string]string{
		"name": "Films", "kind": "movie", "path": h.dir,
	})
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = h.do(t, "DELETE", "/api/libraries/"+itoa(h.lib.ID), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", resp.StatusCode)
	}

	body := readAudit(t, h, "")
	var del *string
	for i := range body.Events {
		if body.Events[i].Action == "library.delete" {
			del = &body.Events[i].Summary
			if body.Events[i].TargetKind != "library" {
				t.Errorf("target_kind = %q, want library", body.Events[i].TargetKind)
			}
		}
	}
	if del == nil {
		t.Fatalf("no library.delete event; got %+v", body.Events)
	}
	// The summary must still name the library after it is gone — that is the
	// whole reason it is resolved at write time (ADR 0026).
	if !strings.Contains(*del, "Media") {
		t.Errorf("summary = %q; it must name the deleted library", *del)
	}
	if !strings.Contains(*del, "files left on disk") {
		t.Errorf("summary = %q; it must say the media survived", *del)
	}
}

// Removal is two very different acts and the log must not blur them: ignore is
// reversible, delete destroyed files.
func TestAuditDistinguishesIgnoreFromDelete(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "Arrival.mkv", []byte("x"))

	resp := h.do(t, "DELETE", "/api/items/"+itoa(id)+"?mode=ignore", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	body := readAudit(t, h, "?action=item.delete")
	if body.Total != 1 {
		t.Fatalf("total = %d, want 1 (%+v)", body.Total, body.Events)
	}
	e := body.Events[0]
	if !strings.Contains(e.Summary, "Stopped tracking") {
		t.Errorf("summary = %q; an ignore must not read as a deletion", e.Summary)
	}
	if !strings.Contains(e.Detail, `"mode":"ignore"`) {
		t.Errorf("detail = %q; it must carry the mode", e.Detail)
	}
}

// Reads are not audit events. Auditing them would bury the handful of
// deliberate acts that matter under the normal operation of a media server.
func TestAuditIgnoresReads(t *testing.T) {
	h := newHarness(t)
	h.addFile(t, "Arrival.mkv", []byte("x"))

	for _, p := range []string{
		"/api/libraries", "/api/items?library=" + itoa(h.lib.ID),
		"/api/review", "/api/activity", "/api/health",
	} {
		h.do(t, "GET", p, nil).Body.Close()
	}

	body := readAudit(t, h, "")
	if body.Total != 0 {
		t.Errorf("reads produced %d audit events: %+v", body.Total, body.Events)
	}
}

// The actions list is built from what actually happened, so a client filter
// cannot drift from the handlers.
func TestAuditActionsComeFromTheData(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "Arrival.mkv", []byte("x"))
	h.do(t, "DELETE", "/api/items/"+itoa(id)+"?mode=ignore", nil).Body.Close()

	body := readAudit(t, h, "")
	if len(body.Actions) != 1 || body.Actions[0] != "item.delete" {
		t.Errorf("actions = %v, want exactly the one that happened", body.Actions)
	}
}

func TestAuditRejectsBadPaging(t *testing.T) {
	h := newHarness(t)
	for _, q := range []string{"?limit=0", "?limit=-1", "?limit=many", "?offset=-1", "?offset=x"} {
		wantError(t, h.do(t, "GET", "/api/audit"+q, nil), http.StatusBadRequest, "bad_request")
	}
}

// An unattributed act is still recorded. The unconfigured loopback owner is a
// real actor and deserves an honest name rather than a blank.
func TestAuditNamesTheLoopbackOwner(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "Arrival.mkv", []byte("x"))
	h.do(t, "DELETE", "/api/items/"+itoa(id)+"?mode=ignore", nil).Body.Close()

	body := readAudit(t, h, "")
	if body.Total == 0 {
		t.Fatal("no events recorded")
	}
	if body.Events[0].ActorName == "" {
		t.Error("actor_name is empty; every event must be attributable to something")
	}
}
