package api

import (
	"net/http"
	"testing"

	"lancast/internal/coverart"
	"lancast/internal/enrich"
	"lancast/internal/probe"
	"lancast/internal/scan"
	"lancast/internal/transcode"
)

type activityBody struct {
	Active bool       `json:"active"`
	Tasks  []Activity `json:"tasks"`
}

// An idle server says so, and says it with an empty array rather than null —
// a client should not have to defend against two shapes for "nothing".
func TestActivityIdle(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "GET", "/api/activity", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body activityBody
	decode(t, resp, &body)
	if body.Active {
		t.Error("active = true on an idle server")
	}
	if body.Tasks == nil {
		t.Error("tasks is null; want an empty array")
	}
	if len(body.Tasks) != 0 {
		t.Errorf("tasks = %+v, want none", body.Tasks)
	}
}

// A scan that has finished is not activity. Only running and failed are, and a
// failure stays visible because the point of an activity view is that a failure
// has somewhere to appear.
func TestActivityScanStates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state scan.State
		want  bool
	}{
		{"running", scan.StateRunning, true},
		{"failed", scan.StateFailed, true},
		{"idle", scan.StateIdle, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tasks := buildActivity(snapshot{scans: []scanSnapshot{{
				name: "Films",
				progress: scan.Progress{
					LibraryID: 7, State: tc.state, FilesSeen: 120,
					ItemsChanged: 4, StartedAt: 1000, Error: "boom",
				},
			}}})
			if got := len(tasks) == 1; got != tc.want {
				t.Fatalf("listed = %v, want %v (tasks %+v)", got, tc.want, tasks)
			}
			if !tc.want {
				return
			}
			a := tasks[0]
			if a.ID != "scan:7" || a.LibraryID != 7 {
				t.Errorf("id = %q, library = %d", a.ID, a.LibraryID)
			}
			// The library name is resolved server-side; a client showing this
			// row must not have to look it up.
			if a.Title != "Scanning Films" {
				t.Errorf("title = %q", a.Title)
			}
			if a.State != string(tc.state) {
				t.Errorf("state = %q, want %q", a.State, tc.state)
			}
			// A scan knows how many files it has seen and never how many it
			// will see, so total stays 0 and the client renders indeterminate.
			if a.Done != 120 || a.Total != 0 {
				t.Errorf("done/total = %d/%d, want 120/0", a.Done, a.Total)
			}
			if a.Detail != "4 changed" {
				t.Errorf("detail = %q", a.Detail)
			}
		})
	}
}

// Each background worker contributes a row only while it is running, and the
// rows share one shape whatever the worker.
func TestActivityWorkers(t *testing.T) {
	snap := snapshot{
		enrich: enrich.Stats{Running: true, Enriched: 10, Failed: 2, Total: 50},
		probe:  probe.Stats{Running: true, Probed: 5, Total: 9},
		covers: coverart.Stats{Running: false, Found: 3, Total: 4},
	}
	tasks := buildActivity(snap)
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2 (%+v)", len(tasks), tasks)
	}
	if tasks[0].Kind != "enrich" || tasks[0].Done != 10 || tasks[0].Total != 50 {
		t.Errorf("enrich row = %+v", tasks[0])
	}
	if tasks[0].Detail != "2 failed" {
		t.Errorf("detail = %q, want %q", tasks[0].Detail, "2 failed")
	}
	if tasks[1].Kind != "probe" || tasks[1].Done != 5 || tasks[1].Total != 9 {
		t.Errorf("probe row = %+v", tasks[1])
	}
	// A clean run says nothing about failures rather than "0 failed".
	if tasks[1].Detail != "" {
		t.Errorf("detail = %q, want empty", tasks[1].Detail)
	}
}

// A finished transcode session is not activity; a live one is. Sessions linger
// in the manager until reaped, so filtering here is what stops the panel
// claiming work that ended.
func TestActivitySkipsFinishedTranscodes(t *testing.T) {
	tasks := buildActivity(snapshot{sessions: []transcode.SessionInfo{
		{ID: "a", Output: "hls", Finished: true},
		{ID: "b", Output: "hls"},
	}})
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1 (%+v)", len(tasks), tasks)
	}
	if tasks[0].ID != "transcode:b" || tasks[0].Kind != "transcode" {
		t.Errorf("task = %+v", tasks[0])
	}
}
