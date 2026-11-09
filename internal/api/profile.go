package api

import (
	"net/http"
	"strconv"

	"lancast/internal/store"
)

/*
 * Who you are, and what you have watched.
 *
 * One request rather than three, for the same reason /api/activity is one
 * request: a page that needs identity, totals and a list in order to render
 * anything should not have to discover that from three round trips and three
 * loading states.
 *
 * Strictly the caller's own. There is no /api/profile/{id}, and adding one is
 * a decision rather than a route — "what has everyone been watching" is the
 * viewer-stats item on the roadmap, and it needs an answer to who may see it
 * before it needs a handler.
 */
type profileResponse struct {
	User    profileUser          `json:"user"`
	Stats   store.ProfileStats   `json:"stats"`
	History []store.HistoryEntry `json:"history"`
	// HasMore says whether the history was cut short, so the client can offer
	// to fetch the next page instead of guessing from a full-looking one.
	HasMore bool `json:"has_more"`
	// MostPlayed is what was finished most often, up to five in each group
	// (store.MostPlayed): films and episodes, songs, audiobook chapters.
	MostPlayed []store.PlayedItem `json:"most_played"`
}

type profileUser struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Admin bool   `json:"admin"`
	// Secured is false on an unconfigured loopback server, where there is no
	// account and the history belongs to the migrated 'local' id. The page says
	// so rather than inventing a person.
	Secured bool `json:"secured"`
}

// attachArtworkTo attaches posters to items that live inside other rows (a
// history entry, a rating). AttachArtwork works on a slice of values, so they
// are copied out and back.
func (s *Server) attachArtworkTo(r *http.Request, items []*store.Item) error {
	vals := make([]store.Item, len(items))
	for i, it := range items {
		vals[i] = *it
	}
	if err := s.st.AttachArtwork(r.Context(), vals); err != nil {
		return err
	}
	for i := range items {
		*items[i] = vals[i]
	}
	return nil
}

func (s *Server) profile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := s.userID(r)

	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 200 {
		limit = v
	}
	offset := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v > 0 {
		offset = v
	}
	group, ok := historyGroup(w, r)
	if !ok {
		return
	}

	// One more than asked for: the extra row is how HasMore is known without a
	// second COUNT over the same join.
	entries, err := s.st.History(ctx, userID, group, limit+1, offset)
	if err != nil {
		s.writeInternal(w, err, "history")
		return
	}
	more := len(entries) > limit
	if more {
		entries = entries[:limit]
	}

	// Each row draws a poster, so the posters come with the rows (store.Item
	// carries none until AttachArtwork). Missing here, every row on the page
	// drew the first letter of its title instead.
	rows := make([]*store.Item, len(entries))
	for i := range entries {
		rows[i] = &entries[i].Item
	}
	if err := s.attachArtworkTo(r, rows); err != nil {
		s.writeInternal(w, err, "attach artwork")
		return
	}

	most, err := s.st.MostPlayed(ctx, userID, 5)
	if err != nil {
		s.writeInternal(w, err, "most played")
		return
	}
	mostRows := make([]*store.Item, len(most))
	for i := range most {
		mostRows[i] = &most[i].Item
	}
	if err := s.attachArtworkTo(r, mostRows); err != nil {
		s.writeInternal(w, err, "attach artwork")
		return
	}

	stats, err := s.st.ProfileStatistics(ctx, userID)
	if err != nil {
		s.writeInternal(w, err, "profile statistics")
		return
	}

	who := profileUser{ID: userID, Name: "Local", Secured: s.secured(ctx)}
	if sess, ok := sessionFromContext(r); ok {
		who.ID = sess.UserID
		if u, err := s.st.UserByID(ctx, sess.UserID); err == nil {
			who.Name = u.Name
			who.Admin = u.Role == store.RoleAdmin
		}
	}

	writeJSON(w, http.StatusOK, profileResponse{
		User: who, Stats: stats, History: entries, HasMore: more, MostPlayed: most,
	})
}

// historyGroup reads ?kind= for either history list (profile-2-plan §4e):
// video, music, books or comics, or nothing for everything. Anything else is a
// 400 rather than everything, so a typo cannot pass for an empty filter.
func historyGroup(w http.ResponseWriter, r *http.Request) (store.HistoryGroup, bool) {
	g := store.HistoryGroup(r.URL.Query().Get("kind"))
	if !g.Valid() {
		writeError(w, http.StatusBadRequest, "bad_request", "kind must be video, music, books or comics")
		return "", false
	}
	return g, true
}

/*
 * GET /api/profile/kinds — the Overview's books, comics and games
 * (profile-2-plan §4d): what is in progress, what was finished, and for a game
 * its newest save, there being no play time to report.
 *
 * The account's limits (ADR 0084) apply here as on the resume console, whose
 * queries these are: a hidden library or a rating above the ceiling leaves
 * the lists.
 */
func (s *Server) profileKinds(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := s.userID(r)
	k, err := s.st.ProfileKindsFor(ctx, user, 6)
	if err != nil {
		s.writeInternal(w, err, "profile kinds")
		return
	}

	var all []store.Item
	for _, l := range [][]store.KindEntry{k.Books.InProgress, k.Books.Finished, k.Comics.InProgress, k.Comics.Finished} {
		for _, e := range l {
			all = append(all, e.Item)
		}
	}
	for _, g := range k.Games {
		all = append(all, g.Item)
	}
	allowed, err := s.st.PermittedItems(ctx, user, all)
	if err != nil {
		s.writeInternal(w, err, "apply account limits")
		return
	}
	ok := make(map[int64]bool, len(allowed))
	for _, it := range allowed {
		ok[it.ID] = true
	}
	keep := func(l []store.KindEntry) []store.KindEntry {
		out := []store.KindEntry{}
		for _, e := range l {
			if ok[e.Item.ID] {
				out = append(out, e)
			}
		}
		return out
	}
	k.Books.InProgress, k.Books.Finished = keep(k.Books.InProgress), keep(k.Books.Finished)
	k.Comics.InProgress, k.Comics.Finished = keep(k.Comics.InProgress), keep(k.Comics.Finished)
	games := []store.SavedGame{}
	for _, g := range k.Games {
		if ok[g.Item.ID] {
			games = append(games, g)
		}
	}
	k.Games = games

	var rows []*store.Item
	for _, l := range [][]store.KindEntry{k.Books.InProgress, k.Books.Finished, k.Comics.InProgress, k.Comics.Finished} {
		for i := range l {
			rows = append(rows, &l[i].Item)
		}
	}
	for i := range k.Games {
		rows = append(rows, &k.Games[i].Item)
	}
	if err := s.attachArtworkTo(r, rows); err != nil {
		s.writeInternal(w, err, "attach artwork")
		return
	}
	writeJSON(w, http.StatusOK, k)
}
