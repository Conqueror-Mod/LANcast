package api

import (
	"context"
	"encoding/json"
	"net/http"
)

/*
 * Face grouping over HTTP (ADR 0052).
 *
 * The feature is optional in a way the API has to be honest about: the worker
 * is a separate download, and a server without it must say so rather than
 * report a library with no people in it. Every endpoint here distinguishes
 * "nothing found" from "nothing looked", because a client that cannot tell
 * them apart teaches people the feature is broken.
 */

// facesAvailable reports the worker's readiness, with the reason when it is not.
func (s *Server) facesCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.faceTool == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ready":  false,
			"reason": "this build has no face worker configured",
		})
		return
	}
	writeJSON(w, http.StatusOK, s.faceTool.Capabilities(r.Context()))
}

/*
 * startFacePass kicks the worker off over one library and returns immediately.
 *
 * Asynchronous like every other pass here: a picture library is tens of
 * minutes of work, and an HTTP request that waited for it would time out
 * somewhere in the middle and leave the caller unable to tell a finished pass
 * from an abandoned one. Progress is read from /api/activity, which already
 * carries every other worker.
 */
func (s *Server) startFacePass(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return
	}
	lib, err := s.st.GetLibrary(r.Context(), id)
	if s.notFoundOr(w, err, "get library", "no such library") {
		return
	}
	if lib.Kind != "picture" {
		writeError(w, http.StatusBadRequest, "wrong_kind",
			"faces are grouped in picture libraries")
		return
	}
	if s.facesW == nil {
		writeError(w, http.StatusConflict, "not_available",
			"the face worker is not installed")
		return
	}
	if c := s.faceTool.Capabilities(r.Context()); !c.Ready {
		// The reason travels: "no model" and "not installed" are different
		// problems with different fixes, and a generic 409 sends somebody
		// looking in the wrong place.
		writeError(w, http.StatusConflict, "not_available", c.Reason)
		return
	}

	/*
	 * Detached from the request context on purpose.
	 *
	 * The pass outlives the HTTP call that started it, and inheriting the
	 * request's context would cancel the whole thing the moment the browser
	 * navigated away — which is exactly what somebody does after pressing a
	 * button that says the work will take a while.
	 */
	go func() {
		if err := s.facesW.Run(context.Background(), id); err != nil {
			s.log.Error("face pass", "library", id, "error", err)
		}
	}()

	s.audit(r, "library.faces", "library", auditID(id),
		"started grouping faces in "+lib.Name, nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true})
}

// people lists a picture library's face groups, largest first — an unnamed
// group of forty is worth naming before an unnamed group of one.
func (s *Server) people(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return
	}
	clusters, err := s.st.FaceClusters(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "face clusters")
		return
	}
	pending := 0
	if n, err := s.st.PendingFacesCount(r.Context(), id); err == nil {
		pending = n
	}
	/*
	 * `pending` is returned beside the groups so a client can say "still
	 * looking" rather than "nobody here". Those are the same empty list and
	 * completely different sentences, and this project has repeatedly paid for
	 * showing the second when the first was true.
	 */
	writeJSON(w, http.StatusOK, map[string]any{
		"people":  clusters,
		"pending": pending,
	})
}

/*
 * nameCluster records what a person called a group.
 *
 * A name is an edit and locks the group against re-clustering (ADR 0052). An
 * empty name clears it, which is how somebody undoes a mistake — without that,
 * a typo would be permanent, and permanence is what makes people afraid to use
 * a naming UI at all.
 */
func (s *Server) nameCluster(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid cluster id")
		return
	}
	var req struct {
		Name *string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "name is required")
		return
	}
	if err := s.st.NameCluster(r.Context(), id, *req.Name); err != nil {
		s.notFoundOr(w, err, "name cluster", "no such group")
		return
	}
	summary := "named a face group " + *req.Name
	if *req.Name == "" {
		summary = "cleared a face group's name"
	}
	s.audit(r, "faces.name", "face_cluster", auditID(id), summary, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
