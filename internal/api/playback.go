package api

import (
	"net/http"
	"strconv"

	"lancast/internal/probe"
)

// playback reports how a file would be delivered to a client, and why.
//
// Exposed as its own endpoint rather than buried in the stream response so the
// answer is inspectable before playback starts. "Why is this transcoding" is
// the question a media server most often has to answer, and it should not
// require reading logs.
func (s *Server) playback(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	// Takes the same profile and audio parameters as the stream endpoints, and
	// must: an explanation of a decision the server would not actually make is
	// worse than no explanation, because it sends the reader looking in the
	// wrong place.
	prof := clientProfile(r)
	audioIndex := queryIntDefault(r, "audio", -1)

	streams, err := s.st.Streams(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "load streams")
		return
	}
	res := probe.ResultWithStreams(it, streams)
	if audioIndex >= 0 && res != nil && res.AudioByIndex(audioIndex) == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "no audio track at that index")
		return
	}
	decision := probe.DecideTrack(res, prof, audioIndex)

	writeJSON(w, http.StatusOK, map[string]any{
		"item_id":  id,
		"probed":   it.ProbedAt != nil,
		"profile":  prof.Name,
		"decision": decision,
	})
}

// reprobe queues already-probed items to be probed again.
//
// Needed because a probe is only as good as the build that made it. When the
// prober learns to record a field the decision engine depends on — pix_fmt and
// bit depth being the case that prompted this — every item probed by an older
// build carries a decision made without it, and nothing in the normal queue
// will ever revisit them: the pending query is "probed_at IS NULL", and theirs
// is set.
//
// Admin-only and explicit. Re-probing a large library is hours of ffprobe, so
// it is never something the server decides to do on its own.
func (s *Server) reprobe(w http.ResponseWriter, r *http.Request) {
	if !s.probes.Available() {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"ffprobe is not installed, so files cannot be probed")
		return
	}

	var (
		queued int64
		err    error
		scope  = r.URL.Query().Get("scope")
	)
	switch scope {
	case "", "incomplete":
		// The default is the narrow one. "Re-probe everything" is a big enough
		// hammer that it should have to be asked for by name.
		scope = "incomplete"
		queued, err = s.st.ClearIncompleteProbe(r.Context())
	case "all":
		var libraryID int64
		if raw := r.URL.Query().Get("library"); raw != "" {
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
		queued, err = s.st.ClearProbe(r.Context(), libraryID)
	default:
		writeError(w, http.StatusBadRequest, "bad_request",
			`scope must be "incomplete" or "all"`)
		return
	}
	if err != nil {
		s.writeInternal(w, err, "queue re-probe")
		return
	}

	// Kick a pass rather than waiting for the next scan: an operator who asked
	// for this is watching for it to happen.
	if s.probe != nil {
		s.probe()
	}

	s.log.Info("re-probe queued", "scope", scope, "items", queued)
	writeJSON(w, http.StatusOK, map[string]any{"scope": scope, "queued": queued})
}

// probeStatus reports background probing progress.
func (s *Server) probeStatus(w http.ResponseWriter, r *http.Request) {
	stats := s.probes.Stats()
	writeJSON(w, http.StatusOK, map[string]any{
		"available": s.probes.Available(),
		"running":   stats.Running,
		"probed":    stats.Probed,
		"failed":    stats.Failed,
		"remaining": stats.Remaining,
		"total":     stats.Total,
	})
}
