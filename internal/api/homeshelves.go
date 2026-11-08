package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"lancast/internal/store"
)

/*
 * Shelves you pin to Home (ADR 0119 §2).
 *
 *	GET    /api/home/shelves         your pinned shelves, in order
 *	POST   /api/home/shelves         pin one: {library_id, title, query}
 *	PATCH  /api/home/shelves/{id}    rename and/or move: {title?, position?}
 *	DELETE /api/home/shelves/{id}    unpin
 *
 * A shelf is a saved library view, so its query is checked against the
 * parameters the library grid sets and nothing else (web/src/lib/
 * libraryQuery.ts, SHELF_PARAM_KEYS). A shelf can only ever be a listing you
 * could have asked GET /api/items for by hand; the client fetches its items
 * that way, so the account's ceiling and hidden libraries apply to every row
 * exactly as on the grid.
 */

// shelfParams are the parameters a pinned query may carry. Must stay the
// client's SHELF_PARAM_KEYS.
var shelfParams = map[string]bool{
	"genre": true, "decade": true, "year": true, "actor": true, "director": true,
	"author": true, "narrator": true, "series": true, "collection": true,
	"content_rating": true, "min_rating": true, "resolution": true, "platform": true,
	"release_type": true, "tag": true, "status": true, "watched": true,
	"favourite": true, "face_cluster": true, "person": true,
	"sort": true, "show": true, "initial": true, "q": true,
}

// maxShelfTitle is the longest title kept, in characters. A shelf heading is
// one line; anything longer is a paste, not a name.
const maxShelfTitle = 80

// cleanShelfQuery checks a pinned query and returns it in a canonical order.
// ok is false when it carries a parameter the grid does not set.
func cleanShelfQuery(raw string) (string, bool) {
	v, err := url.ParseQuery(raw)
	if err != nil {
		return "", false
	}
	for k := range v {
		if !shelfParams[k] {
			return "", false
		}
	}
	return v.Encode(), true
}

func cleanShelfTitle(raw string) (string, bool) {
	t := strings.TrimSpace(raw)
	if t == "" || utf8.RuneCountInString(t) > maxShelfTitle {
		return "", false
	}
	return t, true
}

func (s *Server) listHomeShelves(w http.ResponseWriter, r *http.Request) {
	user := s.userID(r)
	shelves, err := s.st.HomeShelves(r.Context(), user)
	if err != nil {
		s.writeInternal(w, err, "home shelves")
		return
	}
	/*
	 * A shelf over a library since kept from you is not shown, and it is kept,
	 * in case access comes back. Its position stays, so the order you set is
	 * the order you get back.
	 */
	hidden, err := s.hiddenFor(r)
	if err != nil {
		s.writeInternal(w, err, "check library access")
		return
	}
	kept := shelves[:0]
	for _, h := range shelves {
		if !containsID(hidden, h.LibraryID) {
			kept = append(kept, h)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"shelves": kept, "max": store.MaxHomeShelves})
}

func (s *Server) addHomeShelf(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LibraryID int64  `json:"library_id"`
		Title     string `json:"title"`
		Query     string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	title, ok := cleanShelfTitle(req.Title)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "a shelf needs a title of 1 to 80 characters")
		return
	}
	query, ok := cleanShelfQuery(req.Query)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "the query holds something the library grid does not set")
		return
	}
	user := s.userID(r)
	if _, err := s.st.GetLibrary(r.Context(), req.LibraryID); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such library")
		return
	} else if err != nil {
		s.writeInternal(w, err, "get library")
		return
	}
	// A library you cannot open answers as one that does not exist.
	visible, err := s.st.MayOpenLibrary(r.Context(), user, req.LibraryID)
	if err != nil {
		s.writeInternal(w, err, "check library access")
		return
	}
	if !visible {
		writeError(w, http.StatusNotFound, "not_found", "no such library")
		return
	}
	h, err := s.st.AddHomeShelf(r.Context(), user, req.LibraryID, title, query)
	if errors.Is(err, store.ErrHomeShelfLimit) {
		writeError(w, http.StatusConflict, "shelf_limit", "Home holds at most 12 pinned shelves; unpin one first")
		return
	}
	if err != nil {
		s.writeInternal(w, err, "add home shelf")
		return
	}
	writeJSON(w, http.StatusCreated, h)
}

func (s *Server) patchHomeShelf(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid shelf id")
		return
	}
	var req struct {
		Title    *string `json:"title"`
		Position *int    `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	if req.Title == nil && req.Position == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "nothing to change: send title or position")
		return
	}
	user := s.userID(r)
	notFound := func(err error) bool {
		if errors.Is(err, store.ErrHomeShelfNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "no such shelf")
			return true
		}
		return false
	}
	if req.Title != nil {
		title, ok := cleanShelfTitle(*req.Title)
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_request", "a shelf needs a title of 1 to 80 characters")
			return
		}
		if err := s.st.RenameHomeShelf(r.Context(), user, id, title); err != nil {
			if !notFound(err) {
				s.writeInternal(w, err, "rename home shelf")
			}
			return
		}
	}
	if req.Position != nil {
		if err := s.st.MoveHomeShelf(r.Context(), user, id, *req.Position); err != nil {
			if !notFound(err) {
				s.writeInternal(w, err, "move home shelf")
			}
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteHomeShelf(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid shelf id")
		return
	}
	err := s.st.DeleteHomeShelf(r.Context(), s.userID(r), id)
	if errors.Is(err, store.ErrHomeShelfNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such shelf")
		return
	}
	if err != nil {
		s.writeInternal(w, err, "delete home shelf")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
