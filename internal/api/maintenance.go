package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"lancast/internal/config"
)

/*
 * Maintenance: throwing away things that can be made again.
 *
 * The rule that decides what belongs here is that every action must be
 * *recoverable by the server itself*. Cached artwork re-downloads. Transcode
 * scratch is rebuilt the moment somebody presses play. Settings return to
 * documented defaults. Nothing here touches media, the database, accounts, or
 * anything a person typed and cannot retype.
 *
 * That boundary is the whole feature. "Clear cache and data" is a phrase that
 * appears in a lot of applications meaning a lot of different things, several
 * of which are "lose your library".
 */

// clearCache drops a cache. Admin only.
func (s *Server) clearCache(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}

	switch req.Target {
	case "artwork":
		freed, err := s.art.Clear()
		if err != nil {
			s.writeInternal(w, err, "clear artwork cache")
			return
		}
		// The rows that reference these hashes are left alone: an item keeps
		// knowing which artwork it has, and the bytes come back on the next
		// refresh. Until then a tile falls back the way it already does for art
		// that has not arrived yet.
		s.audit(r, "cache.clear", "cache", "artwork", "Cleared the artwork cache",
			map[string]any{"freed_bytes": freed})
		writeJSON(w, http.StatusOK, map[string]any{"freed_bytes": freed})

	case "transcode":
		// Every session dies with it, which is the honest cost and is stated in
		// the client: anything being transcoded right now stops. A session is a
		// few seconds of buffered video, not work anybody will miss.
		s.trans.StopAll()
		s.audit(r, "cache.clear", "cache", "transcode",
			"Cleared transcode scratch and stopped every session", nil)
		writeJSON(w, http.StatusOK, map[string]any{"freed_bytes": 0})

	default:
		writeError(w, http.StatusBadRequest, "bad_request",
			"target must be 'artwork' or 'transcode'")
	}
}

// resetSettings restores the documented defaults. Admin only.
//
// Credentials are deliberately *not* reset: the password hash, the provider API
// keys, and the TLS certificate paths survive. Wiping the first would lock the
// operator out of their own server, and wiping the others would break metadata
// and HTTPS — none of which is what anybody means by "reset settings", and all
// of which is unrecoverable from the server's side. What resets is the
// behaviour: rate limit, NFO writing, enrichment, update checks, encoder
// preference, and the five server rules.
func (s *Server) resetSettings(w http.ResponseWriter, r *http.Request) {
	prev := s.settings.Get()
	next := config.Defaults()

	// Carried across, in one place so the exceptions are readable as a list
	// rather than inferred from what the code forgot.
	next.PasswordHash = prev.PasswordHash
	next.TMDBKey = prev.TMDBKey
	next.OpenSubtitlesKey = prev.OpenSubtitlesKey
	next.OMDbKey = prev.OMDbKey
	next.TLSCertFile = prev.TLSCertFile
	next.TLSKeyFile = prev.TLSKeyFile
	// Where ffmpeg is, is a fact about this machine that `service install`
	// discovered — not a preference, and re-discovering it is not something a
	// reset can do.
	next.FFmpegDir = prev.FFmpegDir

	if err := s.settings.Set(next); err != nil {
		s.writeInternal(w, err, "reset settings")
		return
	}
	if changed := changedSettings(prev, next); len(changed) > 0 {
		s.audit(r, "settings.reset", "settings", "",
			"Reset settings to defaults: "+strings.Join(changed, ", "),
			map[string]any{"fields": changed})
	}
	if s.rebuild != nil {
		s.rebuild(next)
	}
	s.getSettings(w, r)
}
