package api

import (
	"encoding/json"
	"net/http"
)

/*
 * Marking content sensitive (ADR 0051).
 *
 * Admin-only, like every other write that changes what the library says about
 * itself. The asymmetry that would matter — a member able to mark but not
 * unmark, or the reverse — cannot arise from one permission covering both.
 *
 * The setting gates the *gesture*, not the storage. Marks made while it was on
 * survive it being turned off, so turning it off and on again does not cost
 * somebody the folders they marked; what it stops is new marks and the
 * obscuring itself.
 */
func (s *Server) putSensitive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	if !s.settings.Get().SensitiveMarking {
		writeError(w, http.StatusConflict, "not_enabled",
			"sensitive marking is turned off in settings")
		return
	}

	var req struct {
		Sensitive *bool `json:"sensitive"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Sensitive == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "sensitive must be true or false")
		return
	}

	item, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	/*
	 * Folders only, and refused on the server rather than merely hidden in the
	 * menu (ADR 0051, amended).
	 *
	 * A single photograph could be marked, and that turned out to be the wrong
	 * shape. A loose marked photo has nowhere to be *viewed* — the only place a
	 * cover may be lifted is inside a folder — so marking one produced content
	 * that was covered everywhere and reachable nowhere. The answer is to put it
	 * in a folder and mark that, which is a thing the person can do and the
	 * software cannot do for them.
	 *
	 * Unmarking is deliberately still allowed on anything: photographs marked
	 * before this rule existed have to be clearable, and refusing to let
	 * somebody undo a mark because the mark should not have been possible is
	 * how data becomes permanent by accident.
	 */
	if *req.Sensitive && item.Kind != "gallery" {
		writeError(w, http.StatusBadRequest, "folders_only",
			"only a folder can be marked sensitive — put the photo in a folder and mark that")
		return
	}
	if err := s.st.SetSensitive(r.Context(), id, *req.Sensitive); err != nil {
		s.writeInternal(w, err, "set sensitive")
		return
	}

	action := "item.unmark_sensitive"
	summary := "unmarked " + item.Title + " as sensitive"
	if *req.Sensitive {
		action = "item.mark_sensitive"
		summary = "marked " + item.Title + " as sensitive"
	}
	/*
	 * The title is recorded and nothing else is.
	 *
	 * An audit line saying which folder somebody marked is the record of a
	 * decision, which is what this log is for. A line saying who then looked at
	 * it would be a record of the thing the feature exists to keep private, and
	 * it would live in backups for ever. Acknowledgement is never audited and
	 * never reaches the server at all.
	 */
	s.audit(r, action, "item", auditID(id), summary, nil)
	writeJSON(w, http.StatusOK, map[string]any{"sensitive": *req.Sensitive})
}
