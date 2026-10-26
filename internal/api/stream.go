package api

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"lancast/internal/contain"
	"lancast/internal/store"
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
	// The optimized copy instead of the original, when the playback answer
	// said so (ADR 0087). Same permission: GetItem has answered it above.
	if r.URL.Query().Get("version") == "optimized" {
		s.serveOptimized(w, r, it)
		return
	}
	s.serveItemFile(w, r, it)
}

/*
 * serveItemFile is the half that turns a row into bytes, and it is shared by
 * every caller that has already decided the caller may have them.
 *
 * Shared deliberately. The containment re-check below is the rule CLAUDE.md
 * calls "the boundary where a bad row becomes arbitrary file access", and a
 * second copy of it is a second place for somebody to leave it out — which
 * would not fail any test, because the path would still resolve for every
 * ordinary row.
 *
 * It decides nothing about permission. Whoever calls it has answered that
 * already: an account through GetItem's ceiling, a friend through
 * store.MayPlay with a Friend principal.
 */
func (s *Server) serveItemFile(w http.ResponseWriter, r *http.Request, it *store.Item) {
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
		s.log.Error("stream containment check failed", "item", it.ID, "path", it.Path, "error", err)
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

/*
 * containedPath confirms target lies inside root and returns it as an absolute
 * path, ready to open.
 *
 * Inside on disk, not only by name: links on both sides are followed before
 * comparing (internal/contain). The lexical check alone passed `film.mkv`
 * linking to any file on the machine, and this server runs as LocalSystem.
 */
func containedPath(root, target string) (string, error) {
	return contain.Within(root, target)
}

// containedEntry is containedPath for removing target itself: a link is judged
// by the folder it sits in, not by where it points, because os.Remove removes
// the link.
func containedEntry(root, target string) (string, error) {
	return contain.EntryWithin(root, target)
}
