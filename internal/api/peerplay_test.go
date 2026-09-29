package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lancast/internal/identity"
	"lancast/internal/store"
)

/*
 * Playing somebody else's film, from this side (ADR 0071 §5).
 *
 * The far server is not here, so what is under test is what this side decides
 * *before* asking and what it does with what comes back. That is the whole of
 * this server's job on the play path — it is a pipe, and the interesting
 * question about a pipe is whether it changes what goes through it.
 *
 * **Nothing here proves two real servers play a film.** The peer channel is
 * mutual TLS with a pinned key; these exercise handlers.
 */

// peerPlayRoutes is every route this server offers for playing a peer's item.
// A table so a route added later without a test is missing from a list
// somebody reads rather than from a file nobody opens.
func peerPlayRoutes(s *Server, fp string) []struct {
	name    string
	target  string
	path    map[string]string
	handler http.HandlerFunc
} {
	return []struct {
		name    string
		target  string
		path    map[string]string
		handler http.HandlerFunc
	}{
		{"decision", "/api/peers/" + fp + "/playback?item=7", nil, s.peerPlayback},
		{"file", "/api/peers/" + fp + "/stream?item=7", nil, s.peerStream},
		{"transcode", "/api/peers/" + fp + "/transcode?item=7", nil, s.peerTranscode},
		{"playlist", "/api/peers/" + fp + "/hls/7/index.m3u8",
			map[string]string{"item": "7"}, s.peerHLSPlaylist},
		{"segment", "/api/peers/" + fp + "/hls/7/sess/1.m4s",
			map[string]string{"item": "7", "session": "sess", "name": "1.m4s"}, s.peerHLSSegment},
		{"subtitles", "/api/peers/" + fp + "/subtitles?item=7", nil, s.peerSubtitles},
		{"track", "/api/peers/" + fp + "/subtitles/7/en",
			map[string]string{"item": "7", "key": "en"}, s.peerSubtitleFile},
	}
}

/*
 * A peer this server was never introduced to is refused here, before anything
 * is asked of anybody.
 *
 * That is the one decision this side owns on the play path. Everything about
 * whether the film may be watched belongs to the far server, and a check here
 * would be this household deciding what the other one meant to share.
 */
func TestPlayingFromAnUnknownPeerIsRefusedLocally(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	stranger := anotherServer(t)

	for _, route := range peerPlayRoutes(h.srvAPI, stranger.Fingerprint()) {
		t.Run(route.name, func(t *testing.T) {
			resp := h.authed(t, "GET", route.target, nil)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("status = %d, want 404", resp.StatusCode)
			}
		})
	}
}

// Signed out, there is nobody asking.
func TestPlayingFromAPeerNeedsASession(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")

	for _, route := range peerPlayRoutes(h.srvAPI, georgia.Fingerprint()) {
		t.Run(route.name, func(t *testing.T) {
			resp := h.do(t, "GET", route.target, nil)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", resp.StatusCode)
			}
		})
	}
}

/*
 * A paired peer that is not answering is 502, not 500 — and reaching 502 is
 * itself the assertion.
 *
 * It proves the request got past everything this side decides and was actually
 * attempted. A 403 or a 404 here would mean this server had formed its own
 * view about somebody else's film, which is the thing it must not do.
 */
func TestAPeerThatIsNotAnsweringIsAGatewayError(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")

	for _, route := range peerPlayRoutes(h.srvAPI, georgia.Fingerprint()) {
		t.Run(route.name, func(t *testing.T) {
			resp := h.authed(t, "GET", route.target, nil)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadGateway {
				t.Fatalf("status = %d, want 502", resp.StatusCode)
			}
			var e struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			decode(t, resp, &e)
			if e.Error.Code != "peer_unreachable" {
				t.Errorf("code = %q, want peer_unreachable", e.Error.Code)
			}
		})
	}
}

// --- the rewrite, which is the whole feature in one function ---------------

