package api

import (
	"net/http"
	"strconv"
	"time"

	"lancast/internal/store"
)

/*
 * A year, computed here and sent nowhere.
 *
 * The whole appeal of this is that it is not a marketing artefact: everything
 * it says comes off `playback_state`, which has never left the machine, and no
 * part of producing it contacts anything. That also means the honesty is not
 * decoration — a page like this is believed, so a number that is quietly
 * inflated does more damage here than an error would.
 *
 * Your own year and nobody else's. Viewing is private by default
 * ([ADR 0035](../../docs/adr/0035-who-may-see-whose-viewing.md)) and there is
 * deliberately no route that takes another account's id: the sharing opt-in
 * publishes *what was watched*, never somebody's year assembled for them by
 * somebody else.
 */

// yearResponse is one account's year, plus the years it could ask about.
type yearResponse struct {
	store.YearInReview
	/*
	 * Years is every year this account has history in, newest first.
	 *
	 * Travelling with the year itself so the picker exists on first paint: a
	 * second request to discover which years are worth asking for would put a
	 * spinner on a control that is four numbers wide.
	 */
	Years []int `json:"years"`
	/*
	 * Partial says the year is still running.
	 *
	 * The difference between "your 2025" and "your 2025 so far" is the
	 * difference between a summary and a claim, and only the server knows which
	 * one it just computed.
	 */
	Partial bool `json:"partial"`
}

/*
 * yearInReview answers GET /api/profile/year.
 *
 * The year is a query parameter rather than a path segment because the default
 * — this year — is the overwhelmingly common request, and a client that has to
 * name a year before it can ask for the obvious one is a client that has to
 * know what year it is in the server's timezone.
 */
func (s *Server) yearInReview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := s.userID(r)

	// The server's local year, deliberately, since that is the calendar the
	// history is bucketed on. A client in another timezone asking for "now"
	// gets the household's year rather than its own, which is the right answer
	// on a server whose library sits in one house.
	now := time.Now()
	year := now.Year()
	if v := r.URL.Query().Get("year"); v != "" {
		parsed, err := strconv.Atoi(v)
		/*
		 * A year outside anything a media library could hold is refused rather
		 * than answered with an empty summary, because the two look identical
		 * on screen and only one of them is worth showing.
		 */
		if err != nil || parsed < 1900 || parsed > now.Year()+1 {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid year")
			return
		}
		year = parsed
	}

	review, err := s.st.YearInReview(ctx, userID, year)
	if err != nil {
		s.writeInternal(w, err, "year in review")
		return
	}
	// The first and last of the year are shown as tiles, so they need posters
	// for the same reason every other list endpoint attaches them.
	ends := []store.Item{}
	if review.First != nil {
		ends = append(ends, *review.First)
	}
	if review.Last != nil {
		ends = append(ends, *review.Last)
	}
	if len(ends) > 0 {
		if err := s.st.AttachArtwork(ctx, ends); err != nil {
			s.writeInternal(w, err, "attach artwork")
			return
		}
		review.First = &ends[0]
		if len(ends) > 1 {
			review.Last = &ends[1]
		} else {
			// One title in the whole year: it is both ends, and giving the two
			// fields separate copies of one row is how they drift apart.
			review.Last = &ends[0]
		}
	}

	years, err := s.st.WatchYears(ctx, userID)
	if err != nil {
		s.writeInternal(w, err, "watch years")
		return
	}

	writeJSON(w, http.StatusOK, yearResponse{
		YearInReview: review,
		Years:        years,
		Partial:      year == now.Year(),
	})
}
