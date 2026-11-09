package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"lancast/internal/comic"
	"lancast/internal/store"
)

/*
 * Comics (ADR 0101): a comic's pages, served one at a time out of its archive,
 * and where each person is in it.
 *
 * Nothing is unpacked to disk. A page is read out of the zip when it is asked
 * for, which costs a few milliseconds and keeps a library of comics from
 * doubling in size. A page is named by its number in reading order, never by
 * its name inside the archive, so nothing a client sends is a path.
 */

// comicFor opens the archive behind a comic item, after the same visibility
// and containment checks every file route makes. The caller closes it.
func (s *Server) comicFor(w http.ResponseWriter, r *http.Request) (*store.Item, *comicArchive, bool) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return nil, nil, false
	}
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return nil, nil, false
	}
	if it.Kind != "comic" || it.Path == "" {
		writeError(w, http.StatusNotFound, "not_found", "item is not a comic")
		return nil, nil, false
	}
	path, err := s.itemFilePath(r, it)
	if err != nil {
		s.log.Error("comic containment check failed", "item", id, "path", it.Path, "error", err)
		writeError(w, http.StatusNotFound, "not_found", "no such item")
		return nil, nil, false
	}
	a, err := openComicArchive(path)
	if err != nil {
		s.log.Warn("could not open a comic", "item", id, "error", err)
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"this comic cannot be opened: the file is missing or is not a comic archive")
		return nil, nil, false
	}
	return it, a, true
}

// comicArchive is an opened comic — a .cbz or a .pdf (ADR 0102) — and the
// file's modification time, which a page is revalidated against.
type comicArchive struct {
	path string
	info os.FileInfo
	comic.Book
}

func openComicArchive(path string) (*comicArchive, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	b, err := comic.OpenBook(path)
	if err != nil {
		return nil, err
	}
	return &comicArchive{path: path, info: info, Book: b}, nil
}

func (a *comicArchive) Close() { _ = a.Book.Close() }

// comicPages answers GET /api/items/{id}/pages: how many, and where the caller
// is.
func (s *Server) comicPages(w http.ResponseWriter, r *http.Request) {
	it, a, ok := s.comicFor(w, r)
	if !ok {
		return
	}
	defer a.Close()
	out := map[string]any{"pages": a.Len(), "place": nil}
	place, has, err := s.st.ComicPlaceFor(r.Context(), s.userID(r), it.ID)
	if err != nil {
		s.writeInternal(w, err, "read comic place")
		return
	}
	if has {
		// A place past the end of an archive that has since lost pages is
		// its last page, not an error.
		if place.Page >= a.Len() {
			place.Page = a.Len() - 1
		}
		out["place"] = place
	}
	d, err := s.comicDirection(r.Context(), it)
	if err != nil {
		s.writeInternal(w, err, "read reading direction")
		return
	}
	out["direction"], out["direction_from"], out["series"] = d.Direction, d.From, d.Series
	writeJSON(w, http.StatusOK, out)
}

// readingDirection is which way a comic reads and why (ADR 0123).
type readingDirection struct {
	Direction string  `json:"direction"`      // ltr | rtl
	From      string  `json:"direction_from"` // series | library | default
	Series    *string `json:"series"`         // null for a comic at the library's root
}

/*
 * comicDirection resolves a comic's reading direction: its series' override,
 * else its library's, else left to right. Resolved here so no client works
 * out the precedence for itself, and two clients cannot disagree about one
 * book.
 */
func (s *Server) comicDirection(ctx context.Context, it *store.Item) (readingDirection, error) {
	out := readingDirection{Direction: "ltr", From: "default"}
	if it.Series != nil && *it.Series != "" {
		out.Series = it.Series
		dir, err := s.st.SeriesDirection(ctx, it.LibraryID, *it.Series)
		if err != nil {
			return out, err
		}
		if dir != "" {
			out.Direction, out.From = dir, "series"
			return out, nil
		}
	}
	lib, err := s.st.GetLibrary(ctx, it.LibraryID)
	if err != nil {
		return out, err
	}
	if lib.ReadingDirection != nil && *lib.ReadingDirection != "" {
		out.Direction, out.From = *lib.ReadingDirection, "library"
	}
	return out, nil
}

