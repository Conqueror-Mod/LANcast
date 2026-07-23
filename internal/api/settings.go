package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// getSettings returns the current configuration.
//
// The TMDB key is write-only: the response reports whether one is configured
// and never the value. A secret that can be read back out of the API is a
// secret that leaks through screenshots, logs, and shared sessions.
func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	cur := s.settings.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"tmdb": map[string]any{
			"configured": cur.TMDBKey != "",
		},
		"opensubtitles": map[string]any{
			"configured": cur.OpenSubtitlesKey != "",
		},
		"rate_per_sec": cur.RatePerSec,
		"write_nfo":    cur.WriteNFO,
		"auto_enrich":  cur.AutoEnrich,
	})
}

// putSettings updates configuration. Omitted fields are left unchanged, so a
// client that only wants to toggle NFO writing need not resend the API key.
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TMDBKey          *string  `json:"tmdb_key"`
		OpenSubtitlesKey *string  `json:"opensubtitles_key"`
		RatePerSec       *float64 `json:"rate_per_sec"`
		WriteNFO         *bool    `json:"write_nfo"`
		AutoEnrich       *bool    `json:"auto_enrich"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}

	next := s.settings.Get()
	if req.TMDBKey != nil {
		next.TMDBKey = strings.TrimSpace(*req.TMDBKey)
	}
	if req.OpenSubtitlesKey != nil {
		next.OpenSubtitlesKey = strings.TrimSpace(*req.OpenSubtitlesKey)
	}
	if req.RatePerSec != nil {
		if *req.RatePerSec <= 0 || *req.RatePerSec > 50 {
			writeError(w, http.StatusBadRequest, "bad_request", "rate_per_sec must be between 0 and 50")
			return
		}
		next.RatePerSec = *req.RatePerSec
	}
	if req.WriteNFO != nil {
		next.WriteNFO = *req.WriteNFO
	}
	if req.AutoEnrich != nil {
		next.AutoEnrich = *req.AutoEnrich
	}

	if err := s.settings.Set(next); err != nil {
		s.writeInternal(w, err, "save settings")
		return
	}
	// Providers are rebuilt so a newly entered key takes effect immediately
	// rather than after a restart.
	if s.rebuild != nil {
		s.rebuild(next)
	}

	s.getSettings(w, r)
}
