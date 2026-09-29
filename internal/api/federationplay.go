package api

import (
	"context"
	"net/http"
	"strconv"

	"lancast/internal/store"
)

/*
 * Playing a shared item, asked for by the friend's own server.
 *
 * [ADR 0071's amendment](../../docs/adr/0071-a-shared-library-is-a-standing-grant.md)
 * settled the route: the friend's client cannot reach this server, so their
 * server asks, over the mutual-TLS peer channel, and passes the bytes on.
 * `federationStream` already served the file that way. This is the rest of the
 * playback surface — the delivery decision, the progressive transcode, HLS and
 * subtitles — because a file this household can play directly is the minority
 * case and a friend who can only watch those cannot really watch anything.
 *
 * # Nothing here re-implements playback
 *
 * Every route below authorises and then calls **the handler that already
 * serves it locally**. The decision about codecs, the ffmpeg arguments, the
 * session bookkeeping and the containment check are one implementation, used
 * by the household and by a friend alike.
 *
 * That is the same argument `writeSharedLibraries` makes about browsing, and
 * it matters more here: the transcode path is where the hard-won things live
 * (ADR 0072's alignment, the complete-playlist fix, `decodeAccel`), and a
 * second copy would be a second place for a service in session 0 to fail
 * differently.
 *
 * # What is *not* shared with the local path
 *
 * Two things, and both are corrections rather than special cases:
 *
 * **Who owns the transcode session.** A running encode is keyed by item and
 * owner so a seek replaces the viewer's own stream instead of starting a
 * second one beside it. With no session, `userID` answers the local owner —
 * so a friend seeking would have evicted the household's own stream of the
 * same film, and the household's seek would have killed the friend's. See
 * `streamOwner`.
 *
 * **What the playlist's segment URLs say.** They are server-absolute, and the
 * server they are absolute to is the friend's, not this one. See
 * `playlistPrefix`.
 */

/*
 * federationPlay is the whole permission decision for one item, in one place.
 *
 * It is a wrapper rather than a check inside each handler for the reason
 * `guestgate.go` gives at length: a check that lives where it can be omitted
 * will be omitted, and the handler it was omitted from streams whatever id it
 * is handed. Here a route either goes through this or is not reachable by a
 * peer at all.
 *
 * `MayPlay` with a Friend principal **fails closed** — an unshared library, an
 * item above that share's ceiling, and a question that could not be answered
 * are all refusals — which is the opposite of what an account with no row
 * gets, and the whole reason the two principals were split (ADR 0071 §6).
 *
 * A refusal is 404, indistinguishable from an item that is not here, so this
 * cannot be used to learn what the household holds.
 */
func (s *Server) federationPlay(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fingerprint, ok := s.federationPeer(w, r)
		if !ok {
			return
		}

		/*
		 * From the path when the route has one, otherwise the query.
		 *
		 * The HLS routes carry the item in the path because their *playlist*
		 * has to name segments with a prefix and no query string; the flat
		 * routes keep `?item=` because `federationStream` shipped with it and
		 * changing a route a paired server may already be calling is a
		 * compatibility break for no gain.
		 */
		raw := r.PathValue("item")
		if raw == "" {
			raw = r.URL.Query().Get("item")
		}
		itemID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || itemID <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "which item")
			return
		}

		allowed, err := s.st.MayPlay(r.Context(), store.Friend(fingerprint), itemID)
		if err != nil || !allowed {
			writeError(w, http.StatusNotFound, "not_found", "no such item")
			return
		}

		/*
		 * The handlers below read the item from the path segment `id`, because
		 * that is how they are routed locally. Setting it here lets them stay
		 * exactly as they are — the alternative is a second parameter threaded
		 * through playback, transcode, HLS and subtitles for the sole benefit
		 * of this file.
		 */
		r.SetPathValue("id", strconv.FormatInt(itemID, 10))
		r = r.WithContext(withStreamOwner(r.Context(), peerOwner(fingerprint)))
		h(w, r)
	}
}

