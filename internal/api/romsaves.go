package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lancast/internal/retro/romhash"
	"lancast/internal/retro/saves"
	"lancast/internal/sharedopen"
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
		fh, err := sharedopen.Open(f)
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
 *
 * With one exception, which is about a player rather than a household
 * (R12-4). `?based_on=` names the save the writer loaded: its SHA-256 (the
 * ETag a GET gave), or `none` for an empty slot. When the slot holds anything
 * else, the answer is 409 and nothing changes. A player that could not fetch
 * the save, or that started from one another machine has since replaced, would
 * otherwise send blank or stale save RAM over the real one — many cartridges
 * write theirs at boot — and a second write would rotate the real one out of
 * `.prev` as well. Without the parameter a write is unconditional, as before.
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
	basedOn := r.URL.Query().Get("based_on")
	if !saves.ValidBasedOn(basedOn) {
		writeError(w, http.StatusBadRequest, "bad_request",
			"based_on is the SHA-256 of the save that was loaded, or none")
		return
	}
	user := s.userID(r)
	/*
	 * The row is written as part of the write, and on a context the request
	 * cannot cancel (R03-5). On the request's own context a player that
	 * closed its connection after sending the body — a save on exit — failed
	 * the row after the files had rotated: a first save sat on disk with no
	 * row, so the slot answered 404 and the game started without it, and a
	 * later one was served under a row describing the bytes before it. Now
	 * the row is not abandoned with the request, and if it still cannot be
	 * written the files are put back as they were.
	 */
	rowCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	now := time.Now().Unix()
	_, err := s.saveFiles().WriteIfRecorded(user, it.ID, slot, basedOn,
		http.MaxBytesReader(w, r.Body, saves.MaxBytes+1),
		func(written saves.Written) error {
			sv := store.ROMSave{
				Slot: slot, SizeBytes: written.Size, SHA256: written.SHA256, UpdatedAt: now,
			}
			if saves.IsState(slot) {
				sv.Core, sv.CoreVersion = core, ver
			}
			return s.st.PutROMSave(rowCtx, user, it.ID, sv)
		})
	if errors.Is(err, saves.ErrConflict) {
		writeError(w, http.StatusConflict, "conflict",
			"the save on the server changed since this one was loaded")
		return
	}
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
	got, err := s.st.GetROMSave(rowCtx, user, it.ID, slot)
	if err != nil {
		s.writeInternal(w, err, "read save")
		return
	}
	writeJSON(w, http.StatusOK, got)
}
