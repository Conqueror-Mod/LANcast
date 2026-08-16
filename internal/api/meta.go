package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"lancast/internal/media"
	"lancast/internal/meta"
	"lancast/internal/scan"
	"lancast/internal/store"
)

// patchItem edits metadata fields. Every edited field is locked, so no later
// refresh can overwrite it (ADR 0008).
func (s *Server) patchItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	if _, err := s.st.GetItem(r.Context(), id, s.userID(r)); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	// Decoded into a map so "absent" and "explicitly empty" stay distinct —
	// clearing a field is a legitimate edit.
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	if len(raw) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "no fields to update")
		return
	}

	var upd store.ItemMetadata
	var touched []string

	for field, val := range raw {
		if !meta.IsField(field) {
			writeError(w, http.StatusBadRequest, "bad_request", "unknown field: "+field)
			return
		}
		switch field {
		case meta.FieldTitle:
			var v string
			if err := json.Unmarshal(val, &v); err != nil || strings.TrimSpace(v) == "" {
				writeError(w, http.StatusBadRequest, "bad_request", "title must be a non-empty string")
				return
			}
			upd.Title = &v
			sort := meta.SortTitleOf(v)
			upd.SortTitle = &sort
		case meta.FieldYear:
			var v int
			if err := json.Unmarshal(val, &v); err != nil {
				writeError(w, http.StatusBadRequest, "bad_request", "year must be a number")
				return
			}
			upd.Year = &v
		case meta.FieldOverview:
			var v string
			if err := json.Unmarshal(val, &v); err != nil {
				writeError(w, http.StatusBadRequest, "bad_request", "overview must be a string")
				return
			}
			upd.Overview = &v
		case meta.FieldRating:
			var v float64
			if err := json.Unmarshal(val, &v); err != nil {
				writeError(w, http.StatusBadRequest, "bad_request", "rating must be a number")
				return
			}
			upd.Rating = &v
		case meta.FieldContentRating:
			var v string
			if err := json.Unmarshal(val, &v); err != nil {
				writeError(w, http.StatusBadRequest, "bad_request", "content_rating must be a string")
				return
			}
			upd.ContentRating = &v
		case meta.FieldSeries:
			var v string
			if err := json.Unmarshal(val, &v); err != nil {
				writeError(w, http.StatusBadRequest, "bad_request", "series must be a string")
				return
			}
			upd.Series = &v
		case meta.FieldSeason, meta.FieldEpisode:
			var v int
			if err := json.Unmarshal(val, &v); err != nil {
				writeError(w, http.StatusBadRequest, "bad_request", field+" must be a number")
				return
			}
			if field == meta.FieldSeason {
				upd.Season = &v
			} else {
				upd.Episode = &v
			}
		default:
			writeError(w, http.StatusBadRequest, "bad_request", "field is not editable: "+field)
			return
		}
		touched = append(touched, field)
	}

	if err := s.st.UpdateItemMetadata(r.Context(), id, upd); err != nil {
		s.writeInternal(w, err, "update item")
		return
	}
	for _, field := range touched {
		if err := s.st.LockField(r.Context(), id, field); err != nil {
			s.writeInternal(w, err, "lock field")
			return
		}
	}

	// An edit locks every field it touches, so this is not just a value change
	// — it is a standing instruction that no provider may overwrite it. The
	// field list is what makes that reviewable later.
	s.audit(r, "item.edit", "item", auditID(id),
		fmt.Sprintf("Edited and locked %s on %q", strings.Join(touched, ", "), itemTitle(r, s, id)),
		map[string]any{"fields": touched})

	s.respondItem(w, r, id)
}

// deleteLock releases one field so it resumes updating.
func (s *Server) deleteLock(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	field := r.PathValue("field")
	if !meta.IsField(field) {
		writeError(w, http.StatusBadRequest, "bad_request", "unknown field: "+field)
		return
	}
	if _, err := s.st.GetItem(r.Context(), id, s.userID(r)); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	if err := s.st.UnlockField(r.Context(), id, field); err != nil {
		s.writeInternal(w, err, "unlock field")
		return
	}
	// Unlocking hands a field back to providers, so a value a user chose can
	// now change on its own. That is the surprising half of ADR 0008 and it
	// deserves a record.
	s.audit(r, "item.unlock", "item", auditID(id),
		fmt.Sprintf("Unlocked %s on %q — providers may overwrite it again", field, itemTitle(r, s, id)),
		map[string]any{"field": field})
	w.WriteHeader(http.StatusNoContent)
}

func contains(fields []string, want string) bool {
	for _, f := range fields {
		if f == want {
			return true
		}
	}
	return false
}

