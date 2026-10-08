package api

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lancast/internal/retro/romhash"
	"lancast/internal/retro/saves"
	"lancast/internal/store"
)

/*
 * What the desktop player needs from the server to run a game (ADR 0073,
 * stage 2): every file the game is made of, and the person's saves.
 *
 * Reachable with a session, and with a stream ticket minted for the game
 * (streamticket.go) — which is how the player reaches them, since it holds no
 * cookie. Not reachable by a guest or a paired server: ROM libraries are not
 * shared, and a save is one person's.
 */

// romFor resolves the game a request names, with the caller's visibility, or
// writes the error and returns nil.
func (s *Server) romFor(w http.ResponseWriter, r *http.Request) *store.Item {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return nil
	}
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return nil
	}
	if it.Kind != "rom" {
		writeError(w, http.StatusNotFound, "not_found", "not a game")
		return nil
	}
	return it
}

// RomFile is one file a game is made of, named relative to its entry file.
type RomFile struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	// Present is false for a file the game names that is not on disk — a
	// cue's missing track. Listed rather than dropped, so the player can say
	// which file is missing instead of failing inside the core.
	Present bool `json:"present"`
}

// gameFiles resolves a game's files, each re-verified inside the library
// location the game was scanned under.
func (s *Server) gameFiles(r *http.Request, it *store.Item) (dir string, files []string, err error) {
	entry, err := s.itemFilePath(r, it)
	if err != nil {
		return "", nil, err
	}
	root, err := s.st.RootForItem(r.Context(), it.ID)
	if err != nil {
		return "", nil, err
	}
	all, err := romhash.GameFiles(entry)
	if err != nil {
		return "", nil, err
	}
	for _, f := range all {
		// GameFiles already keeps every file inside the entry's folder; this
		// is the boundary check every row-to-path handler makes, made again.
		if p, err := containedPath(root.Path, f); err == nil {
			files = append(files, p)
		}
	}
	return filepath.Dir(entry), files, nil
}

func relName(dir, p string) string {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return filepath.Base(p)
	}
	return filepath.ToSlash(rel)
}

// listGameFiles answers GET /api/items/{id}/files.
func (s *Server) listGameFiles(w http.ResponseWriter, r *http.Request) {
	it := s.romFor(w, r)
	if it == nil {
		return
	}
	dir, files, err := s.gameFiles(r, it)
	if err != nil {
		s.log.Warn("game files", "item", it.ID, "error", err)
		writeError(w, http.StatusServiceUnavailable, "unavailable", "the game's files cannot be read")
		return
	}
	out := make([]RomFile, 0, len(files))
	for _, f := range files {
		rf := RomFile{Name: relName(dir, f)}
		if st, err := os.Stat(f); err == nil && !st.IsDir() {
			rf.SizeBytes, rf.Present = st.Size(), true
		}
		out = append(out, rf)
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": out})
}

// streamGameFile answers GET /api/stream/{id}/files?name=: one file of a
// game, by the name the listing gave it and by no other. A query parameter
// rather than a path, because the names carry slashes (".hidden/Disc 1.cue").
func (s *Server) streamGameFile(w http.ResponseWriter, r *http.Request) {
	it := s.romFor(w, r)
	if it == nil {
		return
	}
	want := r.URL.Query().Get("name")
	dir, files, err := s.gameFiles(r, it)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such file")
		return
	}
	for _, f := range files {
		if relName(dir, f) != want {
			continue
		}
		fh, err := os.Open(f)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "file is missing from disk")
			return
		}
		defer fh.Close()
		info, err := fh.Stat()
		if err != nil || info.IsDir() {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "file is missing from disk")
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, filepath.Base(f), info.ModTime(), fh)
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "no such file")
}

func (s *Server) saveFiles() saves.Files {
	return saves.Files{Dir: filepath.Join(s.dataDir, "saves")}
}

