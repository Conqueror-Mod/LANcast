package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"lancast/internal/artwork"
)

// browserRenderable are the picture formats a browser will display if handed
// the bytes. HEIC is pointedly absent: Chromium and Firefox do not decode it,
// and Safari only on Apple hardware.
var browserRenderable = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
	".gif": true, ".bmp": true,
}

// photo serves a picture at full resolution (ADR 0028).
//
// The second handler that turns a database row into filesystem access, so it
// re-verifies containment within the owning library root before opening
// anything, exactly as stream does. The database is trusted; a bad or
// hand-edited row must not become arbitrary file read access. See CLAUDE.md.
//
// "Full resolution" means the original file when a browser can render it, and
// the cached rendition when it cannot. Serving a HEIC to a viewer that cannot
// decode it would be technically correct and useless: the user asked to see
// their photo, and a broken image icon is not an answer. The rendition is the
// 1600px copy the thumbnail worker already made, which is the best this build
// can offer for that format.
func (s *Server) photo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}

	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	if it.Kind != "photo" || it.Path == "" {
		writeError(w, http.StatusNotFound, "not_found", "item is not a photo")
		return
	}

	lib, err := s.st.GetLibrary(r.Context(), it.LibraryID)
	if s.notFoundOr(w, err, "get library", "owning library is gone") {
		return
	}

	path, err := containedPath(lib.Path, it.Path)
	if err != nil {
		// Not a client error to explain in detail — log it and treat the item
		// as unavailable, the same as stream does.
		s.log.Error("photo containment check failed", "item", id, "path", it.Path, "error", err)
		writeError(w, http.StatusNotFound, "not_found", "no such item")
		return
	}

	if !browserRenderable[strings.ToLower(filepath.Ext(path))] {
		s.servePhotoRendition(w, r, id)
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

	// Revalidated rather than cached immutably. Artwork is content-addressed and
	// can be immutable; a photo is served by item id, and the file behind that
	// id can be replaced on disk without the id changing. ServeContent answers
	// the revalidation from the modtime, so the common case is still a 304.
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

// servePhotoRendition falls back to the cached copy for a format the browser
// cannot render.
func (s *Server) servePhotoRendition(w http.ResponseWriter, r *http.Request, id int64) {
	art, err := s.st.ItemArtwork(r.Context(), id)
	if err != nil || art == nil || art.Poster == "" {
		// No rendition either: the worker has not reached it, or could not read
		// the format at all. Said plainly rather than as a generic 404, because
		// the two have different cures — wait, or install ffmpeg.
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"this picture has no viewable rendition yet")
		return
	}

	f, err := s.art.Open(art.Poster, artwork.SizeOriginal)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusServiceUnavailable, "unavailable",
				"this picture has no viewable rendition yet")
			return
		}
		s.writeInternal(w, err, "open photo rendition")
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		s.writeInternal(w, err, "stat photo rendition")
		return
	}
	// The rendition is content-addressed: these bytes never change, so it is
	// cached as aggressively as artwork is.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeContent(w, r, art.Poster+".jpg", info.ModTime(), f)
}
