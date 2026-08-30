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
		"rate_per_sec":  cur.RatePerSec,
		"debug_logging": cur.DebugLogging,
		// The server's rules about what a client shows and what it may do.
		"watched_threshold":    cur.WatchedThreshold,
		"continue_weeks":       cur.ContinueWeeks,
		"continue_limit":       cur.ContinueLimit,
		"allow_media_deletion": cur.AllowMediaDeletion,
		"empty_trash_on_scan":  cur.EmptyTrashOnScan,
		"scan_interval_hours":  cur.ScanIntervalHours,
		"audit_retention_days": cur.AuditRetentionDays,
		"write_nfo":            cur.WriteNFO,
		"auto_enrich":          cur.AutoEnrich,
		"update_check":         cur.UpdateCheck,
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
		UpdateCheck      *bool    `json:"update_check"`
		HardwareEncoder  *string  `json:"hardware_encoder"`

		DebugLogging       *bool `json:"debug_logging"`
		WatchedThreshold   *int  `json:"watched_threshold"`
		ContinueWeeks      *int  `json:"continue_weeks"`
		ContinueLimit      *int  `json:"continue_limit"`
		AllowMediaDeletion *bool `json:"allow_media_deletion"`
		EmptyTrashOnScan   *bool `json:"empty_trash_on_scan"`
		ScanIntervalHours  *int  `json:"scan_interval_hours"`
		AuditRetentionDays *int  `json:"audit_retention_days"`
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
	if req.UpdateCheck != nil {
		next.UpdateCheck = *req.UpdateCheck
	}
	if req.AutoEnrich != nil {
		next.AutoEnrich = *req.AutoEnrich
	}
	// Ranges are rejected rather than clamped, because a client sending 200%
	// has a bug and silently storing 90 hides it. config.clamp is the floor
	// under a hand-edited file, not a substitute for saying no here.
	if req.DebugLogging != nil {
		next.DebugLogging = *req.DebugLogging
	}
	if req.WatchedThreshold != nil {
		if *req.WatchedThreshold < 50 || *req.WatchedThreshold > 100 {
			writeError(w, http.StatusBadRequest, "bad_request",
				"watched_threshold must be between 50 and 100")
			return
		}
		next.WatchedThreshold = *req.WatchedThreshold
	}
	if req.ContinueWeeks != nil {
		if *req.ContinueWeeks < 0 || *req.ContinueWeeks > 520 {
			writeError(w, http.StatusBadRequest, "bad_request",
				"continue_weeks must be between 0 and 520")
			return
		}
		next.ContinueWeeks = *req.ContinueWeeks
	}
	if req.ContinueLimit != nil {
		if *req.ContinueLimit < 1 || *req.ContinueLimit > 100 {
			writeError(w, http.StatusBadRequest, "bad_request",
				"continue_limit must be between 1 and 100")
			return
		}
		next.ContinueLimit = *req.ContinueLimit
	}
	if req.AllowMediaDeletion != nil {
		next.AllowMediaDeletion = *req.AllowMediaDeletion
	}
	if req.EmptyTrashOnScan != nil {
		next.EmptyTrashOnScan = *req.EmptyTrashOnScan
	}
	if req.ScanIntervalHours != nil {
		if *req.ScanIntervalHours < 0 || *req.ScanIntervalHours > 168 {
			writeError(w, http.StatusBadRequest, "bad_request",
				"scan_interval_hours must be between 0 (off) and 168")
			return
		}
		next.ScanIntervalHours = *req.ScanIntervalHours
	}
	if req.AuditRetentionDays != nil {
		// The ceiling is ten years. Not a technical limit -- it is the point
		// past which "keep for this many days" is a clumsier way of saying
		// zero, which is the supported way to keep an audit trail for ever.
		if *req.AuditRetentionDays < 0 || *req.AuditRetentionDays > 3650 {
			writeError(w, http.StatusBadRequest, "bad_request",
				"audit_retention_days must be between 0 (keep for ever) and 3650")
			return
		}
		next.AuditRetentionDays = *req.AuditRetentionDays
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
	add("update_check", prev.UpdateCheck != next.UpdateCheck)
	add("hardware_encoder", prev.HardwareEncoder != next.HardwareEncoder)
	add("debug_logging", prev.DebugLogging != next.DebugLogging)
	add("watched_threshold", prev.WatchedThreshold != next.WatchedThreshold)
	add("continue_weeks", prev.ContinueWeeks != next.ContinueWeeks)
	add("continue_limit", prev.ContinueLimit != next.ContinueLimit)
	// Worth auditing loudly: it is the switch that decides whether this server
	// can destroy media at all.
	add("allow_media_deletion", prev.AllowMediaDeletion != next.AllowMediaDeletion)
	// Audited like the deletion switch beside it, and for the same reason: it
	// changes whether the server destroys records without being asked again.
	add("empty_trash_on_scan", prev.EmptyTrashOnScan != next.EmptyTrashOnScan)
	add("scan_interval_hours", prev.ScanIntervalHours != next.ScanIntervalHours)
	add("audit_retention_days", prev.AuditRetentionDays != next.AuditRetentionDays)
	return out
}