// listSaves answers GET /api/items/{id}/saves: the caller's own saves.
func (s *Server) listSaves(w http.ResponseWriter, r *http.Request) {
	it := s.romFor(w, r)
	if it == nil {
		return
	}
	list, err := s.st.ROMSaves(r.Context(), s.userID(r), it.ID)
	if err != nil {
		s.writeInternal(w, err, "rom saves")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saves": list})
}

// getSave answers GET /api/items/{id}/saves/{slot}. `?previous=1` returns
// the copy the current one replaced.
func (s *Server) getSave(w http.ResponseWriter, r *http.Request) {
	it := s.romFor(w, r)
	if it == nil {
		return
	}
	slot := r.PathValue("slot")
	if !saves.ValidSlot(slot) {
		writeError(w, http.StatusBadRequest, "bad_request", "no such slot")
		return
	}
	previous := r.URL.Query().Get("previous") == "1"
	meta, err := s.st.GetROMSave(r.Context(), s.userID(r), it.ID, slot)
	if errors.Is(err, store.ErrNotFound) || (err == nil && previous && meta.Previous == nil) {
		writeError(w, http.StatusNotFound, "not_found", "no save in this slot")
		return
	}
	if err != nil {
		s.writeInternal(w, err, "rom save")
		return
	}
	f, err := s.saveFiles().Open(s.userID(r), it.ID, slot, previous)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no save in this slot")
		return
	}
	defer f.Close()
	if _, err := f.Stat(); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no save in this slot")
		return
	}
	sha, core, ver, at := meta.SHA256, meta.Core, meta.CoreVersion, meta.UpdatedAt
	if previous {
		sha, core, ver, at = meta.Previous.SHA256, meta.Previous.Core, meta.Previous.CoreVersion, meta.Previous.UpdatedAt
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("ETag", `"`+sha+`"`)
	h.Set("Cache-Control", "no-store")
	if core != "" {
		h.Set("X-LANcast-Core", core)
		h.Set("X-LANcast-Core-Version", ver)
	}
	http.ServeContent(w, r, slot, time.Unix(at, 0), f)
}

/*
 * putSave answers PUT /api/items/{id}/saves/{slot}.
 *
 * The body is the save's bytes. A save state names the core and version that
 * wrote it (`?core=&core_version=`), because loading it into anything else is
 * a crash rather than a refusal; save RAM needs neither. The newer write wins
 * and the one it replaces is kept — the ADR's whole conflict policy, and
 * deliberately no more, since two machines saving one slot at the same moment
 * is not how one household plays.
 */
func (s *Server) putSave(w http.ResponseWriter, r *http.Request) {
	it := s.romFor(w, r)
	if it == nil {
		return
	}
	slot := r.PathValue("slot")
	if !saves.ValidSlot(slot) {
		writeError(w, http.StatusBadRequest, "bad_request", "no such slot")
		return
	}
	core := strings.TrimSpace(r.URL.Query().Get("core"))
	ver := strings.TrimSpace(r.URL.Query().Get("core_version"))
	if saves.IsState(slot) && (core == "" || ver == "") {
		writeError(w, http.StatusBadRequest, "bad_request",
			"a save state must name the core and core_version that wrote it")
		return
	}
	if len(core) > 64 || len(ver) > 64 {
		writeError(w, http.StatusBadRequest, "bad_request", "core names are short")
		return
	}
	user := s.userID(r)
	written, err := s.saveFiles().Write(user, it.ID, slot, http.MaxBytesReader(w, r.Body, saves.MaxBytes+1))
	if errors.Is(err, saves.ErrTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "a save is never this large")
		return
	}
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "a save is never this large")
		return
	}
	if err != nil && !errors.Is(err, io.EOF) {
		s.writeInternal(w, err, "write save")
		return
	}
	sv := store.ROMSave{
		Slot: slot, SizeBytes: written.Size, SHA256: written.SHA256, UpdatedAt: time.Now().Unix(),
	}
	if saves.IsState(slot) {
		sv.Core, sv.CoreVersion = core, ver
	}
	if err := s.st.PutROMSave(r.Context(), user, it.ID, sv); err != nil {
		s.writeInternal(w, err, "record save")
		return
	}
	got, err := s.st.GetROMSave(r.Context(), user, it.ID, slot)
	if err != nil {
		s.writeInternal(w, err, "read save")
		return
	}
	writeJSON(w, http.StatusOK, got)
}
