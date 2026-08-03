package api

import (
	"net/http"
	"strconv"
)

// coverArtStatus reports album-art progress.
//
// Its own endpoint for the reason /api/probe has one: a background pass that
// cannot be observed is a background pass that fails silently, which is the
// mistake this project keeps having to relearn. "found" and "none" are reported
// separately because they are different answers — an album with no cover has
// not failed, and a status that merges the two makes a library of untagged rips
// look broken.
func (s *Server) coverArtStatus(w http.ResponseWriter, r *http.Request) {
	if s.covers == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false})
		return
	}
	stats := s.covers.Stats()
	writeJSON(w, http.StatusOK, map[string]any{
		// available is about *embedded* extraction, which needs ffmpeg. Sidecar
		// covers are found without it, so false here does not mean no artwork.
		"available": s.covers.Available(),
		"running":   stats.Running,
		"found":     stats.Found,
		"none":      stats.None,
		"failed":    stats.Failed,
		"remaining": stats.Remaining,
		"total":     stats.Total,
	})
}

// recoverArt re-queues albums for another look.
//
// The counterpart to re-probing, and needed for the same reason: the pending
// queue is "cover_checked_at IS NULL", so every album a previous build gave up
// on is invisible to it forever. A user who has just added cover.jpg files to a
// library has no other way to ask LANcast to look again.
func (s *Server) recoverArt(w http.ResponseWriter, r *http.Request) {
	var libraryID int64
	if raw := r.URL.Query().Get("library"); raw != "" {
		var err error
		libraryID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || libraryID <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
			return
		}
		if _, err := s.st.GetLibrary(r.Context(), libraryID); s.notFoundOr(w, err,
			"get library", "no such library") {
			return
		}
	}

	queued, err := s.st.ClearCoverArtChecks(r.Context(), libraryID)
	if err != nil {
		s.writeInternal(w, err, "queue album art refresh")
		return
	}

	// Kick a pass rather than waiting for the next scan: someone who asked for
	// this is watching for it to happen.
	if s.coversSoon != nil {
		s.coversSoon()
	}

	s.log.Info("album art refresh queued", "library", libraryID, "albums", queued)
	writeJSON(w, http.StatusOK, map[string]any{"queued": queued})
}
