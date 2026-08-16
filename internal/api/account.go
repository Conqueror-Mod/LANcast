package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"lancast/internal/store"
)

/*
 * Two surfaces over the same rows, separated because they answer to different
 * authority.
 *
 * `PATCH /api/profile` is yours: change your own display name. It needs a
 * session and nothing else, because renaming yourself affects nobody.
 *
 * `PATCH /api/users/{id}` is the administrator's: rename an account or change
 * its role. It is admin-gated, and it is refused for the last remaining
 * administrator — a server with no admin cannot be administered remotely at
 * all, and the only way back is `lancastd reset-auth` on the machine itself.
 *
 * The rename keeps the account id, which is what makes it a rename rather than
 * a replacement: sessions, watch history, ratings and playlist membership all
 * hang off the id and follow silently.
 */

const maxNameLength = 60

// validName applies the same rule to both surfaces, so an administrator cannot
// create a name a person could not choose for themselves — two rules would
// eventually disagree and the disagreement would be a bug report.
func validName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if name == "" || len(name) > maxNameLength {
		return "", false
	}
	// Control characters would render as nothing and make two accounts look
	// identical in every list that shows a name.
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	return name, true
}

// patchProfile changes the caller's own display name.
func (s *Server) patchProfile(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		// The unconfigured loopback state has no account to edit. Saying so is
		// better than silently succeeding against a row that does not exist.
		writeError(w, http.StatusConflict, "no_account",
			"this server has no accounts yet; create one first")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	name, ok := validName(req.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request",
			"a name is required, and must be 60 characters or fewer")
		return
	}

	// The old name is read first so the audit entry can say what changed. An
	// entry that records only the new value cannot answer "who was this?".
	was := name
	if u, err := s.st.UserByID(r.Context(), sess.UserID); err == nil {
		was = u.Name
	}
	if err := s.st.RenameUser(r.Context(), sess.UserID, name); err != nil {
		s.renameError(w, err)
		return
	}
	s.audit(r, "profile.rename", "user", sess.UserID,
		fmt.Sprintf("%q changed their own display name to %q", was, name), nil)
	writeJSON(w, http.StatusOK, map[string]any{"id": sess.UserID, "name": name})
}

// patchUser is the administrator's edit: rename, change role, or both.
func (s *Server) patchUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid user id")
		return
	}

	var req struct {
		Name *string `json:"name"`
		Role *string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	if req.Name == nil && req.Role == nil {
		writeError(w, http.StatusBadRequest, "bad_request",
			"nothing to change: send a name, a role, or both")
		return
	}

	target, err := s.st.UserByID(r.Context(), id)
	if s.notFoundOr(w, err, "get user", "no such account") {
		return
	}

	if req.Name != nil {
		name, ok := validName(*req.Name)
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_request",
				"a name is required, and must be 60 characters or fewer")
			return
		}
		if err := s.st.RenameUser(r.Context(), id, name); err != nil {
			s.renameError(w, err)
			return
		}
		s.audit(r, "user.rename", "user", id,
			fmt.Sprintf("renamed %q to %q", target.Name, name), nil)
	}

	if req.Role != nil {
		if !store.ValidRole(*req.Role) {
			writeError(w, http.StatusBadRequest, "bad_request",
				"role must be admin or member")
			return
		}
		if err := s.st.SetUserRole(r.Context(), id, *req.Role); err != nil {
			switch {
			case errors.Is(err, store.ErrLastAdmin):
				// Refused in the store, inside a transaction with the count —
				// two admins demoting each other at once is a race a handler
				// check loses, and the prize is a server nobody can administer.
				writeError(w, http.StatusConflict, "last_admin",
					"this is the only administrator; promote somebody else first")
			case errors.Is(err, store.ErrNotFound):
				writeError(w, http.StatusNotFound, "not_found", "no such account")
			default:
				s.writeInternal(w, err, "set role")
			}
			return
		}
		s.audit(r, "user.role", "user", id,
			fmt.Sprintf("%q is now %s", target.Name, *req.Role), nil)
	}

	updated, err := s.st.UserByID(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "get user")
		return
	}
	sessions, err := s.st.SessionCountFor(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "count sessions")
		return
	}
	writeJSON(w, http.StatusOK, managedUser(*updated, sessions))
}

func (s *Server) renameError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrDuplicate):
		writeError(w, http.StatusConflict, "duplicate", "that name is already taken")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no such account")
	default:
		s.writeInternal(w, err, "rename")
	}
}

// managedUser is what the Users pane needs to manage an account, which is more
// than the account itself: whether somebody is currently signed in decides
// whether a rename or a role change will be noticed immediately or on their
// next visit.
type managedUserView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	CreatedAt int64  `json:"created_at"`
	// Sessions is live sessions for this account, not a login history. It
	// answers "is this person here right now", which is the question an admin
	// asks before changing something under them.
	Sessions int `json:"sessions"`
}

func managedUser(u store.User, sessions int) managedUserView {
	return managedUserView{
		ID: u.ID, Name: u.Name, Role: u.Role,
		CreatedAt: u.CreatedAt, Sessions: sessions,
	}
}
