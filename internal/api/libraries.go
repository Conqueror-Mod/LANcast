package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"lancast/internal/scan"
)

func (s *Server) listLibraries(w http.ResponseWriter, r *http.Request) {
	libs, err := s.st.ListLibraries(r.Context())
	if err != nil {
		s.writeInternal(w, err, "list libraries")
		return
	}
	writeJSON(w, http.StatusOK, libs)
}

var validKinds = map[string]bool{
	"movie": true, "show": true, "music": true, "picture": true, "other": true,
}

func (s *Server) createLibrary(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Path = strings.TrimSpace(req.Path)
	if req.Name == "" || req.Path == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "name and path are required")
		return
	}
	if !validKinds[req.Kind] {
		writeError(w, http.StatusBadRequest, "bad_request", "kind must be one of movie, show, music, picture, other")
		return
	}

	abs, err := filepath.Abs(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "path could not be resolved")
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "path does not exist or is unreadable")
		return
	}
	if !info.IsDir() {
		writeError(w, http.StatusBadRequest, "bad_request", "path is not a directory")
		return
	}

	lib, err := s.st.CreateLibrary(r.Context(), req.Name, req.Kind, abs)
	if err != nil {
		// The unique index on path is the only realistic conflict here.
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, "conflict", "a library already uses that path")
			return
		}
		s.writeInternal(w, err, "create library")
		return
	}
	s.audit(r, "library.create", "library", auditID(lib.ID),
		fmt.Sprintf("Added %s library %q at %s", lib.Kind, lib.Name, lib.Path),
		map[string]any{"path": lib.Path, "kind": lib.Kind})
	writeJSON(w, http.StatusCreated, lib)
}

func (s *Server) startScan(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return
	}
	lib, err := s.st.GetLibrary(r.Context(), id)
	if s.notFoundOr(w, err, "get library", "no such library") {
		return
	}

	p, err := s.scanner.Start(*lib)
	if errors.Is(err, scan.ErrBusy) {
		writeJSON(w, http.StatusConflict, p)
		return
	}
	if err != nil {
		s.writeInternal(w, err, "start scan")
		return
	}
	writeJSON(w, http.StatusAccepted, p)
}

func (s *Server) scanStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return
	}
	if _, err := s.st.GetLibrary(r.Context(), id); s.notFoundOr(w, err, "get library", "no such library") {
		return
	}
	writeJSON(w, http.StatusOK, s.scanner.Status(id))
}

// deleteLibrary forgets a library: its rows and everything cascading off them.
// It never deletes media from disk — LANcast only stored paths — so this is a
// safe "stop tracking this folder", not a destroy.
func (s *Server) deleteLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return
	}
	lib, err := s.st.GetLibrary(r.Context(), id)
	if s.notFoundOr(w, err, "get library", "no such library") {
		return
	}
	// A scan mid-write against this library must not have it deleted out from
	// under it. Make the caller wait rather than risk a torn state.
	if s.scanner.Status(id).State == scan.StateRunning {
		writeError(w, http.StatusConflict, "conflict",
			"a scan is running; wait for it to finish before removing the library")
		return
	}
	if err := s.st.DeleteLibrary(r.Context(), id); s.notFoundOr(w, err, "delete library", "no such library") {
		return
	}
	// The event this whole log exists for. Read before the delete so the
	// summary still names what is now gone (ADR 0026).
	s.audit(r, "library.delete", "library", auditID(id),
		fmt.Sprintf("Removed library %q (%d items) — files left on disk", lib.Name, lib.ItemCount),
		map[string]any{"path": lib.Path, "kind": lib.Kind, "item_count": lib.ItemCount})
	w.WriteHeader(http.StatusNoContent)
}