/*
 * setSeriesDirection answers PUT /api/items/{id}/series-direction: the comic's
 * whole series reads this way, whatever its library does (ADR 0123). Admin
 * only, like the library's own direction.
 *
 * Addressed through a comic rather than by series name, so a client can only
 * name a series that exists, spelled as the library spells it. "" removes the
 * override and the series reads its library's way again. Answers with the
 * comic's direction as resolved after the change.
 */
func (s *Server) setSeriesDirection(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	var req struct {
		Direction *string `json:"direction"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Direction == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "body must be {\"direction\": \"ltr\" | \"rtl\" | \"\"}")
		return
	}
	dir := strings.TrimSpace(*req.Direction)
	if dir != "" && dir != "ltr" && dir != "rtl" {
		writeError(w, http.StatusBadRequest, "bad_request", "direction must be ltr, rtl or empty")
		return
	}
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	if it.Kind != "comic" {
		writeError(w, http.StatusNotFound, "not_found", "item is not a comic")
		return
	}
	if it.Series == nil || *it.Series == "" {
		writeError(w, http.StatusBadRequest, "bad_request",
			"this comic is not in a series folder; set the library's reading direction instead")
		return
	}
	if err := s.st.SetSeriesDirection(r.Context(), it.LibraryID, *it.Series, dir); err != nil {
		s.writeInternal(w, err, "set series direction")
		return
	}
	shown := map[string]string{"": "its library's way", "ltr": "left to right", "rtl": "right to left"}[dir]
	s.audit(r, "library.update", "library", auditID(it.LibraryID),
		"Set the series "+*it.Series+" to read "+shown,
		map[string]any{"series": *it.Series, "direction": dir})
	d, err := s.comicDirection(r.Context(), it)
	if err != nil {
		s.writeInternal(w, err, "read reading direction")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// comicPage answers GET /api/items/{id}/pages/{n}: one page, numbered from 0.
func (s *Server) comicPage(w http.ResponseWriter, r *http.Request) {
	_, a, ok := s.comicFor(w, r)
	if !ok {
		return
	}
	defer a.Close()
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 || n >= a.Len() {
		writeError(w, http.StatusNotFound, "not_found", "no such page")
		return
	}
	body, ctype, err := a.Page(n)
	if err != nil {
		msg := "this page cannot be read"
		var why comic.UnreadablePage
		if errors.As(err, &why) {
			msg = string(why)
		} else {
			s.log.Warn("could not read a comic page", "path", a.path, "page", n, "error", err)
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable", msg)
		return
	}
	// Revalidated against the archive's modification time, as a photo is: the
	// file behind an item id can be replaced without the id changing.
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", a.info.ModTime(), bytes.NewReader(body))
}

// putComicPlace answers PUT /api/items/{id}/place: {"page": n}. Reaching the
// last page marks the comic read, as finishing a film marks it watched.
func (s *Server) putComicPlace(w http.ResponseWriter, r *http.Request) {
	it, a, ok := s.comicFor(w, r)
	if !ok {
		return
	}
	pages := a.Len()
	a.Close()
	var req struct {
		Page *int `json:"page"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Page == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "page is required")
		return
	}
	page := *req.Page
	if page < 0 || page >= pages {
		writeError(w, http.StatusBadRequest, "bad_request", "no such page")
		return
	}
	user := s.userID(r)
	if err := s.st.SetComicPlace(r.Context(), user, it.ID, page, pages); err != nil {
		s.writeInternal(w, err, "save comic place")
		return
	}
	if page == pages-1 {
		if err := s.st.SaveProgress(r.Context(), it.ID, user, 0, true); err != nil {
			s.writeInternal(w, err, "mark comic read")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
