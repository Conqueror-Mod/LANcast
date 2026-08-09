package api

import (
	"net/http"
	"testing"

	"lancast/internal/release"
	"lancast/internal/update"
)

// An available update appears in the activity panel, because that is where a
// person already looks to find out what their server wants from them.
func TestUpdateAppearsInActivity(t *testing.T) {
	tasks := buildActivity(snapshot{update: update.State{
		Current: "0.6.1", Latest: "v0.7.0", Available: true,
	}})
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1: %+v", len(tasks), tasks)
	}
	a := tasks[0]
	if a.Kind != "update" || a.State != "available" {
		t.Errorf("task = %+v; want kind=update state=available", a)
	}
	if a.Title != "LANcast v0.7.0 is available" {
		t.Errorf("title = %q", a.Title)
	}
	// Says what you are on, so the row answers "should I care" by itself.
	if a.Detail != "you are running 0.6.1" {
		t.Errorf("detail = %q", a.Detail)
	}
}

// No update, no row. An indicator that is always lit tells you nothing, which
// is the rule the whole activity surface follows.
func TestNoUpdateNoRow(t *testing.T) {
	for _, st := range []update.State{
		{Current: "0.6.1"},
		{Current: "0.6.1", Latest: "v0.6.1"},
		{Current: "0.6.1", Latest: "v0.7.0", Available: false},
	} {
		if tasks := buildActivity(snapshot{update: st}); len(tasks) != 0 {
			t.Errorf("state %+v produced %+v, want nothing", st, tasks)
		}
	}
}

// The status endpoint answers even before any check has run, so the UI has
// something to render on first load rather than an empty box.
func TestUpdateStatusBeforeAnyCheck(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "GET", "/api/update", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	decode(t, resp, &body)
	if body["supported"] != true {
		t.Errorf("supported = %v, want true", body["supported"])
	}
	if body["available"] != false {
		t.Errorf("available = %v before any check, want false", body["available"])
	}
	// Tracks the build rather than restating a constant. What matters is that
	// the API tells the UI the truth about whether this build can verify a
	// release at all — a build without a key must not offer an install button
	// that cannot work, and a build with one must not hide it.
	if body["can_verify"] != release.Signable() {
		t.Errorf("can_verify = %v, want %v to match this build",
			body["can_verify"], release.Signable())
	}
}
