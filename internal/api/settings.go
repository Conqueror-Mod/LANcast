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
		"omdb": map[string]any{
			"configured": cur.OMDbKey != "",
		},
		"rate_per_sec": cur.RatePerSec,
		"write_nfo":    cur.WriteNFO,
		"auto_enrich":  cur.AutoEnrich,
		// Whether the server can actually inspect and convert media. Reported so
		// the UI can say so plainly: without these, every file is direct-played
		// and anything the browser cannot decode fails with no explanation — the
		// failure that went unnoticed across a whole library (ADR 0016).
		"media_tools": map[string]any{
			"probe_available":     s.probes.Available(),
			"transcode_available": s.trans.Available(),
			"directory":           cur.FFmpegDir,
		},
		"encoder": map[string]any{
			"preference": firstNonEmptyStr(cur.HardwareEncoder, "auto"),
			"active":     s.trans.Encoder(),
			"available":  s.trans.AvailableEncoders(),
		},
	})
}

// putSettings updates configuration. Omitted fields are left unchanged, so a
// client that only wants to toggle NFO writing need not resend the API key.
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TMDBKey          *string  `json:"tmdb_key"`
		OpenSubtitlesKey *string  `json:"opensubtitles_key"`
		OMDbKey          *string  `json:"omdb_key"`
		FFmpegDir        *string  `json:"ffmpeg_dir"`
		RatePerSec       *float64 `json:"rate_per_sec"`
		WriteNFO         *bool    `json:"write_nfo"`
		AutoEnrich       *bool    `json:"auto_enrich"`
		HardwareEncoder  *string  `json:"hardware_encoder"`
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
	if req.OMDbKey != nil {
		next.OMDbKey = strings.TrimSpace(*req.OMDbKey)
	}
	if req.FFmpegDir != nil {
		next.FFmpegDir = strings.TrimSpace(*req.FFmpegDir)
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
	encoderChanged := req.HardwareEncoder != nil &&
		*req.HardwareEncoder != next.HardwareEncoder
	if req.HardwareEncoder != nil {
		next.HardwareEncoder = strings.TrimSpace(*req.HardwareEncoder)
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
	// Re-selecting runs a test encode per candidate, so it is only done when
	// the preference actually changed rather than on every settings save.
	if encoderChanged {
		s.trans.DetectHardware(r.Context(), next.HardwareEncoder)
	}

	s.getSettings(w, r)
}
