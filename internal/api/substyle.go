package api

import (
	"encoding/json"
	"net/http"

	"lancast/internal/store"
)

/*
 * The calling account's subtitle style (docs/settings-plan.md: per account).
 *
 * `style` is null for an account that has never chosen one, and a client then
 * keeps the style its own device already had — so turning this on changes
 * nothing for anybody until they pick something. A server with no accounts
 * answers null too, the way languagePreferences answers its defaults.
 */
func (s *Server) subtitleStyle(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"style": nil})
		return
	}
	st, err := s.st.SubtitleStyleFor(r.Context(), sess.UserID)
	if err != nil {
		s.writeInternal(w, err, "read subtitle style")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"style": st})
}

func (s *Server) setSubtitleStyle(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusConflict, "no_account",
			"this server has no accounts yet; create one first")
		return
	}
	var req store.SubtitleStyle
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := s.st.SetSubtitleStyle(r.Context(), sess.UserID, req); err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "no such account")
			return
		}
		s.writeInternal(w, err, "save subtitle style")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"style": req})
}
