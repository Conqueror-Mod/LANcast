package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"lancast/internal/store"
)

/*
 * Tags and favourites over HTTP (ADR 0062).
 *
 * Every handler here passes `s.userID(r)` into the store, and none of them takes
 * an account from the request. That is not defensive style — it is the feature.
 * A tag is a note somebody wrote to themselves, and the store is written so
 * there is no shape of caller that can read another account's; these handlers
 * exist to make sure nothing at this layer hands it a different one.
 *
 * Not behind adminOnly, and reachable with an API key (ADR 0061): tagging
 * changes what is written about an item, not what the server can reach. A key
 * acts as its owner, so it writes that owner's tags and sees no others.
 */

// listTags is the caller's own tags, with counts — what a filter row is built
// from.
func (s *Server) listTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.st.ListTags(r.Context(), s.userID(r))
	if err != nil {
		s.writeInternal(w, err, "list tags")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

// itemTags is the caller's tags on one item, and whether they have favourited
// it. Both are answered together because the detail page wants both and they
// are the same question asked about the same row.
func (s *Server) itemTags(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	user := s.userID(r)

	tags, err := s.st.ItemTags(r.Context(), user, id)
	if err != nil {
		s.writeInternal(w, err, "item tags")
		return
	}
	fav, err := s.st.IsFavourite(r.Context(), user, id)
	if err != nil {
		s.writeInternal(w, err, "is favourite")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags, "favourite": fav})
}

// addItemTag puts one of the caller's tags on an item, creating it if this is
// the first use of the word by this account.
func (s *Server) addItemTag(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	if _, err := s.st.GetItem(r.Context(), id, s.userID(r)); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not read the request")
		return
	}

	tag, err := s.st.AddTag(r.Context(), s.userID(r), id, body.Name)
	if errors.Is(err, store.ErrEmptyTag) {
		writeError(w, http.StatusBadRequest, "bad_request", "a tag needs a name")
		return
	}
	if err != nil {
		s.writeInternal(w, err, "add tag")
		return
	}
	/*
	 * Not audited.
	 *
	 * The audit log is readable by an administrator, and a private note whose
	 * text appears in a log an administrator reads is not private. Every other
	 * write in this API is audited; this one is the exception the decision
	 * requires, and it is worth saying so here rather than leaving it looking
	 * like an omission.
	 */
	writeJSON(w, http.StatusCreated, map[string]any{"tag": tag})
}

// removeItemTag takes one of the caller's tags off an item.
func (s *Server) removeItemTag(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	tagID, err := strconv.ParseInt(r.PathValue("tag"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid tag id")
		return
	}

	err = s.st.RemoveTag(r.Context(), s.userID(r), id, tagID)
	if errors.Is(err, store.ErrNotFound) {
		/*
		 * 404 for a tag that is somebody else's, exactly as for one that does
		 * not exist. Distinguishing them would answer "does this id belong to
		 * another account" for anybody who asked, which is the same disclosure
		 * the per-account vocabulary exists to prevent.
		 */
		writeError(w, http.StatusNotFound, "not_found", "no such tag on this item")
		return
	}
	if err != nil {
		s.writeInternal(w, err, "remove tag")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": true})
}

// putFavourite marks or unmarks an item for the caller.
func (s *Server) putFavourite(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	if _, err := s.st.GetItem(r.Context(), id, s.userID(r)); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	var body struct {
		Favourite bool `json:"favourite"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not read the request")
		return
	}
	if err := s.st.SetFavourite(r.Context(), s.userID(r), id, body.Favourite); err != nil {
		s.writeInternal(w, err, "set favourite")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"favourite": body.Favourite})
}
