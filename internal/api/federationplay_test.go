package api

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"

	"lancast/internal/identity"
	"lancast/internal/store"
)

/*
 * Playing a shared item, asked for by the friend's own server (ADR 0071 §5).
 *
 * What is under test here is the **gate**, not the transcode. Every route
 * delegates to a handler that already has its own tests, and re-testing ffmpeg
 * through a peer would be testing ffmpeg. What is new — and what the whole
 * feature's safety rests on — is that a peer reaching these routes is
 * authorised per item, fails closed, and cannot tell a refusal from an absence.
 *
 * Two genuinely new mechanisms are tested on their own below, because neither
 * is visible in a status code: who a running encode belongs to, and what a
 * playlist's segment URLs point at. Both were bugs waiting rather than
 * features — see their comments.
 */

// playRoutes is every route a peer may use to play something, as a table, so a
// route added later without a test is a route missing from this list.
func playRoutes(s *Server) []struct {
	name    string
	target  string
	item    string
	handler http.HandlerFunc
} {
	return []struct {
		name    string
		target  string
		item    string
		handler http.HandlerFunc
	}{
		{"decision", "/api/federation/playback?item=", "", s.federationPlay(s.playback)},
		{"file", "/api/federation/stream?item=", "", s.federationStream},
		{"transcode", "/api/federation/transcode?item=", "", s.federationPlay(s.transcodeStream)},
		{"playlist", "/api/federation/hls/%d/index.m3u8", "item", s.federationPlay(s.federationHLSPlaylist)},
		{"segment", "/api/federation/hls/%d/sess/1.m4s", "item", s.federationPlay(s.hlsSegment)},
		{"subtitles", "/api/federation/subtitles?item=", "", s.federationPlay(s.listSubtitles)},
	}
}

// playRequest builds a peer request for one route, putting the item wherever
// that route carries it.
func playRequest(t *testing.T, id identity.Identity, target, itemSeg string, item int64) *http.Request {
	t.Helper()
	if itemSeg == "" {
		return peerRequest(t, id, target+itoa64(item))
	}
	r := peerRequest(t, id, "/api/federation/hls/"+itoa64(item)+"/index.m3u8")
	// The router would set this; these tests call the handler directly.
	r.SetPathValue("item", itoa64(item))
	return r
}

/*
 * The gate refuses before it asks anything, and says the same thing for an
 * item that is not shared as for one that does not exist.
 *
 * Three separate refusals collapsed into one table on purpose: they must be
 * indistinguishable from outside, and a test that asserted them in three
 * different shapes would stop noticing if one of them started being more
 * informative than the others.
 */
func TestAPeerMayNotPlayWhatWasNotSharedWithIt(t *testing.T) {
	f := newFedFixture(t)
	item := f.h.addFile(t, "a film.mkv", []byte("not really a film"))

	for _, route := range playRoutes(f.h.srvAPI) {
		t.Run(route.name, func(t *testing.T) {
			// Paired, but nothing shared: the library is not in the grant.
			w := f.call(route.handler,
				playRequest(t, f.georgia, route.target, route.item, item))
			if w.Code != http.StatusNotFound {
				t.Errorf("unshared: status = %d, want 404", w.Code)
			}

			// An item that does not exist answers identically, which is what
			// stops this being used to enumerate the household's library.
			w = f.call(route.handler,
				playRequest(t, f.georgia, route.target, route.item, item+9999))
			if w.Code != http.StatusNotFound {
				t.Errorf("absent: status = %d, want 404", w.Code)
			}
		})
	}
}

// A server this household never paired with is refused at the pin, before any
// item is looked at — so unpairing stops playback immediately.
func TestAnUnpairedServerMayNotPlay(t *testing.T) {
	f := newFedFixture(t)
	f.share(t, f.h.lib.ID, "")
	item := f.h.addFile(t, "a film.mkv", []byte("not really a film"))

	for _, route := range playRoutes(f.h.srvAPI) {
		t.Run(route.name, func(t *testing.T) {
			w := f.call(route.handler,
				playRequest(t, f.stranger, route.target, route.item, item))
			if w.Code != http.StatusForbidden {
				t.Errorf("stranger: status = %d, want 403", w.Code)
			}
		})
	}
}

/*
 * A ceiling on the share refuses the item, not just hides it from the listing.
 *
 * This is the one that would be quiet if it were wrong. Browsing already
 * applies the ceiling, so a film above it never appears — and a play route
 * that forgot the check would be unreachable through the UI and wide open to
 * anything that guessed an id. The check has to be on the *play* path, which
 * is exactly what MayPlay(Friend) is.
 */
