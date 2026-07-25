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
	"lancast/internal/subtitle"
)

// searchSubtitles finds candidate subtitle files for an item.
//
// The hash is computed first and sent with the query: a hash match means the
// subtitle was timed against these exact bytes, which no amount of release-name
// agreement can equal.
func (s *Server) searchSubtitles(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	it, err := s.st.GetItem(r.Context(), id, localUser)
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	client := s.subtitleClient()
	if client == nil || !client.Configured() {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"add an OpenSubtitles API key in Settings to search for subtitles")
		return
	}

	path, err := s.itemFilePath(r, it)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such item")
		return
	}

	query := subtitle.SearchQuery{
		Query:     strings.TrimSpace(r.URL.Query().Get("q")),
		Languages: []string{"en"},
	}
	if query.Query == "" {
		query.Query = it.Title
		if it.Series != nil && *it.Series != "" {
			query.Query = *it.Series
		}
	}
	if lang := r.URL.Query().Get("language"); lang != "" {
		query.Languages = []string{subtitle.NormalizeLanguage(lang)}
	}
	if it.Year != nil {
		query.Year = *it.Year
	}
	if it.Season != nil {
		query.Season = *it.Season
	}
	if it.Episode != nil {
		query.Episode = *it.Episode
	}
	if hash, err := subtitle.MovieHash(path); err == nil {
		query.MovieHash = hash
	} else {
		s.log.Debug("moviehash failed", "item", id, "error", err)
	}

	cands, err := client.Search(r.Context(), query)
	if err != nil {
		s.writeSubtitleError(w, err)
		return
	}

	subtitle.Rank(subtitle.Target{
		FileName:   filepath.Base(path),
		Title:      query.Query, // the same title asked of the provider
		FPS:        frameRateOf(it),
		Height:     derefInt(it.Height),
		DurationMS: derefInt64(it.DurationMS),
	}, cands)

	if len(cands) > 25 {
		cands = cands[:25]
	}
	best, auto := subtitle.BestAutoMatch(cands)

	writeJSON(w, http.StatusOK, map[string]any{
		"item_id":        id,
		"hash_used":      query.MovieHash != "",
		"candidates":     cands,
		"auto_match":     auto,
		"auto_match_key": best.FileID,
	})
}

// downloadSubtitle fetches a chosen candidate and attaches it to the item.
func (s *Server) downloadSubtitle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	it, err := s.st.GetItem(r.Context(), id, localUser)
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	client := s.subtitleClient()
	if client == nil || !client.Configured() {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"add an OpenSubtitles API key in Settings")
		return
	}

	var req struct {
		FileID   int64  `json:"file_id"`
		Language string `json:"language"`
		FileName string `json:"file_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FileID == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "file_id is required")
		return
	}

	link, name, err := client.DownloadLink(r.Context(), req.FileID)
	if err != nil {
		s.writeSubtitleError(w, err)
		return
	}
	body, err := client.Fetch(r.Context(), link)
	if err != nil {
		s.writeSubtitleError(w, err)
		return
	}
	if !subtitle.LooksLikeText(body[:min(len(body), 512)]) {
		writeError(w, http.StatusUnprocessableEntity, "unsupported",
			"the downloaded file is not a text subtitle")
		return
	}

	// Downloads live in the data directory, never beside the media. Writing
	// into someone's library unasked is the same rule NFO writing follows.
	lang := subtitle.NormalizeLanguage(firstNonEmptyStr(req.Language, "en"))
	dir := filepath.Join(s.dataDir, "subtitles", "downloaded", itoa64(id))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.writeInternal(w, err, "create subtitle dir")
		return
	}
	dest := filepath.Join(dir, fmt.Sprintf("%d.%s.srt", req.FileID, lang))
	if err := os.WriteFile(dest, body, 0o644); err != nil {
		s.writeInternal(w, err, "save subtitle")
		return
	}

	label := firstNonEmptyStr(req.FileName, name)
	subID, err := s.st.AddSubtitle(r.Context(), store.ExternalSubtitle{
		ItemID: id, Path: dest, Language: lang,
		Title: trimLabel(label), Format: "srt", Source: "downloaded",
	})
	if err != nil {
		s.writeInternal(w, err, "record subtitle")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"key":      "external-" + itoa64(subID),
		"language": lang,
		"label":    subtitleLabel(lang, trimLabel(label), false, "Downloaded"),
	})
	_ = it
}

// deleteSubtitle removes a downloaded subtitle: its row and its file.
//
// Only downloaded subtitles can be removed this way. An embedded track lives
// inside the video, and a sidecar lives in the user's library — deleting files
// there is the same line the scanner refuses to cross ("marks missing, never
// deletes"). A wrong download, by contrast, is entirely the server's own, so
// removing it is the one safe case and the one the UI needs.
func (s *Server) deleteSubtitle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	if _, err := s.st.GetItem(r.Context(), id, localUser); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	kind, rest, ok := strings.Cut(r.PathValue("key"), "-")
	if !ok || kind != "external" {
		writeError(w, http.StatusBadRequest, "bad_request",
			"only downloaded subtitles can be removed")
		return
	}
	subID, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid subtitle key")
		return
	}

	// Scoped to the item, so a crafted id cannot reach another item's subtitle.
	ext, err := s.st.ExternalSubtitle(r.Context(), id, subID)
	if s.notFoundOr(w, err, "get subtitle", "no such subtitle") {
		return
	}
	if ext.Source != "downloaded" {
		writeError(w, http.StatusForbidden, "forbidden",
			"only downloaded subtitles can be removed; this one lives in your library")
		return
	}

	// Defense in depth: only ever unlink files under our own subtitles directory,
	// however the stored path looks. This is the same containment boundary every
	// row-to-path handler re-checks.
	abs, err := containedPath(filepath.Join(s.dataDir, "subtitles"), ext.Path)
	if err != nil {
		writeError(w, http.StatusConflict, "conflict",
			"this subtitle's file is not one the server manages")
		return
	}

	if err := s.st.DeleteExternalSubtitle(r.Context(), id, subID); s.notFoundOr(w, err, "delete subtitle", "no such subtitle") {
		return
	}
	// The row is already gone; a missing file must not fail the request.
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		s.log.Warn("remove subtitle file failed", "item", id, "path", abs, "error", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeSubtitleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, subtitle.ErrNoProvider):
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"add an OpenSubtitles API key in Settings to search for subtitles")
	case errors.Is(err, subtitle.ErrQuotaExhausted):
		// A real, expected condition on a free tier — not a server fault.
		writeError(w, http.StatusTooManyRequests, "too_many_requests",
			"OpenSubtitles download quota is used up for today")
	default:
		s.log.Warn("subtitle provider failed", "error", err)
		writeError(w, http.StatusBadGateway, "unavailable",
			"the subtitle provider could not be reached")
	}
}

// subtitleClient builds a client from current settings, so a newly entered key
// works without a restart.
func (s *Server) subtitleClient() *subtitle.OpenSubtitles {
	key := s.settings.Get().OpenSubtitlesKey
	if key == "" {
		return nil
	}
	return subtitle.NewOpenSubtitles(key)
}

// frameRateOf returns the probed video frame rate, or 0 when unknown.
func frameRateOf(it *store.Item) float64 {
	if it.FrameRate == nil {
		return 0
	}
	return *it.FrameRate
}

func trimLabel(s string) string {
	s = strings.TrimSuffix(s, filepath.Ext(s))
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
