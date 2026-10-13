package api

import (
	"net/http"

	"lancast/internal/tasks"
)

/*
 * Status › Scheduled tasks (docs/settings-plan.md, step 3): every background
 * job, what sets it off, when it last ran and how, and Run now.
 *
 * Administrators only. Running a job spends the machine — a scan reads every
 * drive, credit detection reads a quarter of every film — which is the same
 * reason scanning is admin-only already.
 */
func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	list := []tasks.Task{}
	if s.tasks != nil {
		list = s.tasks.List()
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": list})
}

// runTask starts a job and answers at once: 202, because the job runs in the
// background and reports to the board, which the list then shows. A job
// already running is not started twice — each one coalesces its own requests,
// so asking again while it runs is harmless.
func (s *Server) runTask(w http.ResponseWriter, r *http.Request) {
	if s.tasks == nil || !s.tasks.RunNow(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "not_found", "no such task")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