func TestAPeerMayNotPlayAboveTheCeiling(t *testing.T) {
	f := newFedFixture(t)
	f.share(t, f.h.lib.ID, "PG")

	item := f.h.addFile(t, "an 18 rated film.mkv", []byte("not really a film"))
	rated := "R"
	if err := f.h.st.UpdateItemMetadata(context.Background(), item,
		store.ItemMetadata{ContentRating: &rated}); err != nil {
		t.Fatal(err)
	}

	for _, route := range playRoutes(f.h.srvAPI) {
		t.Run(route.name, func(t *testing.T) {
			w := f.call(route.handler,
				playRequest(t, f.georgia, route.target, route.item, item))
			if w.Code != http.StatusNotFound {
				t.Errorf("status = %d for an item above the ceiling, want 404", w.Code)
			}
		})
	}
}

// --- the two mechanisms a status code cannot show --------------------------

/*
 * A friend's encode is not the household's encode.
 *
 * A transcode session is keyed by item *and owner* so that seeking replaces
 * your own stream rather than starting a second one beside it. With no session
 * — which is every federated request, by construction — `userID` answers the
 * local owner, so a friend seeking would have torn down the household's stream
 * of the same film, and the household seeking would have torn down theirs.
 *
 * Neither would have failed. Both would have looked like the film stopping for
 * no reason, on somebody else's machine, once.
 */
func TestAFriendsEncodeIsNotTheHouseholds(t *testing.T) {
	f := newFedFixture(t)

	household := httptest.NewRequest(http.MethodGet, "/api/stream/1/transcode", nil)
	friend := household.Clone(withStreamOwner(household.Context(),
		peerOwner(f.peerFP)))
	other := household.Clone(withStreamOwner(household.Context(),
		peerOwner("SOMEOTHERSERVERFINGERPRINT")))

	mine := f.h.srvAPI.streamOwner(household)
	theirs := f.h.srvAPI.streamOwner(friend)
	third := f.h.srvAPI.streamOwner(other)

	if mine == theirs {
		t.Errorf("a friend shares the household's transcode session (%q)", mine)
	}
	if theirs == third {
		t.Errorf("two different servers share one transcode session (%q)", theirs)
	}
	if mine == "" || theirs == "" {
		t.Error("an owner is empty, so every caller would share one session")
	}
}

/*
 * A peer's playlist points at the peer routes, not at this server's own.
 *
 * The playlist is written here and read by a player on a machine that cannot
 * reach this server at all. Its segment URLs are server-absolute, so left
 * alone they name `/api/stream/{id}/hls/...` on the *friend's* server — where
 * that id is a different item.
 *
 * The failure mode is the reason this is tested rather than reasoned about: it
 * would not 404. The friend's server holds its own items, so the request would
 * very likely succeed and play somebody else's film.
 */
func TestAPeersPlaylistNamesTheFederationRoutes(t *testing.T) {
	local := httptest.NewRequest(http.MethodGet, "/api/stream/7/hls/index.m3u8", nil)
	if got, want := playlistPrefix(local, 7, "abc"), "/api/stream/7/hls/abc/"; got != want {
		t.Errorf("local prefix = %q, want %q", got, want)
	}

	peer := local.Clone(withPlaylistPrefix(local.Context(), "/api/federation/hls/7/"))
	if got, want := playlistPrefix(peer, 7, "abc"), "/api/federation/hls/7/abc/"; got != want {
		t.Errorf("peer prefix = %q, want %q", got, want)
	}
}

/*
 * One shared item must not be a key to every image on the disk.
 *
 * Artwork is content-addressed, so a hash is **not an item**: the local route
 * takes a hash alone, which is right for a household where anyone with a
 * session may see any of its artwork, and wrong for a friend. A peer able to
 * ask for arbitrary hashes could fetch images belonging to libraries nobody
 * shared with them, and the only obstacle would be not knowing the hash.
 *
 * Both halves are needed and this asserts the second: the gate says whether
 * they may see the *item*, and this says whether the hash belongs to it.
 */
