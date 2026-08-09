package api

import (
	"context"
	"net/http"

	"lancast/internal/release"
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

// updateView is the shape both endpoints return, and the one the activity
// surface reads.
func (s *Server) updateView() map[string]any {
	if s.updates == nil {
		return map[string]any{"supported": false}
	}
	st := s.updates.State()
	return map[string]any{
		"supported":  true,
		"current":    st.Current,
		"latest":     st.Latest,
		"available":  st.Available,
		"url":        st.URL,
		"checked_at": st.CheckedAt,
		"checking":   st.Checking,
		"error":      st.Error,
		// Whether this build can verify a release at all. False means automatic
		// installation is unavailable no matter what the setting says, and the
		// UI should say so rather than offering a button that cannot work
		// (ADR 0016 amendment).
		"can_verify": release.Signable(),
		// Enabled reports the setting, so the UI can distinguish "nothing to
		// report" from "not looking".
		"enabled": s.settings.Get().UpdateCheck,
	}
}

// updateActivity turns an available update into an activity row.
//
// An update is not work the server is doing, which is what /api/activity
// otherwise reports. It earns its place because the activity panel is where a
// person already looks to find out what their server wants from them, and an
// update waiting is exactly that — state:available rather than running, so a
// client renders it as something to act on rather than something in progress.
func updateActivity(st update.State) (Activity, bool) {
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
