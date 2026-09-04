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
	"lancast/internal/scan"
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
//   - forget: the row goes and nothing else happens — no file is touched and
//     no path is ignored. Refused unless the item is already marked missing.
//
// A container (show, work) removes its whole subtree — every episode or part.
// A collection removes only the grouping, never the member films. Admin-only.
//
/*
 * Why `forget` exists rather than reusing `ignore`.
 *
 * A renamed file leaves a row behind: the old path is marked missing, the new
 * one is added, and the pair shows up in the collision report. On one real
 * library 34 of 43 collisions were exactly that, and there was no way to
 * resolve any of them.
 *
 * `ignore` is the wrong tool for it. It records the path so a rescan never
 * re-adds the file — but the file is *gone*, so there is nothing to suppress,
 * and `ignored_path` has no way back: nothing in the API or the client removes
 * an entry. Clearing 34 stale rows that way would write 34 permanent, invisible
 * entries for paths that do not exist, and quietly refuse to see those names
 * again if a backup ever restored them.
 *
 * Refused unless the row is missing, and that is the safety property rather
 * than a validation nicety. "Scanning marks missing, never deletes" exists so
 * an unmounted drive cannot destroy library data; a mode that forgets rows on
 * demand must not become the hole in it. A present file's row cannot be
 * forgotten — a rescan would re-add it anyway, so the only thing that mode
 * could achieve is confusion.
 */
func (s *Server) deleteItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode != "ignore" && mode != "delete" && mode != "forget" {
		writeError(w, http.StatusBadRequest, "bad_request",
			"mode must be 'ignore', 'delete' or 'forget'")
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
	// The gate on `forget`, checked against the stored row rather than anything
	// the caller said. A client that believes a file is gone and a server that
	// knows it is are different claims, and only one of them is evidence.
	if mode == "forget" && !it.Missing {
		writeError(w, http.StatusConflict, "not_missing",
			"this file is still on disk, so there is nothing to forget; remove it from the library instead")
		return
	}
	/*
	 * And the location has to be readable *now*.
	 *
	 * `missing` says a walk did not find the file, which is also true of every
	 * file on a drive that was asleep at the time. This is the difference: a
	 * location that reads fine and does not hold the file is evidence the file
	 * has gone.
	 *
	 * It replaces a weaker proxy. The collision report used to offer this only
	 * where another copy of the work survived, on the reasoning that a drive
	 * going away takes every copy missing together — which is true, and also
	 * refused the case where somebody had genuinely deleted both halves of a
	 * split-cut film and wanted the leftover rows gone. Measuring the drive
	 * answers the real question instead of standing in for it.
	 */
	if mode == "forget" {
		root, err := s.st.RootForItem(r.Context(), id)
		if err != nil {
			s.writeInternal(w, err, "root for item")
			return
		}
		if err := scan.CheckRoot(root.Path); err != nil {
			writeError(w, http.StatusConflict, "location_unavailable",
				"this title's location cannot be read, so the file may simply be offline rather than gone; nothing was forgotten")
			return
		}
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
	} else if mode == "ignore" {
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
	if mode == "forget" {
		verb = "Forgot a missing file"
	}
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
	/*
	 * Library 0 searches every library.
	 *
	 * "Everything this person is in" does not stop at the boundary between
	 * films and television, and those are two libraries here — so with every
	 * route requiring one id, the question people actually have could not be
	 * asked at all. A library id of 0 cannot exist (AUTOINCREMENT starts at 1),
	 * so it is free to mean all of them and no existing caller changes.
	 *
	 * Parsed here rather than by loosening `pathID`, which refuses anything
	 * <= 0 and is shared by every route that takes an id: relaxing it would
	 * quietly make `/api/items/0` and every other id-bearing path accept a
	 * value none of them has a meaning for. The one route where 0 means
	 * something parses it itself, and skips the existence check that would
	 * otherwise reject it.
	 */
	var id int64
	if r.PathValue("id") != "0" {
		parsed, ok := pathID(r)
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
			return
		}
		if _, err := s.st.GetLibrary(r.Context(), parsed); s.notFoundOr(w, err, "get library", "no such library") {
			return
		}
		id = parsed
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
	collections, ok := parseInt64s(q["collection"])
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid collection")
		return
	}
	// face_cluster is a face group (ADR 0052), not a credit. Repeatable and OR,
	// because one person is often several groups — see ItemFilter.FaceClusterIDs.
	// Malformed is a 400 on the same reasoning as `person`.
	faceClusters, ok := parseInt64s(q["face_cluster"])
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid face_cluster")
		return
	}
	// An unparseable rating widens rather than 400s: it is a threshold typed
	// into a URL, not an id, and showing the library is a better answer than an
	// error page.
	minRating, _ := strconv.ParseFloat(q.Get("min_rating"), 64)
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
		/*
		 * Opening a timeline bucket. `taken_month` is "2019-07";
		 * `taken_undated=1` is the bucket for photographs carrying no capture
		 * time at all.
		 *
		 * Either one also excludes sensitive folders, because the timeline's
		 * counts do (ADR 0051, amended) — a listing that disagreed with the
		 * count above it is a bug somebody reports as "the month says 40 and
		 * shows 43". It is derived here rather than accepted as a parameter:
		 * whether covered content appears is the server's decision, and a
		 * client that could ask for it would be a client that could ask for it.
		 */
		TakenMonth:   q.Get("taken_month"),
		TakenUndated: q.Get("taken_undated") == "1",

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
		Resolutions:    nonEmpty(q["resolution"]),
		PersonIDs:      people,
		ActorIDs:       actors,
		DirectorIDs:    directors,
		CollectionIDs:  collections,
		FaceClusterIDs: faceClusters,
		MinRating:      minRating,
		// status is a single value rather than a set. The two are not
		// combinable in any useful way -- an item cannot be both unmatched and
		// in progress in the same breath as a question -- and offering an AND
		// that always yields nothing is worse than not offering it.
		InProgress: q.Get("status") == "in_progress",
		Unmatched:  q.Get("status") == "unmatched",
		Limit:      queryInt(r, "limit"),
		Offset:     queryInt(r, "offset"),
	}
	/*
	 * Derived, never accepted from the caller. See TakenMonth above.
	 *
	 * face_cluster is deliberately NOT here. It implies the same exclusion, and
	 * for a stronger reason — being able to ask who is in a folder you cannot
	 * open is the disclosure ADR 0051 covers, by another route — so the store
	 * enforces it where the clause is written rather than trusting this line.
	 * A security property that every caller has to remember is one caller away
	 * from not being one. See ItemFilter.FaceClusterIDs.
	 */
	f.ExcludeSensitive = f.TakenMonth != "" || f.TakenUndated
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

	// The player writes progress every five seconds while the picture is
	// moving, which makes this the heartbeat presence needs without adding a
	// second timer to the client. The *moment* is shared; the data is not —
	// what this records goes to memory and never to a table (ADR 0045 §4).
	s.recordWatching(s.userID(r), it)
	w.WriteHeader(http.StatusNoContent)
}
