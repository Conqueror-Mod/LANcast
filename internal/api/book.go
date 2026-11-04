package api

import (
	"encoding/json"
	"math"
	"net/http"
	"os"

	"lancast/internal/probe"
	"lancast/internal/store"
)

/*
 * Playing a book (ADR 0116 step 2).
 *
 * A book is one item however many files it is: the player is handed the book,
 * reads its parts from here, and plays them as one timeline. Progress is
 * written against the book and never against a part, so the Continue shelf
 * lists the book, a part never shows as "played", and a book counts once
 * when it finishes. This is the ADR's trap — a book must never become a
 * playlist of its chapters — answered on the server's side.
 */

// bookPart is one file of a book, placed on the book's timeline.
type bookPart struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	StartMS    int64  `json:"start_ms"`
	DurationMS int64  `json:"duration_ms"`
}

// bookInfo is everything the player needs to play a book from where this
// account left it.
type bookInfo struct {
	DurationMS int64 `json:"duration_ms"`
	// Ready is false while any part has not been probed yet: a timeline with
	// an unknown length in it cannot place anything after it.
	Ready    bool            `json:"ready"`
	Parts    []bookPart      `json:"parts"`
	Chapters []probe.Chapter `json:"chapters"`
	// The caller's own place, and the speed they last chose; speed is absent
	// when they never chose one, which plays at 1× (ADR 0116, answer 9).
	PositionMS int64    `json:"position_ms"`
	Watched    bool     `json:"watched"`
	Speed      *float64 `json:"speed,omitempty"`
}

// bookTimeline lays a book's parts end to end. Shared by the info route and
// the progress write, so the two cannot disagree about how long a book is.
func bookTimeline(parts []store.Item) ([]bookPart, int64, bool) {
	out := make([]bookPart, 0, len(parts))
	var at int64
	ready := len(parts) > 0
	for _, p := range parts {
		var d int64
		if p.DurationMS != nil {
			d = *p.DurationMS
		}
		if d <= 0 {
			ready = false
		}
		out = append(out, bookPart{ID: p.ID, Title: p.Title, StartMS: at, DurationMS: d})
		at += d
	}
	return out, at, ready
}

func (s *Server) bookInfo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	user := s.userID(r)
	it, err := s.st.GetItem(r.Context(), id, user)
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	if it.Kind != "book" {
		writeError(w, http.StatusBadRequest, "not_a_book", "this item is not a book")
		return
	}
	rows, err := s.st.BookParts(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "book parts")
		return
	}
	parts, total, ready := bookTimeline(rows)

	info := bookInfo{DurationMS: total, Ready: ready, Parts: parts}
	info.Chapters = s.bookChapters(r, rows, parts)
	if it.Progress != nil {
		info.PositionMS, info.Watched = it.Progress.PositionMS, it.Progress.Watched
	}
	if v, set, err := s.st.PlaybackSpeed(r.Context(), id, user); err == nil && set {
		info.Speed = &v
	}
	writeJSON(w, http.StatusOK, info)
}

/*
 * bookChapters is the book's chapter list on its own timeline.
 *
 * A one-file book is chaptered by its file (an .m4b carries them exactly as an
 * MKV does, read by the chapters route's cache). A book of several parts is
 * chaptered by its parts: the test library has no chapters inside any file
 * (ADR 0116), and reading every part of a forty-part rip with ffprobe on
 * every open would cost more than it could find.
 */
func (s *Server) bookChapters(r *http.Request, rows []store.Item, parts []bookPart) []probe.Chapter {
	if len(rows) == 1 {
		if ch := s.fileChapters(r, &rows[0]); len(ch) > 1 {
			return ch
		}
	}
	out := make([]probe.Chapter, 0, len(parts))
	for _, p := range parts {
		out = append(out, probe.Chapter{StartMS: p.StartMS, EndMS: p.StartMS + p.DurationMS, Title: p.Title})
	}
	return out
}

// fileChapters reads one file's chapters through the chapters route's cache,
// or nothing — every failure leaves the book chaptered by its parts.
func (s *Server) fileChapters(r *http.Request, it *store.Item) []probe.Chapter {
	if it.Path == "" || s.prober == nil || !s.prober.Available() {
		return nil
	}
	path, err := s.itemFilePath(r, it)
	if err != nil {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if cached, ok := s.chapters.get(it.ID, fi); ok {
		return cached
	}
	ch, err := s.prober.ReadChapters(r.Context(), path)
	if err != nil {
		return nil
	}
	s.chapters.put(it.ID, fi, ch)
	return ch
}

// bookFinishedSlackMS: a book is finished within its last minute. The film
// rule is a percentage, and 90% of a ten-hour book is an hour of it unheard.
const bookFinishedSlackMS = 60_000

// bookProgress is the progress write for a book: its length is its parts'.
func (s *Server) bookProgress(r *http.Request, it *store.Item, positionMS int64, watched bool) (bool, error) {
	rows, err := s.st.BookParts(r.Context(), it.ID)
	if err != nil {
		return false, err
	}
	_, total, ready := bookTimeline(rows)
	if ready && total > 0 && positionMS >= total-bookFinishedSlackMS {
		watched = true
	}
	return watched, nil
}

// The speeds a book offers (ADR 0116): 0.75× to 2×.
const (
	minBookSpeed = 0.75
	maxBookSpeed = 2.0
)

// PUT /api/items/{id}/speed: keep this account's speed for one book.
func (s *Server) putSpeed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	var req struct {
		Speed float64 `json:"speed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	if math.IsNaN(req.Speed) || req.Speed < minBookSpeed || req.Speed > maxBookSpeed {
		writeError(w, http.StatusBadRequest, "bad_request", "speed must be from 0.75 to 2")
		return
	}
	user := s.userID(r)
	it, err := s.st.GetItem(r.Context(), id, user)
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	if it.Kind != "book" {
		writeError(w, http.StatusBadRequest, "not_a_book", "speed is kept for books only")
		return
	}
	if err := s.st.SetPlaybackSpeed(r.Context(), id, user, req.Speed); err != nil {
		s.writeInternal(w, err, "set speed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
