package api

import (
	"net/http"
	"testing"
	"time"

	"lancast/internal/tasks"
)

// Status › Scheduled tasks: an administrator sees every job and can run one;
// a member can do neither, because running a job spends the machine.
func TestScheduledTasksListAndRun(t *testing.T) {
	h := newHarness(t)
	ran := make(chan struct{}, 1)
	board := tasks.New()
	board.Add(tasks.Spec{
		ID: "probe", Name: "Read media files", Trigger: tasks.AfterScan,
		Run: func() { ran <- struct{}{} },
	})
	board.Add(tasks.Spec{
		ID: "prune", Name: "Maintenance", Trigger: tasks.Interval,
		Every: func() time.Duration { return 24 * time.Hour },
	})
	h.srvAPI.tasks = board

	var body struct {
		Tasks []struct {
			ID         string  `json:"id"`
			Trigger    string  `json:"trigger"`
			EveryHours float64 `json:"every_hours"`
		} `json:"tasks"`
	}
	decode(t, h.do(t, "GET", "/api/tasks", nil), &body)
	if len(body.Tasks) != 2 || body.Tasks[0].ID != "probe" || body.Tasks[1].EveryHours != 24 {
		t.Fatalf("tasks = %+v", body.Tasks)
	}

	resp := h.do(t, "POST", "/api/tasks/probe/run", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("run status = %d, want 202", resp.StatusCode)
	}
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("Run now did not run the job")
	}

	wantError(t, h.do(t, "POST", "/api/tasks/nothing/run", nil), 404, "not_found")
}

func TestAMemberCannotSeeOrRunScheduledTasks(t *testing.T) {
	h := newHarness(t)
	h.srvAPI.tasks = tasks.New()
	member := h.addMember(t, "viewer", "correct horse battery")
	for _, req := range [][2]string{{"GET", "/api/tasks"}, {"POST", "/api/tasks/scan/run"}} {
		resp := h.doAs(t, member, req[0], req[1], nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s as a member = %d, want 403", req[0], req[1], resp.StatusCode)
		}
	}
}
