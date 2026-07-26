package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"lancast/internal/meta"
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
	if _, err := s.st.GetItem(r.Context(), id, localUser); s.notFoundOr(w, err, "get item", "no such item") {
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
	if _, err := s.st.GetItem(r.Context(), id, localUser); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	if err := s.st.UnlockField(r.Context(), id, field); err != nil {
		s.writeInternal(w, err, "unlock field")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// candidates searches providers for possible matches.
func (s *Server) candidates(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	it, err := s.st.GetItem(r.Context(), id, localUser)
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	q := meta.Query{Kind: meta.Kind(it.Kind), Title: it.Title}
	if v := strings.TrimSpace(r.URL.Query().Get("q")); v != "" {
		q.Title = v
		q.Series = v
	} else {
		if it.Series != nil {
			q.Series = *it.Series
		}
		if it.Year != nil {
			q.Year = *it.Year
		}
	}

	cands, err := s.reg.Search(r.Context(), q)
	if err != nil {
		// A provider that is unconfigured or unreachable is not a server
		// fault; report it as unavailable so the UI can explain.
		writeError(w, http.StatusServiceUnavailable, "unavailable", "no metadata provider is available")
		return
	}
	if cands == nil {
		cands = []meta.Candidate{}
	}
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
	if _, err := s.st.GetItem(r.Context(), id, localUser); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	var req struct {
		Provider   string `json:"provider"`
		ExternalID string `json:"external_id"`
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

	if err := s.st.SetMatch(r.Context(), id, req.Provider, req.ExternalID, meta.StateLocked, 1.0); err != nil {
		s.writeInternal(w, err, "set match")
		return
	}
	// Requeue so the confirmed identity is fetched and applied.
	if err := s.st.ClearMetadataStamp(r.Context(), 0, id); err != nil {
		s.writeInternal(w, err, "requeue item")
		return
	}
	s.enrichSoon()

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
	it, err := s.st.GetItem(r.Context(), id, localUser)
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
	if _, err := s.st.GetItem(r.Context(), id, localUser); s.notFoundOr(w, err, "get item", "no such item") {
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

// enrichStatus reports background enrichment progress.
func (s *Server) enrichStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.worker.Stats())
}

// respondItem writes an item with its detail fields attached.
func (s *Server) respondItem(w http.ResponseWriter, r *http.Request, id int64) {
	it, err := s.st.GetItem(r.Context(), id, localUser)
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
	writeJSON(w, http.StatusOK, it)
}
