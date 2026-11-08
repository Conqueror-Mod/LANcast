package api

import (
	"net/http"

	"lancast/internal/store"
)

/*
 * GET /api/home/suggestion — the Suggested spotlight's fallback (ADR 0119 §1).
 *
 * Suggested seeds from what you are part-way through. With nothing part-way
 * through it used to fall back to Continue, and the spotlight became a
 * monument to the last thing anybody watched. This answers from what you
 * finished instead: the genre your recent finished films and shows share most
 * (store.RecentTaste), and films and shows in it you have not begun.
 *
 * The genre and its count come back with the items, because the spotlight
 * states them: "Because you've finished 6 Thriller titles lately". No genre
 * reaching three titles is `taste: null` and no items, so the client falls
 * back the way it always did, rather than making a claim about a coincidence.
 *
 * `seed` shuffles the candidates the way the Unwatched shelves are shuffled,
 * once per visit, so the pick changes between visits and not under the
 * pointer.
 */

// suggestionKinds are the kinds the spotlight can show from a finished
// history: what was finished, films and shows.
var suggestionKinds = map[string]bool{"movie": true, "show": true}

// notSuggestable are listed for the query so the window it fills is spent on
// films and shows. The kinds are an open set, so the result is filtered by
// suggestionKinds as well; this list only keeps a music genre of the same name
// from crowding them out.
var notSuggestable = []string{
	"collection", "playlist", "artist", "album", "track", "gallery", "photo",
	"book", "book_part", "comic", "rom",
}

const suggestionLimit = 20

func (s *Server) homeSuggestion(w http.ResponseWriter, r *http.Request) {
	user := s.userID(r)
	type taste struct {
		Genre  string `json:"genre"`
		Titles int    `json:"titles"`
	}
	empty := func() {
		writeJSON(w, http.StatusOK, map[string]any{"taste": nil, "items": []store.Item{}})
	}

	t, ok, err := s.st.RecentTaste(r.Context(), user)
	if err != nil {
		s.writeInternal(w, err, "recent taste")
		return
	}
	if !ok {
		empty()
		return
	}

	hidden, err := s.hiddenFor(r)
	if err != nil {
		s.writeInternal(w, err, "check library access")
		return
	}
	items, _, err := s.st.ListItems(r.Context(), store.ItemFilter{
		Genres:           []string{t.Genre},
		UserID:           user,
		Unstarted:        true,
		TopLevel:         true,
		ExcludeMissing:   true,
		ExcludeKinds:     notSuggestable,
		MaxContentRating: s.ceilingFor(r),
		HiddenLibraries:  hidden,
		Sort:             "random",
		Seed:             int64(queryInt(r, "seed")),
		Limit:            suggestionLimit,
	})
	if err != nil {
		s.writeInternal(w, err, "suggestion candidates")
		return
	}
	kept := items[:0]
	for _, it := range items {
		if suggestionKinds[it.Kind] {
			kept = append(kept, it)
		}
	}
	kept, err = s.st.PermittedItems(r.Context(), user, kept)
	if err != nil {
		s.writeInternal(w, err, "apply account limits")
		return
	}
	// The spotlight needs a backdrop to show anything at all.
	if err := s.st.AttachArtwork(r.Context(), kept); err != nil {
		s.writeInternal(w, err, "suggestion artwork")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"taste": taste{Genre: t.Genre, Titles: t.Titles},
		"items": kept,
	})
}