func TestAnItemIsNotAKeyToEveryImage(t *testing.T) {
	poster, fanart, thumb := "aaa", "bbb", "ccc"
	mine := &store.Item{
		Artwork: &store.Artwork{Poster: poster, Fanart: fanart, Thumb: thumb},
	}

	for _, hash := range []string{poster, fanart, thumb} {
		if !itemHasArtwork(mine, hash) {
			t.Errorf("%q belongs to this item and was refused", hash)
		}
	}
	for _, hash := range []string{
		"ddd",  // somebody else's image
		"",     // nothing at all
		"aaa ", // not a hash, and not equal to one
		"AAA",  // hashes are not case-folded here
	} {
		if itemHasArtwork(mine, hash) {
			t.Errorf("%q does not belong to this item and was allowed", hash)
		}
	}

	// An item with no artwork grants nothing, rather than granting everything
	// by having nothing to compare against.
	if itemHasArtwork(&store.Item{}, poster) {
		t.Error("an item with no artwork allowed a hash")
	}
	if itemHasArtwork(nil, poster) {
		t.Error("no item at all allowed a hash")
	}
}

/*
 * Every field of Artwork is covered.
 *
 * The check is a list of comparisons, so a field added later is a field it
 * silently does not cover — and the symptom would be a picture that stops
 * loading for a friend, with nothing failing anywhere. This counts the string
 * fields by reflection so that omission fails here instead.
 */
func TestTheArtworkCheckCoversEveryField(t *testing.T) {
	var covered []string
	for _, h := range []string{"p", "f", "t"} {
		a := &store.Artwork{}
		switch h {
		case "p":
			a.Poster = h
		case "f":
			a.Fanart = h
		case "t":
			a.Thumb = h
		}
		if itemHasArtwork(&store.Item{Artwork: a}, h) {
			covered = append(covered, h)
		}
	}
	if len(covered) != 3 {
		t.Fatalf("covered %v, want all three", covered)
	}

	rt := reflect.TypeOf(store.Artwork{})
	var strings int
	for i := range rt.NumField() {
		if rt.Field(i).Type.Kind() == reflect.String {
			strings++
		}
	}
	if strings != 3 {
		t.Errorf("store.Artwork has %d hash fields and itemHasArtwork checks 3; "+
			"a field added here is artwork a friend silently cannot load", strings)
	}
}

/*
 * The gate actually records where a peer connected from (ADR 0044 §5).
 *
 * **This test exists because removing the call compiled and nothing failed.**
 * The decision — which host, which port, what to ignore — is covered next door
 * by `learnedAddress`, and covering only that leaves the wiring untested: a
 * pure function nobody calls passes every assertion about itself.
 *
 * So this goes through a handler, with a request shaped like one arriving on
 * the peer channel, and asks the store what it now believes.
 */
func TestAPeerWhoMovedIsFoundAgain(t *testing.T) {
	f := newFedFixture(t)
	ctx := context.Background()

	before, err := f.h.st.PeerByFingerprint(ctx, f.peerFP)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Addrs) == 0 {
		t.Fatal("fixture: the peer has no address to serve on")
	}
	_, port, err := net.SplitHostPort(before.Addrs[0])
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}

	// A request from an address nobody has recorded, carrying the identity key
	// this server pinned at pairing. The key is what makes the source address
	// evidence rather than a claim.
	r := peerRequest(t, f.georgia, "/api/federation/libraries")
	r.RemoteAddr = "192.0.2.77:51234"
	f.call(f.h.srvAPI.federationLibraries, r)

	after, err := f.h.st.PeerByFingerprint(ctx, f.peerFP)
	if err != nil {
		t.Fatal(err)
	}
	want := net.JoinHostPort("192.0.2.77", port)
	if len(after.Addrs) == 0 || after.Addrs[0] != want {
		t.Errorf("addresses are %v, want %q first — a peer that moved was not "+
			"found again, which is the fault ADR 0044 §5 describes and never fixed",
			after.Addrs, want)
	}
	// The address it was introduced on survives behind the new one: a peer
	// reachable two ways must not lose one because it called from the other.
	if !slices.Contains(after.Addrs, before.Addrs[0]) {
		t.Errorf("addresses are %v; the original %q was lost",
			after.Addrs, before.Addrs[0])
	}
}

// A stranger teaches nothing. The refusal happens before any address is
// recorded, so an unpaired server cannot write into another peer's list.
func TestAStrangerCannotMoveAPeer(t *testing.T) {
	f := newFedFixture(t)
	ctx := context.Background()

	before, _ := f.h.st.PeerByFingerprint(ctx, f.peerFP)

	r := peerRequest(t, f.stranger, "/api/federation/libraries")
	r.RemoteAddr = "192.0.2.99:51234"
	f.call(f.h.srvAPI.federationLibraries, r)

	after, _ := f.h.st.PeerByFingerprint(ctx, f.peerFP)
	if !slices.Equal(after.Addrs, before.Addrs) {
		t.Errorf("addresses changed from %v to %v because a stranger called",
			before.Addrs, after.Addrs)
	}
}
