package api

import (
	"context"
	"net/http"
	"strings"
)

/*
 * Semantic photograph search over HTTP (ADR 0060).
 *
 * A SEPARATE FEATURE FROM FACE GROUPING, ALL THE WAY UP TO THE ROUTES
 *
 * They share a worker binary and a runtime library and nothing else: two
 * optional downloads, and a server may have either, both or neither. Hanging
 * search off /api/faces because that is where the binary lives would tell
 * somebody who wanted search and not face grouping that the feature was
 * unavailable because a *different* model was missing — the report ADR 0052
 * built the reason field to avoid, one feature along.
 *
 * So it has its own capabilities route, its own install job and its own reason,
 * and every one of them distinguishes "nothing matched" from "nothing was ever
 * indexed". A client that cannot tell those apart teaches people the feature is
 * broken.
 */

// semanticCapabilities reports whether this server can answer a typed query,
// and why not when it cannot.
func (s *Server) semanticCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.faceTool == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"semantic_ready":  false,
			"semantic_reason": "this build has no photograph worker configured",
		})
		return
	}
	c := s.faceTool.Capabilities(r.Context())
	/*
	 * Only the semantic half is answered here, rather than the whole
	 * capabilities struct.
	 *
	 * The struct also carries face readiness, and a client reading it from this
	 * route would eventually branch on `ready` — which is a different feature's
	 * answer, and false on a server that has semantic search working perfectly
	 * well.
	 */
	out := map[string]any{"semantic_ready": c.SemanticReady}
	if c.SemanticReason != "" {
		out["semantic_reason"] = c.SemanticReason
	}
	if c.SemanticModel != "" {
		out["semantic_model"] = c.SemanticModel
	}
	writeJSON(w, http.StatusOK, out)
}

/*
 * startSemanticPass indexes one library's photographs and returns immediately.
 *
 * Asynchronous and detached from the request for the same reasons the face pass
 * is: a picture library is a long job, and inheriting the request's context
 * would cancel the whole thing the moment the browser navigated away — which is
 * exactly what somebody does after pressing a button that says the work will
 * take a while.
 */
func (s *Server) startSemanticPass(w http.ResponseWriter, r *http.Request) {
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
			"photographs are indexed in picture libraries")
		return
	}
	if s.embedder == nil {
		writeError(w, http.StatusConflict, "not_available",
			"this build has no photograph worker configured")
		return
	}
	if c := s.faceTool.Capabilities(r.Context()); !c.SemanticReady {
		// The reason travels: "not installed" and "no model" are different
		// problems with different fixes, and a bare 409 sends somebody looking
		// in the wrong place.
		writeError(w, http.StatusConflict, "not_available", c.SemanticReason)
		return
	}

	go func() {
		if err := s.embedder.Run(context.Background(), id); err != nil {
			s.log.Error("semantic pass", "library", id, "error", err)
		}
	}()

	s.audit(r, "photos.index", "library", lib.Name,
		"started indexing photographs for search", nil)
	writeJSON(w, http.StatusAccepted, s.embedder.Stats())
}

/*
 * semanticStatus says how much of one library is indexed, without searching.
 *
 * Added after watching the screen: arriving at it on a library that has never
 * been indexed showed a search field and *nothing else*, because every sentence
 * on the page was derived from a search result and no search had been run. The
 * one screen written to keep three empty states apart opened in a fourth with
 * no state at all.
 *
 * A search cannot answer this. It costs a process start and a model load, needs
 * a query nobody has typed yet, and returning `indexed: 0` for a library that
 * has 12,000 vectors under a *previous* model would be the wrong number
 * besides. This is a count, so it is a route that counts.
 */
func (s *Server) semanticStatus(w http.ResponseWriter, r *http.Request) {
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
			"photographs are indexed in picture libraries")
		return
	}

	/*
	 * Answers even when nothing can embed, rather than 409ing like the search
	 * does.
	 *
	 * The counts are still true — a library that was indexed and then had its
	 * models removed holds exactly as many vectors as it did — and this is the
	 * route a screen calls on arrival. Refusing it would put the page back in
	 * the state that caused it to be written.
	 */
	model := ""
	if s.faceTool != nil {
		model = s.faceTool.Capabilities(r.Context()).SemanticModel
	}
	out := map[string]any{"indexed": 0, "pending": 0, "model": model}
	if model != "" {
		indexed, err := s.st.EmbeddedPhotoCount(r.Context(), id, model)
		if err != nil {
			s.writeInternal(w, err, "count embedded photographs")
			return
		}
		pending, err := s.st.PhotosPendingEmbeddingCount(r.Context(), id, model)
		if err != nil {
			s.writeInternal(w, err, "count photographs to embed")
			return
		}
		out["indexed"], out["pending"] = indexed, pending
	}
	if s.embedder != nil {
		out["running"] = s.embedder.Stats().Running
	} else {
		out["running"] = false
	}
	writeJSON(w, http.StatusOK, out)
}

/*
 * searchPhotos answers a typed query with photographs.
 *
 * Two things are reported alongside the results and neither is decoration.
 * `indexed` is how many photographs this library has vectors for, so an empty
 * answer can say "nothing matched" or "nothing has been indexed yet" — the same
 * distinction every face route makes, and the one that decides whether somebody
 * rephrases the query or presses the button that starts the pass.
 *
 * The score travels too. It is a cosine against a unit vector, so it is
 * bounded and comparable within one answer and means very little across two;
 * it is here because a result list with no notion of confidence gives a
 * distant fifth-best the same standing as an obvious first, and because
 * choosing a floor is impossible without being able to see what the numbers
 * actually look like.
 */
func (s *Server) searchPhotos(w http.ResponseWriter, r *http.Request) {
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
			"photographs are searched in picture libraries")
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "a query is required")
		return
	}

	if s.faceTool == nil {
		writeError(w, http.StatusConflict, "not_available",
			"this build has no photograph worker configured")
		return
	}
	caps := s.faceTool.Capabilities(r.Context())
	if !caps.SemanticReady {
		writeError(w, http.StatusConflict, "not_available", caps.SemanticReason)
		return
	}

	vec, err := s.faceTool.EmbedText(r.Context(), q)
	if err != nil {
		/*
		 * A query the worker could not turn into a vector is the server's
		 * failure, not the caller's. There is no input this route accepts that
		 * should make the worker fail — the tokenizer truncates rather than
		 * refusing — so a failure here means the install is wrong, and calling
		 * it a bad request would send somebody rewording a query that was fine.
		 */
		s.writeInternal(w, err, "embed query")
		return
	}

	hits, err := s.st.SearchPhotosByVector(r.Context(), id, caps.SemanticModel,
		vec, queryInt(r, "limit"), 0)
	if err != nil {
		s.writeInternal(w, err, "search photographs")
		return
	}

	indexed, err := s.st.EmbeddedPhotoCount(r.Context(), id, caps.SemanticModel)
	if err != nil {
		s.writeInternal(w, err, "count embedded photographs")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"hits": hits,
		// Always present, including when it is zero: "nothing matched" and
		// "nothing has been indexed" are different sentences, and a client that
		// has to infer which one it is will pick wrong.
		"indexed": indexed,
		// The model the answer was ranked in. A library holding vectors from a
		// previous model is ranked against none of them, and this is what lets
		// a client say so rather than showing an empty shelf.
		"model": caps.SemanticModel,
	})
}
