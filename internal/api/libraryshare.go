package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"lancast/internal/identity"
	"lancast/internal/rating"
	"lancast/internal/store"
)

/*
 * Choosing what a paired server may see
 * ([ADR 0071](../../docs/adr/0071-a-shared-library-is-a-standing-grant.md) §1).
 *
 * # Why these are administrative
 *
 * The ADR does not say, so it is decided here and the reasoning matters,
 * because the route next door goes the other way. Presence grants are
 * deliberately *not* administrative (ADR 0045 §6): a switch somebody else can
 * flip is not consent, so an administrator has no privileged position over who
 * may see you watching.
 *
 * A library share is a different kind of decision. It is about the server's
 * *content* rather than about any person's own consent — it grants another
 * household access to a library, which is the same class of operational power
 * as adding a library or pairing, both of which are already gated here rather
 * than hidden in the client (ADR 0015).
 *
 * Nobody's personal consent is being exercised on their behalf, which is the
 * property that makes presence different. If that ever stops being true — a
 * library somebody considers *theirs* — it is a reason to revisit this, not to
 * quietly widen it.
 */

// sharedLibrary is one library and what this peer has been granted on it.
type sharedLibrary struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Shared bool   `json:"shared"`
	// Ceiling is the age limit for this share, empty for none. Meaningless
	// when SupportsCeiling is false.
	Ceiling  string `json:"ceiling"`
	SharedAt int64  `json:"shared_at,omitempty"`
	/*
	 * SupportsCeiling is false for a library whose contents carry no
	 * certificate and never will — music and pictures (ADR 0071 §6).
	 *
	 * Reported rather than left for the client to infer from the kind, because
	 * the ADR is explicit that the UI must *say* a ceiling does not apply
	 * rather than offer one that does nothing: an inert switch on a sharing
	 * screen is worse than no switch, because it reads as a limit that was
	 * applied.
	 */
	SupportsCeiling bool `json:"supports_ceiling"`
	/*
	 * Unrated and Total are what a ceiling would cost, shown to the host
	 * before they choose (ADR 0071 §6).
	 *
	 * An unrated item is blocked, and those items then vanish from the
	 * friend's view with no explanation. The mitigation is not to explain it
	 * to the friend, who should not be told what they cannot see, but to tell
	 * the host at the moment they decide. A number the host sees beats a
	 * mystery the friend does not.
	 */
	Unrated int `json:"unrated"`
	Total   int `json:"total"`
}

/*
 * ceilingApplies reports whether a ceiling means anything for a library kind.
 *
 * Decided by kind rather than by counting what is in the library, because the
 * ADR's reason is that no certificate *exists* for music or pictures — a
 * statement about the kind, not about how many rows happen to be there today.
 * Counting would also make an empty film library report that a ceiling does
 * not apply, and then change its mind after a scan.
 */
func ceilingApplies(kind string) bool {
	return kind == "movie" || kind == "show"
}

// listShares answers what one peer may see, and what choosing a ceiling would
// cost on each library.
func (s *Server) listShares(w http.ResponseWriter, r *http.Request) {
	fingerprint, ok := s.pairedFingerprint(w, r)
	if !ok {
		return
	}

	libs, err := s.st.ListLibraries(r.Context())
	if err != nil {
		s.writeInternal(w, err, "list libraries")
		return
	}
	shares, err := s.st.SharesTo(r.Context(), fingerprint)
	if err != nil {
		s.writeInternal(w, err, "list shares")
		return
	}
	granted := make(map[int64]store.LibraryShare, len(shares))
	for _, sh := range shares {
		granted[sh.LibraryID] = sh
	}

	out := make([]sharedLibrary, 0, len(libs))
	for _, l := range libs {
		row := sharedLibrary{
			ID: l.ID, Name: l.Name, Kind: l.Kind,
			SupportsCeiling: ceilingApplies(l.Kind),
		}
		if sh, ok := granted[l.ID]; ok {
			row.Shared, row.Ceiling, row.SharedAt = true, sh.Ceiling, sh.SharedAt
		}
		if row.SupportsCeiling {
			// Counted for every video library, shared or not: the host needs
			// to know the cost *before* choosing, not after.
			if unrated, total, err := s.st.UnratedInShare(r.Context(), l.ID); err == nil {
				row.Unrated, row.Total = unrated, total
			}
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"libraries": out})
}

// putShare grants a library to a peer, or changes its ceiling.
func (s *Server) putShare(w http.ResponseWriter, r *http.Request) {
	fingerprint, ok := s.pairedFingerprint(w, r)
	if !ok {
		return
	}
	libraryID, ok := s.shareLibraryID(w, r)
	if !ok {
		return
	}

	var body struct {
		Ceiling string `json:"ceiling"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not read the request")
		return
	}
	if body.Ceiling != "" && !rating.Known(body.Ceiling) {
		writeError(w, http.StatusBadRequest, "bad_request",
			"that is not a rating this server can place")
		return
	}

	/*
	 * A ceiling on a library that cannot carry one is refused rather than
	 * stored and ignored. Storing it would leave a host believing a limit was
	 * in force that does nothing — the same failure the store refuses an
	 * unknown label for.
	 */
	l, err := s.st.GetLibrary(r.Context(), libraryID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such library")
		return
	}
	if err != nil {
		s.writeInternal(w, err, "library for share")
		return
	}
	if body.Ceiling != "" && !ceilingApplies(l.Kind) {
		writeError(w, http.StatusBadRequest, "bad_request",
			"nothing in this library carries a certificate, so a limit would do nothing")
		return
	}

	if err := s.st.ShareLibrary(r.Context(), fingerprint, libraryID, body.Ceiling, time.Now()); err != nil {
		s.writeInternal(w, err, "share library")
		return
	}
	s.log.Info("library shared with a peer", "peer", fingerprint, "library", libraryID,
		"name", l.Name, "ceiling", body.Ceiling)
	w.WriteHeader(http.StatusNoContent)
}

// deleteShare takes a grant away. It applies to the peer's next request; a
// stream already in flight finishes (ADR 0071 §6).
func (s *Server) deleteShare(w http.ResponseWriter, r *http.Request) {
	fingerprint, ok := s.pairedFingerprint(w, r)
	if !ok {
		return
	}
	libraryID, ok := s.shareLibraryID(w, r)
	if !ok {
		return
	}
	if err := s.st.UnshareLibrary(r.Context(), fingerprint, libraryID); err != nil {
		s.writeInternal(w, err, "unshare library")
		return
	}
	s.log.Info("library unshared from a peer", "peer", fingerprint, "library", libraryID)
	w.WriteHeader(http.StatusNoContent)
}

/*
 * pairedFingerprint resolves the peer in the path and refuses one this server
 * does not know.
 *
 * Unlike minting, an *added* peer is acceptable here: a host may reasonably
 * decide what to share before the pairing has completed, and the share simply
 * grants nothing until it does. What matters is that the peer is one this
 * server has been introduced to, so a share cannot name a stranger.
 */
func (s *Server) pairedFingerprint(w http.ResponseWriter, r *http.Request) (string, bool) {
	fingerprint := identity.Normalize(r.PathValue("fingerprint"))
	if fingerprint == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "which server")
		return "", false
	}
	if _, err := s.st.PeerByFingerprint(r.Context(), fingerprint); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such peer")
		return "", false
	}
	return fingerprint, true
}

func (s *Server) shareLibraryID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("library"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid library id")
		return 0, false
	}
	return id, true
}
