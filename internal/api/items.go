package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"lancast/internal/store"
)

func (s *Server) listItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.ItemFilter{
		LibraryID: int64(queryInt(r, "library_id")),
		Kind:      q.Get("kind"),
		Query:     q.Get("q"),
		Sort:      q.Get("sort"),
		Limit:     queryInt(r, "limit"),
		Offset:    queryInt(r, "offset"),
	}
	// parent_id fetches the children of one item — a show's episodes, a work's
	// parts. Otherwise the grid shows top-level entries only, so a container's
	// children never leak in loose (ADR 0010, ADR 0017). An explicit kind is
	// treated as a deliberate cross-cutting query and is not forced top-level.
	if v := q.Get("parent_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.ParentID = &id
		} else {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid parent_id")
			return
		}
	} else if f.Kind == "" {
		f.TopLevel = true
	}

	items, total, err := s.st.ListItems(r.Context(), f)
	if err != nil {
		s.writeInternal(w, err, "list items")
		return
	}
	if err := s.st.AttachProgress(r.Context(), items, s.userID(r)); err != nil {
		s.writeInternal(w, err, "attach progress")
		return
	}
	// The grid renders from this response, so posters have to come with it.
	if err := s.st.AttachArtwork(r.Context(), items); err != nil {
		s.writeInternal(w, err, "attach artwork")
		return
	}
	// So a tile knows whether it is a container (a show, a multi-part work) and
	// should open a children view rather than offer Play.
	if err := s.st.AttachChildCounts(r.Context(), items); err != nil {
		s.writeInternal(w, err, "attach child counts")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"total": total, "items": items})
}

// continueWatching lists the user's in-progress items, most recently played
// first — the home screen's first shelf. Progress is included so tiles can draw
// their resume bar without a second call.
func (s *Server) continueWatching(w http.ResponseWriter, r *http.Request) {
	items, err := s.st.ContinueWatching(r.Context(), s.userID(r), queryInt(r, "limit"))
	if err != nil {
		s.writeInternal(w, err, "continue watching")
		return
	}
	if err := s.st.AttachArtwork(r.Context(), items); err != nil {
		s.writeInternal(w, err, "attach artwork")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) getItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	if _, err := s.st.GetItem(r.Context(), id, s.userID(r)); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	s.respondItem(w, r, id)
}

func (s *Server) putProgress(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	var req struct {
		PositionMS int64 `json:"position_ms"`
		Watched    bool  `json:"watched"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	if req.PositionMS < 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "position_ms must not be negative")
		return
	}

	if _, err := s.st.GetItem(r.Context(), id, s.userID(r)); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	if err := s.st.SaveProgress(r.Context(), id, s.userID(r), req.PositionMS, req.Watched); err != nil {
		s.writeInternal(w, err, "save progress")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