/*
 * A peer's playlist is repointed at this server's proxy.
 *
 * This is the piece whose failure does not look like a failure. Left alone,
 * the player would ask *this* server for `/api/stream/{id}/hls/…` — and this
 * server holds its own item at that id, so the request would most likely
 * succeed and play a different film. There is no status code for that.
 *
 * Asserted line by line rather than on a substring, because the way to get
 * this subtly wrong is to rewrite some lines and not others: the init segment
 * is named inside an `#EXT-X-MAP:URI=` attribute rather than on a line of its
 * own, and a rewrite that only looked at bare lines would leave it pointing
 * at this server while every other segment moved.
 */
func TestAPeersPlaylistIsRepointedAtThisServer(t *testing.T) {
	const fp = "AAAABBBBCCCCDDDD"
	in := strings.Join([]string{
		"#EXTM3U",
		"#EXT-X-VERSION:7",
		`#EXT-X-MAP:URI="/api/federation/hls/42/sess/init.mp4"`,
		"#EXTINF:6.000,",
		"/api/federation/hls/42/sess/1.m4s",
		"#EXTINF:6.000,",
		"/api/federation/hls/42/sess/2.m4s",
		"#EXT-X-ENDLIST",
		"",
	}, "\n")

	got := repointPlaylistAtPeer(in, fp)

	if strings.Contains(got, "/api/federation/") {
		t.Error("a federation path survived: the player would ask this server for it")
	}
	for _, want := range []string{
		`#EXT-X-MAP:URI="/api/peers/` + fp + `/hls/42/sess/init.mp4"`,
		"/api/peers/" + fp + "/hls/42/sess/1.m4s",
		"/api/peers/" + fp + "/hls/42/sess/2.m4s",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}

	// Everything that is not a URL is left exactly alone. This server has no
	// business holding an opinion about durations or the endlist marker.
	for _, keep := range []string{"#EXTM3U", "#EXT-X-VERSION:7", "#EXTINF:6.000,", "#EXT-X-ENDLIST"} {
		if !strings.Contains(got, keep) {
			t.Errorf("the rewrite lost %q", keep)
		}
	}
	if a, b := strings.Count(in, "\n"), strings.Count(got, "\n"); a != b {
		t.Errorf("line count changed: %d -> %d", a, b)
	}
}

/*
 * Everything except `item` is forwarded.
 *
 * `t`, `audio` and the quality ceiling all participate in the far server's
 * delivery decision, exactly as they do locally — so one dropped here produces
 * an answer about a different stream than the one asked for, which arrives as
 * a 409 on a film the parameter was the only reason to touch. A parameter this
 * side has never heard of goes through for the same reason browsing forwards
 * its query whole.
 */
