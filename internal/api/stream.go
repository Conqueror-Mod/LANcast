package api

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Audio types are registered explicitly because ServeContent resolves the
// Content-Type from the file extension, and on Windows that lookup goes to the
// registry — where .flac and .opus are frequently absent and .m4a is whatever
// the last media player to install claimed. An unresolved extension falls back
// to content sniffing, which labels a FLAC application/octet-stream and turns
// a direct-play music track into a download prompt. The answer here does not
// depend on which machine the server happens to be running on.
func init() {
	for ext, typ := range map[string]string{
		".mp3":  "audio/mpeg",
		".flac": "audio/flac",
		".ogg":  "audio/ogg",
		".oga":  "audio/ogg",
		".opus": "audio/ogg",
		".m4a":  "audio/mp4",
		".m4b":  "audio/mp4",
		".aac":  "audio/aac",
		".wav":  "audio/wav",
		".wma":  "audio/x-ms-wma",
		".alac": "audio/mp4",
	} {
		// Errors here are impossible for literal types and are not worth
		// failing startup over in any case.
		_ = mime.AddExtensionType(ext, typ)
	}
}

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

	// Contained within the location this item was scanned under, not its
	// library's first one (ADR 0034).
	path, err := s.itemFilePath(r, it)
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