/*
 * federationHLSPlaylist is the one route that cannot simply delegate.
 *
 * `hlsPlaylist` writes segment URLs under `/api/stream/{id}/hls/{session}/`,
 * which is correct for a player talking to this server and wrong for every
 * player that will actually receive this playlist: they are talking to their
 * *own* server, where that path names a different item entirely.
 *
 * So a peer's playlist names the federation route instead, and the friend's
 * server rewrites that prefix to its own proxy path before handing it on. The
 * rewrite has to happen somewhere; it happens there because only that server
 * knows what it calls us.
 */
func (s *Server) federationHLSPlaylist(w http.ResponseWriter, r *http.Request) {
	item := r.PathValue("id")
	r = r.WithContext(withPlaylistPrefix(r.Context(),
		"/api/federation/hls/"+item+"/"))
	s.hlsPlaylist(w, r)
}

// --- who a running encode belongs to ---------------------------------------

type streamOwnerKeyType struct{}
type playlistPrefixKeyType struct{}

var streamOwnerKey streamOwnerKeyType
var playlistPrefixKey playlistPrefixKeyType

// peerOwner namespaces a peer so a fingerprint can never collide with an
// account id. Account ids are `u_…` (and the local owner is a fixed string),
// so the prefix is belt and braces rather than the only thing keeping them
// apart — but a key that two different kinds of caller could produce is worth
// making impossible rather than unlikely.
func peerOwner(fingerprint string) string { return "peer:" + fingerprint }

func withStreamOwner(ctx context.Context, owner string) context.Context {
	return context.WithValue(ctx, streamOwnerKey, owner)
}

func withPlaylistPrefix(ctx context.Context, prefix string) context.Context {
	return context.WithValue(ctx, playlistPrefixKey, prefix)
}

/*
 * streamOwner is who a transcode session belongs to.
 *
 * Not `userID`, which answers the local owner for anybody without a session —
 * and a friend has no session here by construction. Two consequences of that
 * answer, both of which were live before this existed:
 *
 * - A friend seeking evicts the household's own stream of the same film, and
 *   the household seeking kills the friend's. The encode is keyed by item and
 *   owner precisely so that does not happen.
 * - Every friend from every paired server shares one owner, so two friends
 *   watching the same film fight over one session.
 *
 * A guest holding a ticket (ADR 0046) reached the same handlers through
 * `guestgate.go` and had the same problem, so it is answered here for both
 * ways in rather than only for the one being built today.
 */
func (s *Server) streamOwner(r *http.Request) string {
	if owner, ok := r.Context().Value(streamOwnerKey).(string); ok && owner != "" {
		return owner
	}
	if g, ok := guestFromContext(r); ok {
		return peerOwner(g.Peer)
	}
	return s.userID(r)
}

// playlistPrefix is what a playlist's segment URLs are relative to. The local
// shape unless a caller has been given a different one.
func playlistPrefix(r *http.Request, itemID int64, session string) string {
	if p, ok := r.Context().Value(playlistPrefixKey).(string); ok && p != "" {
		return p + session + "/"
	}
	return "/api/stream/" + itoa64(itemID) + "/hls/" + session + "/"
}

/*
 * federationPresenceTitle answers what a friend's server may say somebody is
 * watching (ADR 0045's second amendment, §10).
 *
 * The title is computed **here**, by `presenceTitle`, because §3's reductions —
 * video only, the work and never the episode — are that function and must stay
 * one implementation. The viewer's server records what comes back; it does not
 * derive a title of its own, and neither does their client.
 *
 * An empty title is a complete answer and the common one: music, a photograph,
 * an episode whose series is unknown. The caller records nothing for it, which
 * is silence rather than a guess.
 *
 * It rides `federationPlay`, so it discloses nothing about an item the caller
 * could not already play — a refusal is the same 404 every other route gives.
 */
func (s *Server) federationPresenceTitle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "which item")
		return
	}
	// No account: the ceiling that matters was applied by federationPlay with a
	// Friend principal, and passing one here would apply this household's limit
	// to somebody else's.
	it, err := s.st.GetItem(r.Context(), id, "")
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such item")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"title": presenceTitle(it)})
}