func TestPlaybackParametersSurviveTheHop(t *testing.T) {
	for _, c := range []struct{ raw, want string }{
		{"item=7", ""},
		{"item=7&t=120", "&t=120"},
		{"item=7&audio=3&quality=720p", "&audio=3&quality=720p"},
		{"item=7&somethingNew=1", "&somethingNew=1"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/api/peers/x/transcode?"+c.raw, nil)
		if got := forwardedQuery(r); got != c.want {
			t.Errorf("forwardedQuery(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

/*
 * A refusal from the far server says what it was.
 *
 * All of these were one sentence — "that server did not share this with you" —
 * which was true of every status the browse routes could produce and is a lie
 * about two the play routes can. Telling somebody they lack permission while a
 * film is merely warming up sends them to ask for something they already have.
 */
func TestAPeersRefusalSaysWhichRefusalItWas(t *testing.T) {
	for _, c := range []struct {
		status int
		want   string
	}{
		{http.StatusNotFound, "did not share"},
		{http.StatusConflict, "played as it is"},
		{http.StatusServiceUnavailable, "could not start converting"},
		{http.StatusForbidden, "refused this pairing"},
	} {
		if got := peerRefusalText(c.status); !strings.Contains(got, c.want) {
			t.Errorf("peerRefusalText(%d) = %q, want it to mention %q", c.status, got, c.want)
		}
	}

	// And none of them is the same sentence as another, which is the point.
	seen := map[string]int{}
	for _, status := range []int{
		http.StatusNotFound, http.StatusConflict,
		http.StatusServiceUnavailable, http.StatusForbidden,
	} {
		seen[peerRefusalText(status)]++
	}
	if len(seen) != 4 {
		t.Errorf("%d distinct sentences for 4 refusals", len(seen))
	}
}

// A bad item in the path is refused here rather than sent onward. It is about
// to become a path segment in a request to somebody else's server.
func TestARubbishItemIsNotForwarded(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")
	fp := georgia.Fingerprint()

	for _, target := range []string{
		"/api/peers/" + fp + "/hls/nonsense/index.m3u8",
		"/api/peers/" + fp + "/hls/-1/index.m3u8",
		"/api/peers/" + fp + "/subtitles/nonsense/en",
	} {
		t.Run(target, func(t *testing.T) {
			resp := h.authed(t, "GET", target, nil)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

// --- presence for a peer's film (ADR 0045 §10) -----------------------------

/*
 * The beat needs a peer this server knows and a caller who is signed in, and
 * nothing else it could be talked into.
 */
func TestTheWatchingBeatIsRefusedLikeEveryOtherPeerRoute(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")
	stranger := anotherServer(t)

	for _, c := range []struct {
		name   string
		target string
		signed bool
		want   int
	}{
		{"a peer we never met", "/api/peers/" + stranger.Fingerprint() + "/watching?item=1", true, http.StatusNotFound},
		{"no item named", "/api/peers/" + georgia.Fingerprint() + "/watching", true, http.StatusBadRequest},
		{"signed out", "/api/peers/" + georgia.Fingerprint() + "/watching?item=1", false, http.StatusUnauthorized},
	} {
		t.Run(c.name, func(t *testing.T) {
			var resp *http.Response
			if c.signed {
				resp = h.authed(t, "PUT", c.target, nil)
			} else {
				resp = h.do(t, "PUT", c.target, nil)
			}
			defer resp.Body.Close()
			if resp.StatusCode != c.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, c.want)
			}
		})
	}
}

/*
 * A peer that will not answer discloses nothing, and does not leave the last
 * thing standing either.
 *
 * Presence lingering after the truth changed is the single false statement
 * about the present that ADR 0045 exists not to make — so a beat that could
 * not be resolved is `Stopped`, not "keep whatever was there".
 */
func TestAnUnreachablePeerLeavesNoPresenceBehind(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")

	// Something is being watched locally first, so the assertion is that the
	// beat *cleared* it rather than that it was never set.
	h.srvAPI.tracker().Watching(store.LocalUserID, "A Local Film")
	if st, _ := h.srvAPI.tracker().Snapshot(store.LocalUserID); st.Watching == "" {
		t.Fatal("fixture: nothing was being watched to begin with")
	}

	resp := h.authed(t, "PUT",
		"/api/peers/"+georgia.Fingerprint()+"/watching?item=1", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	st, _ := h.srvAPI.tracker().Snapshot(store.LocalUserID)
	if st.Watching != "" {
		t.Errorf("still watching %q after the peer failed to answer", st.Watching)
	}
}

/*
 * **No title crosses from the client.**
 *
 * This is the rule §10 turns on. §3's reductions — video only, the work and
 * never the episode — are `presenceTitle` on the server that owns the item, and
 * a client able to name its own presence could name an episode, which §3
 * forbids by name.
 *
 * Asserted on the path this server builds rather than on the presence that
 * results. The first version of this test set a title in the query and checked
 * that presence stayed empty — which it did, because the peer was unreachable
 * in the harness and presence would have been cleared either way. It passed
 * while proving nothing, which is the failure this file's own comments warn
 * about twice.
 *
 * What is actually load-bearing is that the request carries the item and
 * nothing else, so a title parameter added later fails here rather than being
 * found in somebody's People screen.
 */
func TestTheClientCannotNameWhatItIsWatching(t *testing.T) {
	for _, c := range []struct{ item, want string }{
		{"42", "/api/federation/item/42"},
		// A client trying to smuggle one through the only field it controls.
		{"42?title=Cowboy+Bebop+S01E02", "/api/federation/item/42%3Ftitle=Cowboy+Bebop+S01E02"},
	} {
		got := peerItemPath(c.item)
		if got != c.want {
			t.Errorf("peerItemPath(%q) = %q, want %q", c.item, got, c.want)
		}
		if strings.Contains(got, "Bebop") && !strings.Contains(got, "%3F") {
			t.Errorf("a client-supplied value reached the peer unescaped: %q", got)
		}
	}

	// And the handler reads no title of its own: the only thing it takes from
	// the caller is which item.
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")
	h.srvAPI.tracker().Watching(store.LocalUserID, "A Local Film")

	resp := h.authed(t, "PUT",
		"/api/peers/"+georgia.Fingerprint()+
			"/watching?item=1&title=Cowboy%20Bebop%20S01E02", nil)
	defer resp.Body.Close()

	st, _ := h.srvAPI.tracker().Snapshot(store.LocalUserID)
	if st.Watching == "Cowboy Bebop S01E02" {
		t.Error("the client named its own disclosure")
	}
}

/*
 * Where we are in one of their films (ADR 0071 §4).
 *
 * §4 is the one place ADR 0071 departs from ADR 0046's "a guest writes
 * nothing", and the decision is about *where the row lives*: here, on the
 * watcher's server, because a row on the host keyed to a remote principal is an
 * account by another name.
 *
 * So what these assert is the boundary — it is written locally, it comes back
 * on the answer the player already fetches, and nothing about it is sent to the
 * server that holds the film.
 */
func TestWhereWeGotToIsKeptHereAndReadBack(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")
	fp := georgia.Fingerprint()

	resp := h.authed(t, "PUT", "/api/peers/"+fp+"/progress/42",
		map[string]any{"position_ms": 600_000})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	// Read back from the store rather than the item route: the item route has
	// to reach the peer, which is unreachable in a harness, and what is under
	// test is that this server kept it.
	got, err := h.st.PeerProgressFor(context.Background(),
		identity.Normalize(fp), 42)
	if err != nil || got != 600_000 {
		t.Errorf("stored %d, %v; want 600000", got, err)
	}
}

// The same refusals as every other peer route, and one of its own.
func TestProgressIsRefusedLikeEveryOtherPeerRoute(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")
	stranger := anotherServer(t)

	for _, c := range []struct {
		name   string
		target string
		body   any
		signed bool
		want   int
	}{
		{"a peer we never met", "/api/peers/" + stranger.Fingerprint() + "/progress/1",
			map[string]any{"position_ms": 1}, true, http.StatusNotFound},
		{"not an item", "/api/peers/" + georgia.Fingerprint() + "/progress/nonsense",
			map[string]any{"position_ms": 1}, true, http.StatusBadRequest},
		{"signed out", "/api/peers/" + georgia.Fingerprint() + "/progress/1",
			map[string]any{"position_ms": 1}, false, http.StatusUnauthorized},
	} {
		t.Run(c.name, func(t *testing.T) {
			var resp *http.Response
			if c.signed {
				resp = h.authed(t, "PUT", c.target, c.body)
			} else {
				resp = h.do(t, "PUT", c.target, c.body)
			}
			defer resp.Body.Close()
			if resp.StatusCode != c.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, c.want)
			}
		})
	}
}

/*
 * Unpairing forgets it, which is what makes §4's argument hold.
 *
 * The reason the host is the wrong place for this is that a row there would
 * outlive the relationship. That reasoning only works if the row *here* does
 * not — so this asserts the cascade at the surface a person actually touches.
 */
func TestUnpairingForgetsWhereWeWereInTheirFilms(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")
	fp := georgia.Fingerprint()

	h.authed(t, "PUT", "/api/peers/"+fp+"/progress/42",
		map[string]any{"position_ms": 600_000}).Body.Close()
	h.authed(t, "DELETE", "/api/peers/"+fp, nil).Body.Close()

	got, err := h.st.PeerProgressFor(context.Background(), identity.Normalize(fp), 42)
	if err != nil || got != 0 {
		t.Errorf("after unpairing got %d, %v; want it forgotten", got, err)
	}
}
