package api

import (
	"net/http"
)

/*
 * The three things you can do with a show: play it, continue it, shuffle it.
 *
 * Continue is the one with a rule worth defending. The failure it is written
 * against is a stale *read*: a server that knows episode 14 was watched and
 * answers with 11 because something between the truth and the button is holding
 * an older picture of it. So there is no cache anywhere on this path — the
 * answer is computed from playback_state at the moment it is asked, and the
 * response says no-store so nothing downstream may keep it either.
 */

// continueShow answers where a show should resume for the calling user.
func (s *Server) continueShow(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	if _, err := s.st.GetItem(r.Context(), id, s.userID(r)); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	next, err := s.st.NextEpisodeFor(r.Context(), id, s.userID(r))
	if err != nil {
		s.writeInternal(w, err, "continue show")
		return
	}

	/*
	 * Never cached, by anybody.
	 *
	 * This is the whole feature. A conditional request, a proxy, or a browser
	 * holding this for thirty seconds reproduces exactly the bug it exists to
	 * avoid — pressing continue and being sent to an episode already watched —
	 * and it would do so intermittently, which is the hardest kind to believe.
	 */
	w.Header().Set("Cache-Control", "no-store")

	out := map[string]any{
		"resume":    next.Resume,
		"exhausted": next.Exhausted,
	}
	if next.Item != nil {
		out["episode"] = next.Item
	}
	writeJSON(w, http.StatusOK, out)
}

/*
 * showEpisodes lists a show's episodes in playing order.
 *
 * Behind Play and Randomize, which are the same list handed over differently:
 * in order, or with the player's own shuffle turned on. Ordered identically to
 * the Continue query, so "next" and "the queue" cannot disagree about what
 * follows what.
 *
 * A dedicated route rather than /api/items?parent_id=, because episodes hang
 * off seasons rather than off the show, so the obvious call returns the seasons
 * and a client would have to walk them — a walk every client would have to
 * reimplement, and get wrong for the shows whose episodes sit directly under
 * the show row.
 */
func (s *Server) showEpisodes(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	if _, err := s.st.GetItem(r.Context(), id, s.userID(r)); s.notFoundOr(w, err, "get item", "no such item") {
		return
	}

	eps, err := s.st.EpisodesOf(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "show episodes")
		return
	}
	// Progress rides along so a list can show what has been watched without a
	// request per episode.
	if err := s.st.AttachProgress(r.Context(), eps, s.userID(r)); err != nil {
		s.writeInternal(w, err, "show episodes progress")
		return
	}
	// Watched state is exactly what must not go stale here either: this list is
	// what the Randomize button queues, and a cached copy would shuffle episodes
	// whose watched flags are out of date.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"episodes": eps})
}
