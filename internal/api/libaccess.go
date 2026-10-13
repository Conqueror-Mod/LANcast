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
 * A library kept from an account (ADR 0084): the HTTP half.
 *
 * The rule itself is in the store, at the chokepoints the content-rating
 * ceiling already uses. This file is the three things only a handler can do:
 * leave hidden libraries out of the library list, refuse a library-scoped
 * route for a hidden library exactly as for one that does not exist, and let
 * an administrator set who sees what.
 */

// hiddenFor is the libraries kept from whoever is asking.
//
// Unlike ceilingFor, an error is not answered as "no restriction". A hidden
// library's titles are the thing being kept, and a listing that showed them
// because a lookup failed would leak precisely that; callers answer 500.
func (s *Server) hiddenFor(r *http.Request) ([]int64, error) {
	return s.st.HiddenLibraries(r.Context(), s.userID(r))
}

// libraryVisible wraps a /api/libraries/{id}/ route so a library kept from the
// caller answers exactly as one that does not exist: 404, same code, same
// message, so the refusal says nothing about what the server holds.
func (s *Server) libraryVisible(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(r)
		if !ok {
			h(w, r) // the handler's own bad-id answer
			return
		}
		visible, err := s.st.MayOpenLibrary(r.Context(), s.userID(r), id)
		if err != nil {
			s.writeInternal(w, err, "check library access")
			return
		}
		if !visible {
			writeError(w, http.StatusNotFound, "not_found", "no such library")
			return
		}
		h(w, r)
	}
}

// userLibrariesResponse is what an administrator edits: every library, and
// which of them this account cannot see.
type userLibrariesResponse struct {
	// Hidden holds the library ids kept from this account.
	Hidden []int64 `json:"hidden"`
	// Applies is false for an administrator, whose stored rows are kept but
	// ignored (ADR 0084 §2). The screen says so rather than offering switches
	// that do nothing.
	Applies bool `json:"applies"`
}

// userLibraries reports which libraries are kept from one account. Admin only.
func (s *Server) userLibraries(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	u, err := s.st.UserByID(r.Context(), id)
	if s.notFoundOr(w, err, "get user", "no such account") {
		return
	}
	hidden, err := s.st.StoredHiddenLibraries(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "read library access")
		return
	}
	if hidden == nil {
		hidden = []int64{}
	}
	writeJSON(w, http.StatusOK, userLibrariesResponse{Hidden: hidden, Applies: u.Role != store.RoleAdmin})
}

// setUserLibraries replaces which libraries are kept from one account. Admin
// only, and audited like a rating ceiling: it changes what somebody can see.
func (s *Server) setUserLibraries(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	u, err := s.st.UserByID(r.Context(), id)
	if s.notFoundOr(w, err, "get user", "no such account") {
		return
	}
	// A pointer, so a body without the field is refused rather than read as
	// "hide nothing", which would silently lift every restriction.
	var req struct {
		Hidden *[]int64 `json:"hidden"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Hidden == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "hidden is required: a list of library ids, empty for none")
		return
	}
	if err := s.st.SetHiddenLibraries(r.Context(), id, *req.Hidden); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "bad_request", "every id in hidden must be a library on this server")
			return
		}
		s.writeInternal(w, err, "set library access")
		return
	}

	names := []string{}
	if libs, err := s.st.ListLibraries(r.Context()); err == nil {
		for _, l := range libs {
			for _, h := range *req.Hidden {
				if l.ID == h {
					names = append(names, l.Name)
				}
			}
		}
	}
	summary := fmt.Sprintf("let %q see every library", u.Name)
	if len(names) > 0 {
		summary = fmt.Sprintf("kept %s from %q", strings.Join(names, ", "), u.Name)
	}
	s.audit(r, "user.libraries", "user", id, summary, map[string]any{"hidden": *req.Hidden})

	s.userLibraries(w, r)
}
