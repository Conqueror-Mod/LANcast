package api

import (
	"context"
	"net/http"

	"lancast/internal/release"
	"lancast/internal/selfupdate"
	"lancast/internal/update"
)

// updateStatus reports whether a newer release exists. Admin only: it names the
// running build and is the surface an install will eventually hang off, both of
// which are operator concerns rather than viewer ones.
func (s *Server) updateStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.updateView())
}

// checkForUpdate asks now rather than waiting for the daily check.
//
// The manual path works even when the automatic one is switched off. That is
// the point of it: someone who does not want LANcast talking to the internet on
// a timer may still want to ask, once, deliberately.
func (s *Server) checkForUpdate(w http.ResponseWriter, r *http.Request) {
	if s.updates == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"update checking is not available in this build")
		return
	}
	// Detached from the request: a check that outlives an impatient client is
	// better than one cancelled halfway and recorded as a failure.
	s.updates.Check(context.WithoutCancel(r.Context()))
	s.audit(r, "update.check", "", "", "Checked for updates", nil)
	writeJSON(w, http.StatusOK, s.updateView())
}

// downloadUpdate fetches, verifies and stages the newest release.
//
// Runs detached and returns immediately: a 15 MB download is not something to
// hold an HTTP request open for, and the activity panel is where its progress
// belongs anyway.
func (s *Server) downloadUpdate(w http.ResponseWriter, r *http.Request) {
	if s.updates == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "updates are not available in this build")
		return
	}
	if !release.Signable() {
		// Said as a refusal rather than a failure. This build cannot prove a
		// release is the project's, so it will not install one.
		writeError(w, http.StatusPreconditionFailed, "unverifiable",
			"this build cannot verify a release signature, so it will not install one")
		return
	}
	if p := s.updates.Progress(); p.Active {
		writeError(w, http.StatusConflict, "conflict", "an update is already downloading")
		return
	}

	s.audit(r, "update.download", "", "", "Started downloading an update", nil)
	go func() {
		if err := s.updates.DownloadAndStage(context.Background(), s.dataDir); err != nil {
			s.log.Error("update download failed", "error", err)
		}
	}()
	w.WriteHeader(http.StatusAccepted)
}

// updateView is the shape both endpoints return, and the one the activity
// surface reads.
func (s *Server) updateView() map[string]any {
	if s.updates == nil {
		return map[string]any{"supported": false}
	}
	st := s.updates.State()
	out := map[string]any{
		"supported":  true,
		"current":    st.Current,
		"latest":     st.Latest,
		"available":  st.Available,
		"url":        st.URL,
		"checked_at": st.CheckedAt,
		"checking":   st.Checking,
		"error":      st.Error,
		// The last failed download, distinct from a failed check. Reported
		// because a download runs detached from the request that starts it —
		// without this the UI has no way to learn the difference between a slow
		// download and one that died half an hour ago.
		"download_error": st.DownloadError,
		// Whether this build can verify a release at all. False means automatic
		// installation is unavailable no matter what the setting says, and the
		// UI should say so rather than offering a button that cannot work
		// (ADR 0016 amendment).
		"can_verify": release.Signable(),
		// Enabled reports the setting, so the UI can distinguish "nothing to
		// report" from "not looking".
		"enabled": s.settings.Get().UpdateCheck,
	}
	// A staged update is a different state from an available one, and the
	// difference is what the reader has to do about it: available means decide,
	// staged means restart. Reporting only "available" after staging would ask
	// someone to do something already done.
	if p := s.updates.Progress(); p.Active {
		out["downloading"] = p
	}
	if m, ok := selfupdate.Pending(s.dataDir); ok {
		out["staged"] = m.Version
		out["staged_at"] = m.StagedAt
	}
	return out
}

// updateActivity turns an available update into an activity row.
//
// An update is not work the server is doing, which is what /api/activity
// otherwise reports. It earns its place because the activity panel is where a
// person already looks to find out what their server wants from them, and an
// update waiting is exactly that — state:available rather than running, so a
// client renders it as something to act on rather than something in progress.
func updateActivity(st update.State, staged string, p update.Progress) (Activity, bool) {
	// A download in flight is genuinely work in progress, unlike the other two
	// states, so it reports as running with a progress pair the panel can draw.
	if p.Active {
		return Activity{
			Kind: "update", ID: "update:download", Title: p.Stage,
			State: "running", Done: int(p.Done), Total: int(p.Total),
		}, true
	}
	// Staged outranks available: the decision has been made and what remains is
	// a restart, so saying "available" here would be asking again.
	if staged != "" {
		return Activity{
			Kind:   "update",
			ID:     "update:staged:" + staged,
			Title:  "LANcast " + staged + " is ready",
			State:  "available",
			Detail: "restart the server to finish updating",
		}, true
	}
	if !st.Available || st.Latest == "" {
		return Activity{}, false
	}
	return Activity{
		Kind:   "update",
		ID:     "update:" + st.Latest,
		Title:  "LANcast " + st.Latest + " is available",
		State:  "available",
		Detail: "you are running " + st.Current,
	}, true
}