// candidates searches providers for possible matches.
func (s *Server) candidates(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	q := meta.Query{Kind: meta.Kind(it.Kind), Title: it.Title}
	if v := strings.TrimSpace(r.URL.Query().Get("q")); v != "" {
		q.Title = v
		q.Series = v
	} else {
		// GetItem does not load locks, so fetch them: a title the user set by
		// hand is locked and its intent is honoured; a title from a match is not,
		// and — since Fix Match exists to correct that very match — searching it
		// would circle the wrong identity. Re-parse the filename in that case to
		// recover the identity the scanner started from.
		locks, _ := s.st.LockedFields(r.Context(), it.ID)
		if contains(locks, meta.FieldTitle) {
			if it.Series != nil {
				q.Series = *it.Series
			}
			if it.Year != nil {
				q.Year = *it.Year
			}
		} else if lib, err := s.st.GetLibrary(r.Context(), it.LibraryID); err == nil {
			info := media.Parse(lib.Path, it.Path, lib.Kind)
			if info.Series != "" {
				q.Title, q.Series = info.Series, info.Series
			} else if info.Title != "" {
				q.Title = info.Title
			}
			if info.Year > 0 {
				q.Year = info.Year
			}
		}
	}

	// Search both film and television, not just the item's own kind. A TV
	// miniseries scanned into a movie library (Storm of the Century) can only be
	// corrected if Fix match can reach TMDB's TV data — a movie-scoped search
	// finds only the wrong, same-named film. The candidate carries its own kind,
	// so applying a TV result fetches from /tv.
	var cands []meta.Candidate
	seen := map[string]bool{}
	var lastErr error
	for _, k := range []meta.Kind{meta.KindMovie, meta.KindShow} {
		qk := q
		qk.Kind = k
		found, err := s.reg.Search(r.Context(), qk)
		if err != nil {
			lastErr = err
			continue
		}
		for _, c := range found {
			key := c.Provider + ":" + string(c.Kind) + ":" + c.ExternalID
			if seen[key] {
				continue
			}
			seen[key] = true
			cands = append(cands, c)
		}
	}
	if len(cands) == 0 {
		if lastErr != nil {
			// A provider that is unconfigured or unreachable is not a server
			// fault; report it as unavailable so the UI can explain.
			writeError(w, http.StatusServiceUnavailable, "unavailable", "no metadata provider is available")
			return
		}
		writeJSON(w, http.StatusOK, []meta.Candidate{})
		return
	}
	// Re-rank the merged list against the query as a film (year scored), so film
	// and TV candidates are ordered together rather than within their kind.
	rankQ := q
	rankQ.Kind = meta.KindMovie
	meta.Rank(rankQ, cands)
	if len(cands) > 20 {
		cands = cands[:20]
	}
	writeJSON(w, http.StatusOK, cands)
}

// applyMatch confirms an identity. The item is locked against re-scoring:
// rescans reconcile files, they do not re-litigate identity.
func (s *Server) applyMatch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	var req struct {
		Provider   string `json:"provider"`
		ExternalID string `json:"external_id"`
		// Kind is the chosen candidate's kind, which may differ from the item's
		// own — correcting a movie-scanned miniseries to its TV entry. Empty
		// falls back to the item's kind (the common same-kind correction).
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	if req.Provider == "" || req.ExternalID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "provider and external_id are required")
		return
	}
	if _, found := s.reg.Provider(req.Provider); !found {
		writeError(w, http.StatusBadRequest, "bad_request", "unknown provider: "+req.Provider)
		return
	}

	// Fetch and apply the chosen record synchronously. It cannot go through the
	// background pass: that queue skips locked items and re-searches, which would
	// re-pick the candidate the user just rejected. The item is then locked so a
	// rescan reconciles files without re-litigating this identity.
	if err := s.worker.ApplyMatch(r.Context(), *it, req.Provider, req.ExternalID, meta.Kind(req.Kind)); err != nil {
		s.writeInternal(w, err, "apply match")
		return
	}
	s.enrichSoon()

	// An identity override outranks every provider from here on, so it is
	// exactly the kind of decision that should be attributable later. The
	// previous identity is recorded because "what was it before" is the first
	// question asked when a match turns out wrong.
	s.audit(r, "item.match", "item", auditID(id),
		fmt.Sprintf("Set %q to %s:%s (was %s)", it.Title, req.Provider, req.ExternalID,
			formerIdentity(it)),
		map[string]any{
			"provider": req.Provider, "external_id": req.ExternalID, "kind": req.Kind,
			"previous_provider": it.Provider, "previous_external_id": it.ExternalID,
			"previous_state": it.MatchState,
		})

	s.respondItem(w, r, id)
}

