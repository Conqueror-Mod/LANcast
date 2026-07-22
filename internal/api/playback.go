package api

import (
	"net/http"

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
	it, err := s.st.GetItem(r.Context(), id, localUser)
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	decision := probe.Decide(probe.ResultFromItem(it), probe.BrowserProfile())

	writeJSON(w, http.StatusOK, map[string]any{
		"item_id":  id,
		"probed":   it.ProbedAt != nil,
		"decision": decision,
	})
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
