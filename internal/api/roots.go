package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"lancast/internal/store"
)

/*
 * A library's locations (ADR 0034).
 *
 * Adding one is filesystem access at an arbitrary path chosen by the caller,
 * which is exactly what creating a library is, so it carries the same
 * admin-only gate. Listing them is not gated, because the paths are already in
 * the library listing that anyone signed in can read — gating one and not the
 * other would be a lock on a door beside an open window.
 */

// rootPathID reads {rootID} from the route.
func rootPathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("rootID"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// checkDirectory resolves a caller-supplied path and confirms it is a readable
// directory.
//
// Checked before anything is written, because a location that is not there
// would be skipped by every scan while looking configured — and on a repoint,
// pointing at a typo would strand the rows that used to resolve.
func checkDirectory(raw string) (string, string) {
	abs, err := filepath.Abs(strings.TrimSpace(raw))
	if err != nil {
		return "", "path could not be resolved"
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", "path does not exist or is unreadable"
	}
	if !info.IsDir() {
		return "", "path is not a directory"
	}
	return abs, ""
}

// writeRootErr maps the store's refusals onto status codes.
//
// ErrRootOverlaps is a conflict rather than a bad request: the path is
// perfectly well formed and the objection is about what is already there.
func (s *Server) writeRootErr(w http.ResponseWriter, err error, what string) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrRootOverlaps):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, store.ErrLastRoot):
		writeError(w, http.StatusConflict, "conflict",
			"a library must keep at least one location; delete the library instead")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no such location")
	default:
		s.writeInternal(w, err, what)
	}
	return true
}

func (s *Server) listRoots(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return
	}
	if _, err := s.st.GetLibrary(r.Context(), id); s.notFoundOr(w, err, "get library", "no such library") {
		return
	}
	roots, err := s.st.ListRoots(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "list roots")
		return
	}
	writeJSON(w, http.StatusOK, roots)
}

// addRoot gives a library another location. Admin-only.
func (s *Server) addRoot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "path is required")
		return
	}
	abs, msg := checkDirectory(req.Path)
	if msg != "" {
		writeError(w, http.StatusBadRequest, "bad_request", msg)
		return
	}

	lib, err := s.st.GetLibrary(r.Context(), id)
	if s.notFoundOr(w, err, "get library", "no such library") {
		return
	}

	root, err := s.st.AddRoot(r.Context(), id, abs)
	if s.writeRootErr(w, err, "add root") {
		return
	}
	s.audit(r, "library.update", "library", auditID(id),
		fmt.Sprintf("Added location %s to library %q", abs, lib.Name),
		map[string]any{"root": abs, "root_id": root.ID})
	writeJSON(w, http.StatusCreated, root)
}

/*
 * removeRoot drops a location and everything scanned under it. Admin-only.
 *
 * This deletes rows, where an unreachable drive marks them missing, and the
 * difference is deliberate: "scanning marks missing, never deletes" governs
 * what the server may *infer*, not what an administrator may ask for. A scan
 * deduces absence from not finding a file and can be wrong about it; this is
 * somebody saying the location is not part of the library any more.
 *
 * The count goes in the audit line because it is the part that surprises after
 * the fact — removing a location takes its watch history with it.
 */
func (s *Server) removeRoot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return
	}
	rootID, ok := rootPathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid location id")
		return
	}

	root, err := s.st.GetRoot(r.Context(), rootID)
	if s.notFoundOr(w, err, "get root", "no such location") {
		return
	}
	// A location id from another library is a not-found rather than a
	// forbidden: the caller has no business knowing it exists.
	if root.LibraryID != id {
		writeError(w, http.StatusNotFound, "not_found", "no such location")
		return
	}

	n, err := s.st.CountItemsInRoot(r.Context(), rootID)
	if err != nil {
		s.writeInternal(w, err, "count items in root")
		return
	}
	if err := s.st.RemoveRoot(r.Context(), rootID); s.writeRootErr(w, err, "remove root") {
		return
	}
	s.audit(r, "library.update", "library", auditID(id),
		fmt.Sprintf("Removed location %s and %d item(s)", root.Path, n),
		map[string]any{"root": root.Path, "root_id": rootID, "items": n})
	w.WriteHeader(http.StatusNoContent)
}

// patchRoot moves one location, carrying its contents with it. Admin-only.
//
// The drive-letter case, per location rather than per library: a library with
// two locations has one of them move while the other stays exactly where it is.
// The rows move with it and nothing is marked missing — a rescan afterwards
// reconciles files as it always does.
func (s *Server) patchRoot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return
	}
	rootID, ok := rootPathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid location id")
		return
	}
	var req struct {
		Path *string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	if req.Path == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "path is required")
		return
	}

	root, err := s.st.GetRoot(r.Context(), rootID)
	if s.notFoundOr(w, err, "get root", "no such location") {
		return
	}
	if root.LibraryID != id {
		writeError(w, http.StatusNotFound, "not_found", "no such location")
		return
	}

	abs, msg := checkDirectory(*req.Path)
	if msg != "" {
		writeError(w, http.StatusBadRequest, "bad_request", msg)
		return
	}
	if abs == root.Path {
		writeJSON(w, http.StatusOK, root)
		return
	}

	if err := s.st.RepointRoot(r.Context(), rootID, abs); s.writeRootErr(w, err, "repoint root") {
		return
	}
	s.audit(r, "library.update", "library", auditID(id),
		fmt.Sprintf("Moved location %s to %s", root.Path, abs),
		map[string]any{"from": root.Path, "to": abs, "root_id": rootID})

	updated, err := s.st.GetRoot(r.Context(), rootID)
	if err != nil {
		s.writeInternal(w, err, "get root")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}