// trailer returns a promotional video for an item, if a provider has one.
//
// Only the video's identity is returned, never a proxied stream: LANcast does
// not sit between the user and YouTube, and playing it is the client's choice
// to make.
func (s *Server) trailer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	if it.ExternalID == nil || *it.ExternalID == "" {
		// Nothing matched, so there is nothing to look up. Not an error.
		writeJSON(w, http.StatusOK, map[string]any{"trailer": nil})
		return
	}

	provider := ""
	if it.Provider != nil {
		provider = *it.Provider
	}
	t, err := s.reg.Trailer(r.Context(), provider, meta.Ref{
		Kind: meta.Kind(it.Kind), ExternalID: *it.ExternalID,
	})
	if err != nil {
		s.log.Debug("trailer lookup failed", "item", id, "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"trailer": t})
}

// reviewQueue lists items whose identity is uncertain.
func (s *Server) reviewQueue(w http.ResponseWriter, r *http.Request) {
	libraryID := int64(queryInt(r, "library_id"))
	items, err := s.st.ReviewQueue(r.Context(), libraryID, queryInt(r, "limit"))
	if err != nil {
		s.writeInternal(w, err, "review queue")
		return
	}
	// Review-band items matched (and so have a poster); attach it so the queue
	// can show artwork. Unmatched items simply have none.
	if err := s.st.AttachArtwork(r.Context(), items); err != nil {
		s.writeInternal(w, err, "attach artwork")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": len(items), "items": items})
}

// refreshItem and refreshLibrary requeue metadata. Locked fields still survive;
// this only schedules the work.
func (s *Server) refreshItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	if _, err := s.st.GetItem(r.Context(), id, s.userID(r)); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	if err := s.st.ClearMetadataStamp(r.Context(), 0, id); err != nil {
		s.writeInternal(w, err, "refresh item")
		return
	}
	s.enrichSoon()
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) refreshLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return
	}
	if _, err := s.st.GetLibrary(r.Context(), id); s.notFoundOr(w, err, "get library", "no such library") {
		return
	}
	if err := s.st.ClearMetadataStamp(r.Context(), id, 0); err != nil {
		s.writeInternal(w, err, "refresh library")
		return
	}
	s.enrichSoon()
	w.WriteHeader(http.StatusAccepted)
}

/*
 * reparseLibrary re-runs the filename heuristics over a library's uncertain
 * rows. Admin only.
 *
 * Distinct from refresh, and the distinction is the whole point: refresh asks
 * the provider the same question again, where this corrects the question. A
 * film whose year lived only in its folder name searched with no year at all,
 * and no number of refreshes would have changed that answer.
 *
 * Only 'review' and 'unmatched' rows are touched, locked fields are skipped
 * per field, and rows that already agree with their filename are not requeued
 * — so this is safe to run twice.
 */
func (s *Server) reparseLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return
	}
	lib, err := s.st.GetLibrary(r.Context(), id)
	if s.notFoundOr(w, err, "get library", "no such library") {
		return
	}

	res, err := scan.Reparse(r.Context(), s.st, id)
	if err != nil {
		s.writeInternal(w, err, "reparse library")
		return
	}

	if res.Changed > 0 {
		s.audit(r, "library.reparse", "library", fmt.Sprint(id),
			fmt.Sprintf("Re-parsed %q — %d of %d uncertain items changed and were requeued",
				lib.Name, res.Changed, res.Examined),
			map[string]any{"examined": res.Examined, "changed": res.Changed})
		s.enrichSoon()
	}
	writeJSON(w, http.StatusOK, res)
}

// enrichStatus reports background enrichment progress.
func (s *Server) enrichStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.worker.Stats())
}

// respondItem writes an item with its detail fields attached.
func (s *Server) respondItem(w http.ResponseWriter, r *http.Request, id int64) {
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "no such item")
			return
		}
		s.writeInternal(w, err, "get item")
		return
	}
	if err := s.st.LoadDetail(r.Context(), it); err != nil {
		s.writeInternal(w, err, "load item detail")
		return
	}
	streams, err := s.st.Streams(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "load streams")
		return
	}
	it.Streams = streams
	// Attach the child count so the detail page can render a container (its
	// seasons, films, or parts) rather than a dead-end Play (ADR 0017).
	counted := []store.Item{*it}
	if err := s.st.AttachChildCounts(r.Context(), counted); err != nil {
		s.writeInternal(w, err, "attach child counts")
		return
	}
	it.ChildCount = counted[0].ChildCount
	writeJSON(w, http.StatusOK, it)
}

// formerIdentity renders what an item was matched to before an override, for
// the audit summary. "Unmatched" is the honest answer for most first
// corrections and reads better than an empty provider pair.
func formerIdentity(it *store.Item) string {
	if it.Provider == nil || it.ExternalID == nil || *it.Provider == "" || *it.ExternalID == "" {
		return "unmatched"
	}
	return *it.Provider + ":" + *it.ExternalID
}

// itemTitle reads an item's title for an audit summary, falling back to the id
// rather than failing the summary. An audit line that says "item 42" is worse
// than one that names the title, and both beat no line at all.
func itemTitle(r *http.Request, s *Server, id int64) string {
	if it, err := s.st.GetItem(r.Context(), id, s.userID(r)); err == nil {
		return it.Title
	}
	return "item " + auditID(id)
}
