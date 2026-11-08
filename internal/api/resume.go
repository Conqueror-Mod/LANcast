package api

import (
	"net/http"
	"sort"
	"time"

	"lancast/internal/store"
)

// resumePerKind is how many cards of one kind the console shows (ADR 0118 §2):
// enough that two films in progress both appear, few enough that one kind
// cannot fill the panel.
const resumePerKind = 2

/*
 * resume answers GET /api/resume: the home page's resume console.
 *
 * The server decides what is in progress and the client draws it. Every kind
 * comes back in one card shape, newest first, with at most two of a kind
 * unless ?all=true asks for the See all page. `more` says whether anything was
 * left out, so the console knows to offer See all.
 */
func (s *Server) resume(w http.ResponseWriter, r *http.Request) {
	cur := s.settings.Get()
	all := r.URL.Query().Get("all") == "true"
	limit := queryInt(r, "limit")
	if limit <= 0 {
		limit = 5
		if all {
			limit = 100
		}
	}
	if limit > 100 {
		limit = 100
	}
	perKind := resumePerKind + 1 // one spare, to know whether there is more
	if all {
		perKind = limit
	}
	user := s.userID(r)
	cards, err := s.st.ResumeCandidates(r.Context(), user, perKind,
		cur.ContinueCutoff(time.Now()))
	if err != nil {
		s.writeInternal(w, err, "resume")
		return
	}

	// The account's limits (ADR 0084), as Continue Watching applies them: the
	// queries behind the console build their own SQL, so neither the rating
	// ceiling nor a hidden library reached them.
	items := make([]store.Item, len(cards))
	for i, c := range cards {
		items[i] = c.Item
	}
	allowed, err := s.st.PermittedItems(r.Context(), user, items)
	if err != nil {
		s.writeInternal(w, err, "apply account limits")
		return
	}
	ok := make(map[int64]bool, len(allowed))
	for _, it := range allowed {
		ok[it.ID] = true
	}

	sort.SliceStable(cards, func(i, j int) bool { return cards[i].TouchedAt > cards[j].TouchedAt })
	out := []store.ResumeCard{}
	perKindShown := map[string]int{}
	more := false
	for _, c := range cards {
		if !ok[c.Item.ID] {
			continue
		}
		if len(out) == limit || (!all && perKindShown[c.Kind] == resumePerKind) {
			more = true
			continue
		}
		perKindShown[c.Kind]++
		out = append(out, c)
	}

	shown := make([]store.Item, len(out))
	for i, c := range out {
		shown[i] = c.Item
	}
	if err := s.attachShelfArtwork(r, shown); err != nil {
		s.writeInternal(w, err, "attach artwork")
		return
	}
	for i := range out {
		out[i].Item = shown[i]
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": out, "more": more})
}

/*
 * attachShelfArtwork attaches artwork to a shelf's items and to the episode a
 * show would play next.
 *
 * Next up draws that episode -- its still, not the show's poster -- and
 * without the second pass the still was never in the response: artwork was
 * attached to the rows and next_episode is not a row. Copied into a slice and
 * back because AttachArtwork works on a slice of values.
 */
func (s *Server) attachShelfArtwork(r *http.Request, items []store.Item) error {
	if err := s.st.AttachArtwork(r.Context(), items); err != nil {
		return err
	}
	var next []store.Item
	for i := range items {
		if items[i].NextEpisode != nil {
			next = append(next, *items[i].NextEpisode)
		}
	}
	if len(next) == 0 {
		return nil
	}
	if err := s.st.AttachArtwork(r.Context(), next); err != nil {
		return err
	}
	j := 0
	for i := range items {
		if items[i].NextEpisode != nil {
			ep := next[j]
			items[i].NextEpisode = &ep
			j++
		}
	}
	return nil
}
