package api

import (
	"encoding/json"
	"math"
	"net/http"

	"lancast/internal/store"
)

/*
 * Your reading and listening defaults (ADR 0120, settings-3-plan step 4):
 *
 *	GET /api/profile/reading   the speed a book starts at, a sleep timer set
 *	                           when a book starts, how comics are laid out
 *	PUT /api/profile/reading   change any of them; a field left out is kept
 *
 * Defaults only. A book's own speed (PUT /api/items/{id}/speed) still wins,
 * and the book player can change or clear the sleep timer it was given. A
 * null field has never been chosen: 1x, no timer, one page fitted to the
 * screen. `account` is false on a server with no accounts yet.
 */

// The speeds a book offers, matching the book player's menu exactly: a
// default the menu cannot show would leave the player with no item ticked.
var bookSpeeds = map[float64]bool{0.75: true, 1: true, 1.25: true, 1.5: true, 1.75: true, 2: true}

var bookSleeps = map[string]bool{"off": true, "15": true, "30": true, "45": true, "60": true, "chapter": true}

var comicLayouts = map[string]bool{"single": true, "spread": true}

var comicFits = map[string]bool{"page": true, "width": true}

type readingPrefsResponse struct {
	Account bool `json:"account"`
	store.ReadingPrefs
}

func (s *Server) readingPreferences(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeJSON(w, http.StatusOK, readingPrefsResponse{})
		return
	}
	p, err := s.st.ReadingPrefsFor(r.Context(), sess.UserID)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "no such account")
			return
		}
		s.writeInternal(w, err, "read reading preferences")
		return
	}
	writeJSON(w, http.StatusOK, readingPrefsResponse{Account: true, ReadingPrefs: p})
}

func (s *Server) setReadingPreferences(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusConflict, "no_account",
			"this server has no accounts yet; create one first")
		return
	}
	var req store.ReadingPrefs
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		(req.BookSpeed == nil && req.BookSleep == nil && req.ComicLayout == nil && req.ComicFit == nil) {
		writeError(w, http.StatusBadRequest, "bad_request",
			"one of book_speed, book_sleep, comic_layout or comic_fit is required")
		return
	}
	if req.BookSpeed != nil && (math.IsNaN(*req.BookSpeed) || !bookSpeeds[*req.BookSpeed]) {
		writeError(w, http.StatusBadRequest, "bad_book_speed",
			"book_speed must be 0.75, 1, 1.25, 1.5, 1.75 or 2")
		return
	}
	if req.BookSleep != nil && !bookSleeps[*req.BookSleep] {
		writeError(w, http.StatusBadRequest, "bad_book_sleep",
			"book_sleep must be off, 15, 30, 45, 60 or chapter")
		return
	}
	if req.ComicLayout != nil && !comicLayouts[*req.ComicLayout] {
		writeError(w, http.StatusBadRequest, "bad_comic_layout", "comic_layout must be single or spread")
		return
	}
	if req.ComicFit != nil && !comicFits[*req.ComicFit] {
		writeError(w, http.StatusBadRequest, "bad_comic_fit", "comic_fit must be page or width")
		return
	}
	if err := s.st.SetReadingPrefs(r.Context(), sess.UserID, req); err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "no such account")
			return
		}
		s.writeInternal(w, err, "set reading preferences")
		return
	}
	s.readingPreferences(w, r)
}
