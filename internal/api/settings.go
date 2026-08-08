package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"lancast/internal/config"
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

	prev := s.settings.Get()
	next := prev
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
	// Names only, never values. Recording that the TMDB key changed is the
	// audit fact; recording the key itself would turn the audit log into a
	// place secrets live.
	if changed := changedSettings(prev, next); len(changed) > 0 {
		s.audit(r, "settings.update", "settings", "",
			"Changed settings: "+strings.Join(changed, ", "),
			map[string]any{"fields": changed})
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

// changedSettings names the fields that actually differ, so the audit summary
// says what changed rather than that a save happened. Secret values are
// compared but never recorded: the fact of a key rotation is auditable, the key
// is not.
func changedSettings(prev, next config.Settings) []string {
	var out []string
	add := func(name string, differs bool) {
		if differs {
			out = append(out, name)
		}
	}
	add("tmdb_key", prev.TMDBKey != next.TMDBKey)
	add("opensubtitles_key", prev.OpenSubtitlesKey != next.OpenSubtitlesKey)
	add("omdb_key", prev.OMDbKey != next.OMDbKey)
	add("ffmpeg_dir", prev.FFmpegDir != next.FFmpegDir)
	add("rate_per_sec", prev.RatePerSec != next.RatePerSec)
	add("write_nfo", prev.WriteNFO != next.WriteNFO)
	add("auto_enrich", prev.AutoEnrich != next.AutoEnrich)
	add("hardware_encoder", prev.HardwareEncoder != next.HardwareEncoder)
	return out
}
