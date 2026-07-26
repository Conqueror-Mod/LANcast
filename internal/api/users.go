package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"lancast/internal/auth"
	"lancast/internal/store"
)

// listUsers returns every account. Admin-only. Password hashes are tagged
// json:"-" on the store type, so serializing the slice cannot leak them.
func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.st.ListUsers(r.Context())
	if err != nil {
		s.writeInternal(w, err, "list users")
		return
	}
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		out = append(out, userJSON(u.ID, u.Name, u.Role))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

// createUser adds an account. Admin-only. Role defaults to member — the least
// privilege — so an omitted role never accidentally mints an admin.
func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}

	name := strings.TrimSpace(req.Username)
	if name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "username is required")
		return
	}
	role := req.Role
	if role == "" {
		role = store.RoleMember
	}
	if !store.ValidRole(role) {
		writeError(w, http.StatusBadRequest, "bad_request", "role must be admin or member")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if errors.Is(err, auth.ErrWeakPassword) {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err != nil {
		s.writeInternal(w, err, "hash password")
		return
	}

	u, err := s.st.CreateUser(r.Context(), "", name, hash, role)
	if errors.Is(err, store.ErrDuplicate) {
		writeError(w, http.StatusConflict, "conflict", "that username is taken")
		return
	}
	if err != nil {
		s.writeInternal(w, err, "create user")
		return
	}
	writeJSON(w, http.StatusCreated, userJSON(u.ID, u.Name, u.Role))
}

// deleteUser removes an account. Admin-only. The last admin cannot be deleted —
// an instance with no admin can never be managed again.
func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	target, err := s.st.UserByID(r.Context(), id)
	if s.notFoundOr(w, err, "get user", "no such user") {
		return
	}

	if target.Role == store.RoleAdmin {
		admins, err := s.st.CountAdmins(r.Context())
		if err != nil {
			s.writeInternal(w, err, "count admins")
			return
		}
		if admins <= 1 {
			writeError(w, http.StatusConflict, "conflict", "cannot delete the last administrator")
			return
		}
	}

	if err := s.st.DeleteUser(r.Context(), id); s.notFoundOr(w, err, "delete user", "no such user") {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resetUserPassword lets an admin set another user's password, logging that
// user out everywhere. It does not require the target's current password —
// that is the point of an admin reset — but it cannot read the old one either.
func (s *Server) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req struct {
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if errors.Is(err, auth.ErrWeakPassword) {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err != nil {
		s.writeInternal(w, err, "hash password")
		return
	}

	if err := s.st.SetUserPassword(r.Context(), id, hash); s.notFoundOr(w, err, "set password", "no such user") {
		return
	}
	if err := s.st.DeleteUserSessions(r.Context(), id); err != nil {
		s.writeInternal(w, err, "revoke sessions")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
