package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	// Deleting files can be switched off for the whole server. Checked before
	// anything is read, so a server that does not delete media never even looks
	// up what it would have deleted. mode=ignore is untouched: it writes no
	// file and removes nothing from disk.
	if mode == "delete" && !s.settings.Get().AllowMediaDeletion {
		writeError(w, http.StatusForbidden, "forbidden",
			"this server does not allow deleting media files; remove from library instead")
		return
	}

	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	targets, err := s.st.ItemSubtree(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "item subtree")
		return
	}

	// The item id travels with the path (ADR 0034).
	//
	// A subtree can span locations — a show whose later seasons live on a
	// second drive is the ordinary case this is for — so "which root contains
	// this file" has a different answer per file, and a flat list of paths
	// cannot ask it. Losing the pairing here is how a delete would end up
	// validating one location's file against another location's root.
	type fileTarget struct {
		itemID int64
		path   string
	}
	var files []fileTarget
	var rowIDs []int64
	for _, t := range targets {
		rowIDs = append(rowIDs, t.ID)
		if t.IsFile && t.Path != "" {
			files = append(files, fileTarget{itemID: t.ID, path: t.Path})
		}
	}

	if mode == "delete" {
		// Verify every path is inside the library before removing anything, so a
		// single bad row cannot delete a file outside the library and a partial
		// delete is avoided.
		// Expand each video to itself plus its companion files (subtitles, nfo,
		// artwork), so a delete does not leave leftovers behind.
		abs := make([]string, 0, len(files)*2)
		for _, ft := range files {
			// Resolved per file, against the location that file was scanned
			// under. A sidecar sits beside its video, so it is checked against
			// the same root — deriving it from the video's path is what makes
			// that true rather than assumed.
			root, err := s.st.RootForItem(r.Context(), ft.itemID)
			if err != nil {
				s.log.Error("delete root lookup failed", "item", ft.itemID, "error", err)
				writeError(w, http.StatusInternalServerError, "internal", "a file could not be resolved to a library location; nothing was deleted")
				return
			}
			for _, f := range append([]string{ft.path}, associatedSidecars(ft.path)...) {
				a, err := containedPath(root.Path, f)
				if err != nil {
					s.log.Error("delete containment check failed", "item", ft.itemID, "path", f, "error", err)
					writeError(w, http.StatusInternalServerError, "internal", "a file path escaped its library; nothing was deleted")
					return
				}
				abs = append(abs, a)
			}
		}
		for _, a := range abs {
			if err := os.Remove(a); err != nil && !os.IsNotExist(err) {
				s.log.Warn("delete file failed", "path", a, "error", err)
			}
		}
	} else {
		// Ignoring records paths, not locations: the list is library-scoped and
		// a path identifies itself.
		paths := make([]string, 0, len(files))
		for _, ft := range files {
			paths = append(paths, ft.path)
		}
		if err := s.st.IgnorePaths(r.Context(), it.LibraryID, paths); err != nil {
			s.writeInternal(w, err, "ignore paths")
			return
		}
	}

	if err := s.st.DeleteItems(r.Context(), rowIDs); err != nil {
		s.writeInternal(w, err, "delete items")
		return
	}
	// "Ignore" and "delete" are very different acts and the log must not blur
	// them: one is reversible by clearing the ignore list, the other destroyed
	// files. Row count is included because removing a container takes its
	// children with it, which is the surprising part after the fact.
	verb := "Stopped tracking"
	if mode == "delete" {
		verb = "Deleted from disk"
	}
	s.audit(r, "item.delete", "item", auditID(id),
		fmt.Sprintf("%s %q (%d row(s), %d file(s))", verb, it.Title, len(rowIDs), len(files)),
		map[string]any{"mode": mode, "kind": it.Kind, "rows": len(rowIDs), "files": len(files)})
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
	facets, err := s.st.LibraryFacets(r.Context(), id, s.userID(r))
	if err != nil {
		s.writeInternal(w, err, "library facets")
		return
	}
	writeJSON(w, http.StatusOK, facets)
}

