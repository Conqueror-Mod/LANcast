package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"lancast/internal/media"
	"lancast/internal/store"
)

// sidecarExt is the set of companion-file extensions a "delete from disk"
// removes alongside a video — subtitles, the Kodi .nfo, and artwork.
var sidecarExt = map[string]bool{
	".srt": true, ".vtt": true, ".ass": true, ".ssa": true, ".sub": true,
	".idx": true, ".nfo": true, ".tbn": true,
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
}

// associatedSidecars returns the companion files that belong to one video —
// its subtitles, .nfo, and artwork — so deleting the video does not strand
// leftovers. It is deliberately narrow: a file counts only when its name is the
// video's stem followed by a '.' or '-' separator (never a space), so a
// sibling's files — "Show S01E02.srt" beside "Show S01E01.mkv", "Part 2.nfo"
// beside "Part 1.mkv" — are left untouched, and a folder-level "poster.jpg"
// shared by the whole folder is never swept. The boundary check is stricter
// than the scanner's subtitle matching on purpose: attaching a stray subtitle
// for display is harmless, deleting the wrong file is not.
func associatedSidecars(videoPath string) []string {
	dir := filepath.Dir(videoPath)
	base := filepath.Base(videoPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == base || !strings.HasPrefix(name, stem) {
			continue
		}
		rest := name[len(stem):]
		if rest == "" || (rest[0] != '.' && rest[0] != '-') {
			// A different title's file ("Part 1" vs "Part 10", "…E01" vs "…E02")
			// or an unrelated name — never touched.
			continue
		}
		if media.IsVideo(name) {
			continue
		}
		if sidecarExt[strings.ToLower(filepath.Ext(name))] {
			out = append(out, filepath.Join(dir, name))
		}
	}
	return out
}

// deleteItem removes a title from the library. mode decides what happens to the
// files on disk:
//
//   - ignore: the files stay on disk; their paths are added to the ignore list
//     so a rescan never re-adds them. Non-destructive.
//   - delete: the files are removed from disk. Each is containment-checked
//     against its library root first (a bad row must never delete outside the
//     library), and a file already gone is not an error.
//
// A container (show, work) removes its whole subtree — every episode or part.
// A collection removes only the grouping, never the member films. Admin-only.
func (s *Server) deleteItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode != "ignore" && mode != "delete" {
		writeError(w, http.StatusBadRequest, "bad_request", "mode must be 'ignore' or 'delete'")
		return
	}
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	lib, err := s.st.GetLibrary(r.Context(), it.LibraryID)
	if err != nil {
		s.writeInternal(w, err, "get library")
		return
	}
	targets, err := s.st.ItemSubtree(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "item subtree")
		return
	}

	var files []string
	var rowIDs []int64
	for _, t := range targets {
		rowIDs = append(rowIDs, t.ID)
		if t.IsFile && t.Path != "" {
			files = append(files, t.Path)
		}
	}

	if mode == "delete" {
		// Verify every path is inside the library before removing anything, so a
		// single bad row cannot delete a file outside the library and a partial
		// delete is avoided.
		// Expand each video to itself plus its companion files (subtitles, nfo,
		// artwork), so a delete does not leave leftovers behind.
		toRemove := make([]string, 0, len(files)*2)
		for _, f := range files {
			toRemove = append(toRemove, f)
			toRemove = append(toRemove, associatedSidecars(f)...)
		}
		abs := make([]string, 0, len(toRemove))
		for _, f := range toRemove {
			a, err := containedPath(lib.Path, f)
			if err != nil {
				s.log.Error("delete containment check failed", "item", id, "path", f, "error", err)
				writeError(w, http.StatusInternalServerError, "internal", "a file path escaped its library; nothing was deleted")
				return
			}
			abs = append(abs, a)
		}
		for _, a := range abs {
			if err := os.Remove(a); err != nil && !os.IsNotExist(err) {
				s.log.Warn("delete file failed", "path", a, "error", err)
			}
		}
	} else {
		if err := s.st.IgnorePaths(r.Context(), it.LibraryID, files); err != nil {
			s.writeInternal(w, err, "ignore paths")
			return
		}
	}

	if err := s.st.DeleteItems(r.Context(), rowIDs); err != nil {
		s.writeInternal(w, err, "delete items")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// libraryFacets returns the genres and decades a library's browse view can
// filter by — only values actually present, so a filter never yields nothing.
func (s *Server) libraryFacets(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return
	}
	if _, err := s.st.GetLibrary(r.Context(), id); s.notFoundOr(w, err, "get library", "no such library") {
		return
	}
	facets, err := s.st.LibraryFacets(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "library facets")
		return
	}
	writeJSON(w, http.StatusOK, facets)
}

