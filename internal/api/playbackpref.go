package api

import (
	"encoding/json"
	"net/http"

	"lancast/internal/store"
)

/*
 * The caller's own playback preferences (ADR 0083): for now, whether an intro
 * skips itself.
 *
 * Per account and acting on the session, never a named user, for the reason
 * the language preferences give: it is a taste, not a limit somebody else sets.
 * Not audited either — which way somebody's own intros go is not something
 * anybody looks for in an audit trail.
 */

// playbackPreferences returns the caller's own.
func (s *Server) playbackPreferences(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		// No accounts yet: no choice has been made, so the default stands.
		writeJSON(w, http.StatusOK, store.PlaybackPrefs{SkipIntros: store.DefaultSkipIntros})
		return
	}
	p, err := s.st.PlaybackPrefsFor(r.Context(), sess.UserID)
	if err != nil {
		s.writeInternal(w, err, "read playback preferences")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// setPlaybackPreferences records the caller's own.
func (s *Server) setPlaybackPreferences(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusConflict, "no_account",
			"this server has no accounts yet; create one first")
		return
	}
	// Pointers, so a field left out is left alone rather than read as "off":
	// a client that sent nothing about it has not chosen anything. At least
	// one is required.
	var req struct {
		SkipIntros  *bool `json:"skip_intros"`
		SkipPreroll *bool `json:"skip_preroll"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.SkipIntros == nil && req.SkipPreroll == nil) {
		writeError(w, http.StatusBadRequest, "bad_request", "skip_intros or skip_preroll is required")
		return
	}
	p, err := s.st.PlaybackPrefsFor(r.Context(), sess.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "no such account")
			return
		}
		s.writeInternal(w, err, "read playback preferences")
		return
	}
	if req.SkipIntros != nil {
		p.SkipIntros = *req.SkipIntros
	}
	if req.SkipPreroll != nil {
		p.SkipPreroll = *req.SkipPreroll
	}
	if err := s.st.SetPlaybackPrefs(r.Context(), sess.UserID, p); err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "no such account")
			return
		}
		s.writeInternal(w, err, "set playback preferences")
		return
	}
	writeJSON(w, http.StatusOK, p)
}