/*
 * libraryCast answers the type-ahead behind the Cast filter.
 *
 * A search endpoint rather than another array on /facets, because the two
 * differ by three orders of magnitude: a library has a dozen genres and
 * thousands of credited people, and shipping all of them on every browse load
 * would be a megabyte of JSON to populate a control most visits never open.
 *
 * Not admin-gated. It reads names already visible on every item detail page of
 * a library the caller can browse, and the containment check that matters here
 * is the library id, which GetLibrary performs.
 */
func (s *Server) libraryCast(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return
	}
	if _, err := s.st.GetLibrary(r.Context(), id); s.notFoundOr(w, err, "get library", "no such library") {
		return
	}
	/*
	 * `id` resolves specific people instead of searching.
	 *
	 * Filter state lives in the URL, so a bookmarked `?person=12` arrives with
	 * an id and no name — and a filter pill reading "person 12" is not one
	 * anybody can read. Answering by id lets the pill render without the search
	 * panel ever having been opened.
	 */
	if ids, ok := parseInt64s(r.URL.Query()["id"]); len(ids) > 0 {
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid id")
			return
		}
		names, err := s.st.CastNames(r.Context(), ids)
		if err != nil {
			s.writeInternal(w, err, "cast names")
			return
		}
		// Emitted in the order asked for, so pills do not reorder themselves
		// between reloads. A missing id is skipped rather than rendered as a
		// blank pill: the person was deleted with their last item.
		people := make([]store.CastMember, 0, len(ids))
		for _, pid := range ids {
			if name, ok := names[pid]; ok {
				people = append(people, store.CastMember{ID: pid, Name: name})
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"people": people})
		return
	}

	/*
	 * `role` scopes the search to one side of the camera.
	 *
	 * Unvalidated on purpose: an unknown role matches nobody and returns an
	 * empty list, which is the truthful answer to "who directed this, filtered
	 * to gaffers". Rejecting it would turn a filter nobody can satisfy into an
	 * error page.
	 */
	people, err := s.st.SearchCast(r.Context(), id, strings.TrimSpace(r.URL.Query().Get("q")),
		r.URL.Query().Get("role"), queryInt(r, "limit"))
	if err != nil {
		s.writeInternal(w, err, "library cast")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": people})
}

// nonEmpty drops blank entries from a repeated query parameter, so a stray
// "&genre=" never becomes a filter for the empty string.
func nonEmpty(vs []string) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		if v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseInts parses a repeatable integer parameter. Blank entries are skipped,
// so a trailing "&year=" widens nothing; a non-numeric one is an error, because
// silently dropping it would show a library the caller did not ask for.
func parseInts(vs []string) ([]int, bool) {
	out := make([]int, 0, len(vs))
	for _, v := range vs {
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, true
	}
	return out, true
}

