package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// stream serves a media file for direct play.
//
// This is the one handler that turns a database row into filesystem access, so
// it re-verifies containment within the owning library root before opening
// anything. The database is trusted, but a bad or hand-edited row must not
// become arbitrary file read access. See CLAUDE.md.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}

	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	if it.Path == "" {
		writeError(w, http.StatusNotFound, "not_found", "item has no playable file")
		return
	}

	lib, err := s.st.GetLibrary(r.Context(), it.LibraryID)
	if s.notFoundOr(w, err, "get library", "owning library is gone") {
		return
	}

	path, err := containedPath(lib.Path, it.Path)
	if err != nil {
		// Not a client error to explain in detail — log it and treat the item
		// as unavailable.
		s.log.Error("stream containment check failed", "item", id, "path", it.Path, "error", err)
		writeError(w, http.StatusNotFound, "not_found", "no such item")
		return
	}

	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "file is missing from disk")
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "file is missing from disk")
		return
	}

	// ServeContent handles Range, If-Modified-Since, and partial responses —
	// which is what makes seeking work.
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

// containedPath resolves target and confirms it lies inside root.
func containedPath(root, target string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errOutsideLibrary
	}
	return absTarget, nil
}

type containmentError string

func (e containmentError) Error() string { return string(e) }

const errOutsideLibrary = containmentError("resolved path escapes the library root")
