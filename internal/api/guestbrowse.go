package api

import (
	"errors"
	"net/http"
	"strconv"

	"lancast/internal/store"
)

/*
 * What a friend may browse
 * ([ADR 0071](../../docs/adr/0071-a-shared-library-is-a-standing-grant.md) §3).
 *
 * # Why these are their own routes
 *
 * `/api/libraries` and `/api/items` answer about everything this server holds,
 * and making them safe for a friend would mean every one of them remembering
 * to narrow itself. §3 calls that out by name: an unscoped read with a filter
 * applied afterwards is the same mistake as a route-level permission.
 *
 * These take **no library from the caller that is not checked against the
 * share**, and derive the scope from the session rather than from the request.
 * A handler here cannot return something outside the share by forgetting
 * something; there is nothing to forget.
 *
 * # One library at a time, and that is a real limit
 *
 * A ceiling rides on the *share* — per peer, per library (§6) — so two shared
 * libraries can carry different limits. `ListItems` applies one ceiling to one
 * query, so a search spanning libraries with different limits has no single
 * correct answer: the strictest would hide things the host permitted, and the
 * loosest would show things the host did not.
 *
 * So browsing and searching both name a library, and the ceiling is that
 * share's. A search across everything a friend may see is a reasonable thing
 * to want and is **owed**, not refused on principle; doing it honestly needs
 * the query to carry a ceiling per library rather than one for the lot.
 */

// guestLibrary is one shared library, as the friend sees it.
type guestLibrary struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

/*
 * guestLibraries lists what this peer was granted.
 *
 * Resolved per request, so un-sharing and unpairing take effect immediately
 * and there is nothing cached to invalidate. A peer granted nothing gets an
 * empty list rather than an error: "you may see nothing here" is an answer,
 * and it is the answer a client needs to render honestly.
 */
func (s *Server) guestLibraries(w http.ResponseWriter, r *http.Request) {
	g, ok := guestFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not a guest session")
		return
	}
	s.writeSharedLibraries(w, r, g.Peer)
}

/*
 * writeSharedLibraries and writeSharedItems are the *only* implementation of
 * what a peer may see, and both ways in call them.
 *
 * A guest session (ADR 0046's ticket) and the mutual-TLS peer channel
 * (ADR 0071's amendment) authenticate differently and authorise identically —
 * both resolve to a peer fingerprint, and everything after that is the same
 * question. Two copies of this would be two chances to narrow one and not the
 * other, and the one that was missed would be a hole nobody was looking at.
 */
func (s *Server) writeSharedLibraries(w http.ResponseWriter, r *http.Request, peerFP string) {
	ids, err := s.st.SharedLibraries(r.Context(), peerFP)
	if err != nil {
		s.writeInternal(w, err, "shared libraries")
		return
	}
	out := make([]guestLibrary, 0, len(ids))
	for _, id := range ids {
		l, err := s.st.GetLibrary(r.Context(), id)
		if err != nil {
			// A share whose library has gone is not an error to report to
			// somebody else's server; it is simply not there.
			continue
		}
		out = append(out, guestLibrary{ID: l.ID, Name: l.Name, Kind: l.Kind})
	}
	writeJSON(w, http.StatusOK, map[string]any{"libraries": out})
}

/*
 * guestItems browses or searches one shared library.
 *
 * The scope and the ceiling both come from the share, looked up together, so
 * there is no path where one is applied and the other is not. CeilingFor fails
 * closed: a library that is not shared answers ErrNotShared, which is a 404
 * here — indistinguishable from one that does not exist, so a friend cannot
 * use this to learn what the host holds.
 */
func (s *Server) guestItems(w http.ResponseWriter, r *http.Request) {
	g, ok := guestFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "not a guest session")
		return
	}
	s.writeSharedItems(w, r, g.Peer)
}

// writeSharedItems is the scoped listing, shared by both ways in. See
// writeSharedLibraries for why there is only one of it.
func (s *Server) writeSharedItems(w http.ResponseWriter, r *http.Request, peerFP string) {
	libraryID, err := strconv.ParseInt(r.URL.Query().Get("library"), 10, 64)
	if err != nil || libraryID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "which library")
		return
	}

	ceiling, err := s.st.CeilingFor(r.Context(), peerFP, libraryID)
	if errors.Is(err, store.ErrNotShared) {
		writeError(w, http.StatusNotFound, "not_found", "no such library")
		return
	}
	if err != nil {
		s.writeInternal(w, err, "share ceiling")
		return
	}

	limit, offset := pageParams(r)
	f := store.ItemFilter{
		/*
		 * Scope rather than LibraryID, and only Scope.
		 *
		 * Both would narrow to the same library today, and that is the problem:
		 * with two mechanisms one of them is doing nothing, and a test that
		 * removes it finds nothing wrong. Removing LibraryID makes the
		 * restriction the *only* thing standing between this handler and the
		 * rest of the library, so breaking it breaks tests.
		 *
		 * Scope is the right one to keep: it is applied last, after every
		 * other narrowing, so nothing added to this filter later can widen
		 * past it.
		 */
		Scoped:           true,
		Scope:            []int64{libraryID},
		Kind:             r.URL.Query().Get("kind"),
		Query:            r.URL.Query().Get("q"),
		Sort:             r.URL.Query().Get("sort"),
		MaxContentRating: ceiling,
		TopLevel:         r.URL.Query().Get("kind") == "",
		Limit:            limit,
		Offset:           offset,
	}

	items, total, err := s.st.ListItems(r.Context(), f)
	if err != nil {
		s.writeInternal(w, err, "guest items")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": total,
	})
}

// pageParams reads a bounded page window. Bounded because the caller is
// another household's client and a limit it chooses is a limit this server
// pays for.
func pageParams(r *http.Request) (limit, offset int) {
	limit = 60
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = min(n, 200)
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && n > 0 {
		offset = n
	}
	return limit, offset
}