// parseInt64s is parseInts for row ids.
func parseInt64s(vs []string) ([]int64, bool) {
	out := make([]int64, 0, len(vs))
	for _, v := range vs {
		if v == "" {
			continue
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, true
	}
	return out, true
}

// parseDecades parses the repeatable decade parameter. A blank entry is skipped;
// a non-numeric one is an error, so a malformed filter is reported rather than
// silently widening the grid.
func parseDecades(vs []string) ([]int, bool) {
	out := make([]int, 0, len(vs))
	for _, v := range vs {
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, true
	}
	return out, true
}

// splitCSV reads a comma-separated query value, dropping empties so a trailing
// comma or a bare "," cannot turn into a filter on the empty kind.
func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	out := []string{}
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
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

	// playlist_id returns a playlist's entries, in playing order. Its own path
	// for the same reason collection_id has one, and one more besides: a
	// playlist may hold the same track twice (ADR 0030), so this is the only
	// listing in the API that can return the same item id more than once. A
	// caller keying on id — a React list, a map — must key on position instead.
	if v := q.Get("playlist_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid playlist_id")
			return
		}
		items, err := s.st.PlaylistEntries(r.Context(), id)
		if err != nil {
			s.writeInternal(w, err, "playlist entries")
			return
		}
		s.decorateAndWriteItems(w, r, items)
		return
	}

	// Genre, decade, and content_rating are repeatable: ?genre=A&genre=B widens
	// within a facet. Empty values are dropped so a trailing "&genre=" is a
	// no-op rather than a filter for the empty string.
	decades, ok := parseDecades(q["decade"])
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid decade")
		return
	}
	years, ok := parseInts(q["year"])
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid year")
		return
	}
	// A person filter that cannot be parsed is an error rather than a widening,
	// unlike a resolution key: an id is machine-generated, so a malformed one
	// means the caller is confused, and silently showing the whole library
	// would look like the person matched everything.
	people, ok := parseInt64s(q["person"])
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid person")
		return
	}
	actors, ok := parseInt64s(q["actor"])
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid actor")
		return
	}
	directors, ok := parseInt64s(q["director"])
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid director")
		return
	}
	f := store.ItemFilter{
		LibraryID: int64(queryInt(r, "library_id")),
		Kind:      q.Get("kind"),
		// The browse grid passes exclude_kind=collection,playlist: a franchise
		// tile beside the films it groups, or a playlist tile beside the artists
		// whose tracks are on it, answers a different question from the grid it
		// is in. Both have their own page. Comma-separated because it was one
		// kind and the second had nowhere to go.
		ExcludeKinds: splitCSV(q.Get("exclude_kind")),
		// The A–Z rail: one letter, or "#" for titles starting with anything
		// that is not a Latin letter.
		Initial:        q.Get("initial"),
		Query:          q.Get("q"),
		Sort:           q.Get("sort"),
		Genres:         nonEmpty(q["genre"]),
		Decades:        decades,
		ContentRatings: nonEmpty(q["content_rating"]),
		// Only the unwatched-only case is expressed; watched=true (watched-only)
		// is not a browse affordance today, so any value but "false" is ignored.
		Unwatched: q.Get("watched") == "false",
		UserID:    s.userID(r),
		Years:     years,
		// Resolution keys are not validated here. An unknown one contributes no
		// clause in the store rather than a 400: these arrive from a bookmarked
		// query string, and a tier that has been renamed should widen the grid
		// back to everything rather than break the page.
		Resolutions: nonEmpty(q["resolution"]),
		PersonIDs:   people,
		ActorIDs:    actors,
		DirectorIDs: directors,
		// status is a single value rather than a set. The two are not
		// combinable in any useful way -- an item cannot be both unmatched and
		// in progress in the same breath as a question -- and offering an AND
		// that always yields nothing is worse than not offering it.
		InProgress: q.Get("status") == "in_progress",
		Unmatched:  q.Get("status") == "unmatched",
		Limit:      queryInt(r, "limit"),
		Offset:     queryInt(r, "offset"),
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
	// The client may ask for fewer; it may not ask for more than the server's
	// configured shelf. A limit is a rule about what this server shows, not a
	// suggestion a client gets to raise.
	cur := s.settings.Get()
	limit := queryInt(r, "limit")
	if limit <= 0 || limit > cur.ContinueLimit {
		limit = cur.ContinueLimit
	}
	items, err := s.st.ContinueWatching(r.Context(), s.userID(r), limit,
		cur.ContinueCutoff(time.Now()))
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

	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	// The threshold is applied here, not in the client. Credits are not the
	// film: stopping at 96% is finishing it, and a shelf that keeps offering
	// the last ninety seconds back is a shelf nobody clears.
	//
	// OR rather than override, in that direction only. A client that knows it
	// reached the end (it fired `ended`) is telling the truth about something
	// the server cannot see; a client that says "not watched" at 98% is a
	// client whose idea of finished is out of date, and the server's rule wins.
	// A nil duration is an unprobed file, and a percentage of an unknown length
	// is not a fact about anything — Watched answers false for it.
	var duration int64
	if it.DurationMS != nil {
		duration = *it.DurationMS
	}
	watched := req.Watched || s.settings.Get().Watched(req.PositionMS, duration)
	if err := s.st.SaveProgress(r.Context(), id, s.userID(r), req.PositionMS, watched); err != nil {
		s.writeInternal(w, err, "save progress")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
