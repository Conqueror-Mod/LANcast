package api

import (
	"encoding/json"
	"net/http"

	"lancast/internal/store"
)

/*
 * What an account wants to hear, and when it wants subtitles.
 *
 * Its own route rather than another field on PATCH /api/profile, which renames
 * an account: those two are unrelated acts, and a single handler taking both
 * would make a rename that also silently cleared a language preference one
 * malformed request away.
 *
 * Acts on the *session*, never on a named user. This is a taste rather than a
 * limit — the opposite of the content-rating ceiling in the same table, which
 * an administrator sets for somebody else and which that person cannot lift. A
 * route that let one account choose another's audio language would be a
 * different feature with a different justification, and there is no reason to
 * want it.
 */

// languagePreferences returns the caller's own preferences.
func (s *Server) languagePreferences(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		// The unconfigured loopback state has no account. Answering with the
		// empty preference is honest and lets the page render: no account means
		// no preference, which is exactly how the server behaves.
		writeJSON(w, http.StatusOK, map[string]any{
			"preferred_audio_lang": "", "preferred_subtitle_lang": "", "subtitle_mode": "off",
		})
		return
	}
	u, err := s.st.UserByID(r.Context(), sess.UserID)
	if err != nil {
		s.writeInternal(w, err, "read language preferences")
		return
	}
	writeJSON(w, http.StatusOK, languagePayload(u))
}

// setLanguagePreferences records the caller's own preferences.
func (s *Server) setLanguagePreferences(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusConflict, "no_account",
			"this server has no accounts yet; create one first")
		return
	}

	/*
	 * All three together, never one at a time.
	 *
	 * A subtitle language with no mode, or a mode with no language, are states
	 * somebody could be left in by a failed second request — and both are
	 * silently inert, which is the worst kind of half-applied change. The store
	 * takes them together for the same reason.
	 */
	var req struct {
		Audio    string `json:"preferred_audio_lang"`
		Subtitle string `json:"preferred_subtitle_lang"`
		Mode     string `json:"subtitle_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}

	if err := s.st.SetLanguagePreferences(r.Context(), sess.UserID,
		req.Audio, req.Subtitle, req.Mode); err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "no such account")
			return
		}
		/*
		 * A rejected code is the caller's mistake rather than the server's, and
		 * saying which is what lets somebody fix it. The store validates
		 * because it owns the column; the status comes from here because the
		 * store does not know about HTTP.
		 */
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	u, err := s.st.UserByID(r.Context(), sess.UserID)
	if err != nil {
		s.writeInternal(w, err, "read language preferences")
		return
	}
	/*
	 * Not audited, unlike the sharing toggle beside it.
	 *
	 * That one changes who can see something about a person, which is the class
	 * of act somebody would want to find in a log. This changes which audio
	 * track their own films start on. An audit trail of that is noise in the
	 * place people go looking for signal.
	 */
	writeJSON(w, http.StatusOK, languagePayload(u))
}

// languagePayload is the shape both handlers answer with, so a caller can send
// back what it read without translating.
func languagePayload(u *store.User) map[string]any {
	mode := u.SubtitleMode
	if mode == "" {
		// Stored empty, reported as "off". The database keeps one spelling of
		// the default; the wire gives the client a value it can put in a
		// <select> without a special case.
		mode = store.SubtitleOff
	}
	return map[string]any{
		"preferred_audio_lang":    u.PreferredAudioLang,
		"preferred_subtitle_lang": u.PreferredSubtitleLang,
		"subtitle_mode":           mode,
	}
}
