package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strconv"

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

type comicArchive struct {
	path string
	info os.FileInfo
	zr   interface{ Close() error }
	// pages in reading order.
	pages []pageEntry
}

type pageEntry struct {
	name string
	read func() ([]byte, error)
}

func openComicArchive(path string) (*comicArchive, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	zr, err := comic.Open(path)
	if err != nil {
		return nil, err
	}
	files, err := comic.Pages(&zr.Reader)
	if err != nil {
		zr.Close()
		return nil, err
	}
	a := &comicArchive{path: path, info: info, zr: zr}
	for _, f := range files {
		f := f
		a.pages = append(a.pages, pageEntry{name: f.Name, read: func() ([]byte, error) { return comic.ReadPage(f) }})
	}
	return a, nil
}

func (a *comicArchive) Close() { _ = a.zr.Close() }

// comicPages answers GET /api/items/{id}/pages: how many, and where the caller
// is.
func (s *Server) comicPages(w http.ResponseWriter, r *http.Request) {
	it, a, ok := s.comicFor(w, r)
	if !ok {
		return
	}
	defer a.Close()
	out := map[string]any{"pages": len(a.pages), "place": nil}
	place, has, err := s.st.ComicPlaceFor(r.Context(), s.userID(r), it.ID)
	if err != nil {
		s.writeInternal(w, err, "read comic place")
		return
	}
	if has {
		// A place past the end of an archive that has since lost pages is
		// its last page, not an error.
		if place.Page >= len(a.pages) {
			place.Page = len(a.pages) - 1
		}
		out["place"] = place
	}
	writeJSON(w, http.StatusOK, out)
}

// comicPage answers GET /api/items/{id}/pages/{n}: one page, numbered from 0.
func (s *Server) comicPage(w http.ResponseWriter, r *http.Request) {
	_, a, ok := s.comicFor(w, r)
	if !ok {
		return
	}
	defer a.Close()
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 || n >= len(a.pages) {
		writeError(w, http.StatusNotFound, "not_found", "no such page")
		return
	}
	p := a.pages[n]
	body, err := p.read()
	if err != nil {
		s.log.Warn("could not read a comic page", "path", a.path, "page", n, "error", err)
		writeError(w, http.StatusServiceUnavailable, "unavailable", "this page cannot be read")
		return
	}
	// Revalidated against the archive's modification time, as a photo is: the
	// file behind an item id can be replaced without the id changing.
	w.Header().Set("Content-Type", comic.ContentType(p.name))
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
	pages := len(a.pages)
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
