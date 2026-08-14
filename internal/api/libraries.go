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
		// Roots is the multi-location form (ADR 0034). `path` remains accepted
		// and means the same as a single-element roots array, so every client
		// that predates this keeps working unchanged.
		Roots []string `json:"roots"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Path = strings.TrimSpace(req.Path)

	// One list, however it was spelled. Both together is accepted rather than
	// refused, with `path` first: a client migrating from one to the other
	// sending both means the same library either way.
	wanted := []string{}
	if req.Path != "" {
		wanted = append(wanted, req.Path)
	}
	for _, p := range req.Roots {
		if t := strings.TrimSpace(p); t != "" {
			wanted = append(wanted, t)
		}
	}
	if req.Name == "" || len(wanted) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "name and at least one path are required")
		return
	}
	if !validKinds[req.Kind] {
		writeError(w, http.StatusBadRequest, "bad_request", "kind must be one of movie, show, music, picture, other")
		return
	}

	// Every path is checked before any of them is written, so a typo in the
	// third location does not leave a half-made library behind.
	abs := make([]string, 0, len(wanted))
	for _, p := range wanted {
		a, msg := checkDirectory(p)
		if msg != "" {
			writeError(w, http.StatusBadRequest, "bad_request", msg)
			return
		}
		abs = append(abs, a)
	}

	lib, err := s.st.CreateLibrary(r.Context(), req.Name, req.Kind, abs[0])
	if err != nil {
		// The unique index on path is the only realistic conflict here.
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, "conflict", "a library already uses that path")
			return
		}
		s.writeInternal(w, err, "create library")
		return
	}

	// The remaining locations. A failure here leaves a library that exists with
	// fewer locations than asked for, so it is removed again rather than left
	// half-configured — a partially created library is the kind of thing
	// somebody scans without noticing.
	for _, a := range abs[1:] {
		if _, err := s.st.AddRoot(r.Context(), lib.ID, a); err != nil {
			if delErr := s.st.DeleteLibrary(r.Context(), lib.ID); delErr != nil {
				s.log.Error("could not roll back a partial library",
					"library", lib.ID, "error", delErr)
			}
			if s.writeRootErr(w, err, "add root") {
				return
			}
			return
		}
	}

	created, err := s.st.GetLibrary(r.Context(), lib.ID)
	if err != nil {
		s.writeInternal(w, err, "get library")
		return
	}
	s.audit(r, "library.create", "library", auditID(lib.ID),
		fmt.Sprintf("Added %s library %q at %s", lib.Kind, lib.Name, strings.Join(abs, ", ")),
		map[string]any{"path": abs[0], "roots": abs, "kind": lib.Kind})
	writeJSON(w, http.StatusCreated, created)
}

// patchLibrary edits a library. Admin-only.
//
// Everything is editable except the kind. A kind is not a label — it decides
// which scanner runs, which metadata provider is asked, what the top level of
// the browse is, and what a file even counts as. Changing it would not convert
// a library, it would leave one describing itself as something its rows are
// not; the honest way to change a kind is to add the library again as the kind
// you meant.
//
// The path is editable because the drive-letter case is real and the
// alternative is deleting a library and losing every match, every piece of
// artwork, every watch position and every playlist that referenced it — to
// record a fact about a drive letter. See store.RepointLibrary: the rows move
// with it, and a rescan afterwards reconciles files exactly as it always does.
func (s *Server) patchLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return
	}
	var req struct {
		Name *string `json:"name"`
		Path *string `json:"path"`
		Kind *string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}

	lib, err := s.st.GetLibrary(r.Context(), id)
	if s.notFoundOr(w, err, "get library", "no such library") {
		return
	}

	// Refused rather than ignored. A client that sends a kind believes it is
	// changing one, and silently dropping it would leave the caller thinking a
	// library had been converted.
	if req.Kind != nil && *req.Kind != lib.Kind {
		writeError(w, http.StatusBadRequest, "bad_request",
			"a library's type cannot be changed; add the folder again as the type you want")
		return
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "name cannot be empty")
			return
		}
		if name != lib.Name {
			if err := s.st.RenameLibrary(r.Context(), id, name); err != nil {
				s.writeInternal(w, err, "rename library")
				return
			}
			s.audit(r, "library.update", "library", auditID(id),
				"Renamed library "+lib.Name+" to "+name, nil)
		}
	}

	if req.Path != nil {
		abs, err := filepath.Abs(strings.TrimSpace(*req.Path))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "path could not be resolved")
			return
		}
		if abs != lib.Path {
			// Checked before anything is rewritten: repointing a library at a
			// folder that is not there would mark its whole contents missing on
			// the next scan, which is a long way to travel for a typo.
			info, err := os.Stat(abs)
			if err != nil {
				writeError(w, http.StatusBadRequest, "bad_request", "path does not exist or is unreadable")
				return
			}
			if !info.IsDir() {
				writeError(w, http.StatusBadRequest, "bad_request", "path is not a directory")
				return
			}
			if err := s.st.RepointLibrary(r.Context(), id, lib.Path, abs); err != nil {
				if strings.Contains(err.Error(), "UNIQUE") {
					writeError(w, http.StatusConflict, "conflict", "another library already uses that path")
					return
				}
				s.writeInternal(w, err, "repoint library")
				return
			}
			s.audit(r, "library.update", "library", auditID(id),
				"Moved library "+lib.Name+" from "+lib.Path+" to "+abs,
				map[string]any{"from": lib.Path, "to": abs})
		}
	}

	updated, err := s.st.GetLibrary(r.Context(), id)
	if s.notFoundOr(w, err, "get library", "no such library") {
		return
	}
	writeJSON(w, http.StatusOK, updated)
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