func (s *Server) listItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// collection_id returns a collection's members, which live in a join table
	// rather than parent_id — so it takes its own path, not the media_item
	// filter below (ADR 0017).
	if v := q.Get("collection_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid collection_id")
			return
		}
		items, err := s.st.CollectionMembers(r.Context(), id)
		if err != nil {
			s.writeInternal(w, err, "collection members")
			return
		}
		s.decorateAndWriteItems(w, r, items)
		return
	}

	f := store.ItemFilter{
		LibraryID: int64(queryInt(r, "library_id")),
		Kind:      q.Get("kind"),
		Query:     q.Get("q"),
		Sort:      q.Get("sort"),
		Genre:     q.Get("genre"),
		Decade:    queryInt(r, "decade"),
		Limit:     queryInt(r, "limit"),
		Offset:    queryInt(r, "offset"),
	}
	// parent_id fetches the children of one item — a show's episodes, a work's
	// parts. Otherwise the grid shows top-level entries only, so a container's
	// children never leak in loose (ADR 0010, ADR 0017). An explicit kind is
	// treated as a deliberate cross-cutting query and is not forced top-level.
	if v := q.Get("parent_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.ParentID = &id
		} else {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid parent_id")
			return
		}
	} else if f.Kind == "" {
		f.TopLevel = true
	}

	items, total, err := s.st.ListItems(r.Context(), f)
	if err != nil {
		s.writeInternal(w, err, "list items")
		return
	}
	s.decorateAndWriteItems(w, r, items, total)
}

// decorateAndWriteItems attaches the per-user and grid data every item listing
// needs — progress, artwork, and child counts — then writes the page. total is
// the count for a paged query; pass -1 for a whole set (a collection's members),
// where the response reports len(items).
func (s *Server) decorateAndWriteItems(w http.ResponseWriter, r *http.Request, items []store.Item, total ...int) {
	if err := s.st.AttachProgress(r.Context(), items, s.userID(r)); err != nil {
		s.writeInternal(w, err, "attach progress")
		return
	}
	// The grid renders from this response, so posters have to come with it.
	if err := s.st.AttachArtwork(r.Context(), items); err != nil {
		s.writeInternal(w, err, "attach artwork")
		return
	}
	// So a tile knows whether it is a container (a show, a collection, a
	// multi-part work) and should open a children view rather than offer Play.
	if err := s.st.AttachChildCounts(r.Context(), items); err != nil {
		s.writeInternal(w, err, "attach child counts")
		return
	}
	n := len(items)
	if len(total) > 0 {
		n = total[0]
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": n, "items": items})
}

// continueWatching lists the user's in-progress items, most recently played
// first — the home screen's first shelf. Progress is included so tiles can draw
// their resume bar without a second call.
func (s *Server) continueWatching(w http.ResponseWriter, r *http.Request) {
	items, err := s.st.ContinueWatching(r.Context(), s.userID(r), queryInt(r, "limit"))
	if err != nil {
		s.writeInternal(w, err, "continue watching")
		return
	}
	if err := s.st.AttachArtwork(r.Context(), items); err != nil {
		s.writeInternal(w, err, "attach artwork")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) getItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	if _, err := s.st.GetItem(r.Context(), id, s.userID(r)); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	s.respondItem(w, r, id)
}

func (s *Server) putProgress(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	var req struct {
		PositionMS int64 `json:"position_ms"`
		Watched    bool  `json:"watched"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	if req.PositionMS < 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "position_ms must not be negative")
		return
	}

	if _, err := s.st.GetItem(r.Context(), id, s.userID(r)); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	if err := s.st.SaveProgress(r.Context(), id, s.userID(r), req.PositionMS, req.Watched); err != nil {
		s.writeInternal(w, err, "save progress")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
