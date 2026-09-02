package api

import (
	"net/http"
)

/*
 * Markers: reading what the detector found, and asking it to look again.
 *
 * Stage 1 of ADR 0054 exposes markers for inspection and nothing else. No
 * playback decision reads one, the watched threshold is untouched, and no
 * client draws a skip button from this. That is the whole point of the stage:
 * the rule behind these timestamps is consistent across two independent
 * samples and has never been checked against a person watching a film, so the
 * only honest thing to do with it is show it to somebody who can.
 *
 * Which also decides the shape here. This is an inspection surface, so it
 * answers with the detector's own numbers — confidence and source included —
 * rather than a tidied "skip to" the caller would be tempted to act on.
 */

// itemMarkers returns the markers detected on one item.
func (s *Server) itemMarkers(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	if _, err := s.st.GetItem(r.Context(), id, s.userID(r)); s.notFoundOr(w, err,
		"get item", "no such item") {
		return
	}

	markers, err := s.st.MarkersFor(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "item markers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"markers": markers})
}

// markerStatus reports background detection progress.
func (s *Server) markerStatus(w http.ResponseWriter, r *http.Request) {
	if s.markers == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": false, "available": false,
		})
		return
	}
	stats := s.markers.Stats()
	remaining, err := s.st.PendingMarkersCount(r.Context())
	if err != nil {
		s.writeInternal(w, err, "marker status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":   s.settings.Get().DetectMarkers,
		"available": s.markers.Available(),
		"running":   stats.Running,
		"examined":  stats.Examined,
		"found":     stats.Found,
		"failed":    stats.Failed,
		"remaining": remaining,
	})
}

/*
 * refreshMarkers queues every examined item to be looked at again.
 *
 * Admin-only and explicit, like re-probing and for a stronger reason: this is
 * a full decode of a quarter of every film in the library, not a header read.
 *
 * It exists because the rule is expected to change. The window and the length
 * thresholds are tuned numbers on a detector nobody has yet checked against a
 * human, and a build that moves them has to be able to ask every film the new
 * question — otherwise the library keeps answers from a rule that no longer
 * exists, which is worse than having none.
 */
func (s *Server) refreshMarkers(w http.ResponseWriter, r *http.Request) {
	if s.markers == nil || !s.markers.Available() {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"ffmpeg is not installed, so credits cannot be detected")
		return
	}
	if !s.settings.Get().DetectMarkers {
		// Refused rather than quietly queued. Turning the setting off is how
		// somebody says they do not want their CPU spent on this, and a
		// request that filled the queue anyway would spend it at the next
		// restart instead.
		writeError(w, http.StatusConflict, "conflict",
			"credits detection is off; turn it on in settings first")
		return
	}

	queued, err := s.st.ClearMarkers(r.Context())
	if err != nil {
		s.writeInternal(w, err, "queue marker refresh")
		return
	}
	if s.detectMarkers != nil {
		s.detectMarkers()
	}
	s.log.Info("marker refresh queued", "items", queued)
	writeJSON(w, http.StatusOK, map[string]any{"queued": queued})
}
