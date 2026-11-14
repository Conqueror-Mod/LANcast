package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lancast/internal/identity"

	"lancast/internal/transcode"
)

/*
 * A running HLS session serves its owner, under the item it was started for,
 * and nobody else (review findings R03-1, R02-2).
 *
 * Every way in to the segment route decides on the item in the path — GetItem
 * for an account, the guest gate's object check, federationPlay for a paired
 * server — and the handler then served whichever session the path named. So an
 * account allowed one film read any running session through it, and a session
 * id was the only thing in the way. `GET /api/transcode` handed every account
 * the list of them.
 *
 * Sessions are planted with AdoptForTest: what is under test is who may read
 * one, not ffmpeg.
 */

// plantSession registers a session for item, owned by owner, with an init
// segment and a playlist on disk so an allowed request is served at once.
func plantSession(t *testing.T, s *Server, item int64, owner string) *transcode.Session {
	t.Helper()
	sess, err := s.trans.AdoptForTest(item, owner)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"index.m3u8": "#EXTM3U\n#EXT-X-ENDLIST\n",
		"init.mp4":   "init segment",
	} {
		if err := os.WriteFile(filepath.Join(sess.Dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return sess
}

func segmentPath(item int64, sess *transcode.Session) string {
	return "/api/stream/" + itoa64(item) + "/hls/" + sess.ID + "/init.mp4"
}

func TestAnHLSSessionServesOnlyItsOwner(t *testing.T) {
	h := newHarness(t)
	item := h.addFile(t, "film.mkv", []byte("x"))
	h.secure(t, "a good long password")
	member := h.addMember(t, "viewer", "another long password")

	sess := plantSession(t, h.srvAPI, item, h.ownID(t))

	// The owner is served.
	resp := h.authed(t, "GET", segmentPath(item, sess), nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "init segment" {
		t.Fatalf("owner: status = %d body %q, want 200 and the segment", resp.StatusCode, body)
	}

	// A second account allowed the same film is refused the session, with the
	// answer a session that has ended gets.
	resp = h.doAs(t, member, "GET", segmentPath(item, sess), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("another account: status = %d, want 404 — it read a session it does not own",
			resp.StatusCode)
	}
}

// The item in the path is the item the session was started for. Through a
// film the caller may play, a session for another film is refused — even the
// caller's own, because a permission decided on one item must not serve
// another (R02-2).
func TestAnHLSSessionIsServedOnlyUnderItsOwnItem(t *testing.T) {
	h := newHarness(t)
	allowed := h.addFile(t, "allowed.mkv", []byte("x"))
	other := h.addFile(t, "other.mkv", []byte("y"))
	h.secure(t, "a good long password")

	sess := plantSession(t, h.srvAPI, other, h.ownID(t))

	resp := h.authed(t, "GET", segmentPath(allowed, sess), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for another item's session", resp.StatusCode)
	}
}

// A refused reader does not keep somebody else's session alive.
func TestARefusedSegmentDoesNotTouchTheSession(t *testing.T) {
	s, _ := segmentServer(t)
	sess, err := s.trans.AdoptForTest(7459, "u_owner")
	if err != nil {
		t.Fatal(err)
	}
	before := idleOf(t, s, sess.ID)

	rec := httptest.NewRecorder()
	r := segmentRequest(sess.ID + "/init.mp4")
	s.hlsSegment(rec, r) // no session in context: the local owner, not u_owner
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if after := idleOf(t, s, sess.ID); after < before {
		t.Error("a refused request touched the session, keeping it from being reaped")
	}
}

func idleOf(t *testing.T, s *Server, id string) int {
	t.Helper()
	for _, si := range s.trans.Sessions() {
		if si.ID == id {
			return si.IdleSeconds
		}
	}
	t.Fatalf("session %s not running", id)
	return 0
}

/*
 * A paired server owns what it started, under `peer:<fingerprint>`, and is
 * served it through the federation route. A session somebody else started —
 * or one of its own for an item it reaches only by naming a shared one — is
 * refused.
 */
func TestAPeerIsServedOnlyItsOwnSession(t *testing.T) {
	f := newFedFixture(t)
	f.share(t, f.h.lib.ID, "")
	shared := f.h.addFile(t, "shared.mkv", []byte("x"))
	elsewhere := f.h.addFile(t, "elsewhere.mkv", []byte("y"))
	s := f.h.srvAPI

	call := func(item int64, sess *transcode.Session) int {
		r := peerRequest(t, f.georgia,
			"/api/federation/hls/"+itoa64(item)+"/"+sess.ID+"/init.mp4")
		r.SetPathValue("item", itoa64(item))
		r.SetPathValue("session", sess.ID)
		r.SetPathValue("name", "init.mp4")
		return f.call(s.federationPlay(s.hlsSegment), r).Code
	}

	own := plantSession(t, s, shared, peerOwner(f.peerFP))
	if got := call(shared, own); got != http.StatusOK {
		t.Fatalf("the peer's own session: status = %d, want 200", got)
	}

	household := plantSession(t, s, shared, f.h.ownID(t))
	if got := call(shared, household); got != http.StatusNotFound {
		t.Errorf("the household's session: status = %d, want 404", got)
	}

	// The peer's own session, for an item it names a shared one to reach: the
	// un-share that never reached a running stream.
	smuggled := plantSession(t, s, elsewhere, peerOwner(f.peerFP))
	if got := call(shared, smuggled); got != http.StatusNotFound {
		t.Errorf("another item's session through a shared item: status = %d, want 404", got)
	}
}

// A guest is identified as the server that vouched for it, as the playlist
// route names it, and is refused anybody else's session.
func TestAGuestIsServedOnlyItsOwnSession(t *testing.T) {
	f := newRedeemFixture(t)
	token := guestToken(t, f)
	item := f.h.addFile(t, "film.mkv", []byte("x"))
	peer := identity.Normalize(f.georgia.Fingerprint())
	if err := f.h.st.ShareLibrary(t.Context(), peer, f.h.lib.ID, "", time.Now()); err != nil {
		t.Fatal(err)
	}

	own := plantSession(t, f.h.srvAPI, item, peerOwner(peer))
	resp := f.asGuest(t, token, http.MethodGet, segmentPath(item, own))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the guest's own session: status = %d, want 200", resp.StatusCode)
	}

	household := plantSession(t, f.h.srvAPI, item, f.h.ownID(t))
	resp = f.asGuest(t, token, http.MethodGet, segmentPath(item, household))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("the household's session: status = %d, want 404", resp.StatusCode)
	}
}

// A channel's session is shared by everyone watching it, so only the channel
// is checked — but a channel route will not serve a film's session.
func TestAChannelSegmentIsServedOnlyFromThatChannel(t *testing.T) {
	s, _ := segmentServer(t)
	channel, err := s.trans.AdoptForTest(-12, "")
	if err != nil {
		t.Fatal(err)
	}
	film := plantSession(t, s, 12, "u_owner")
	if err := os.WriteFile(filepath.Join(channel.Dir, "init.mp4"), []byte("live"), 0o644); err != nil {
		t.Fatal(err)
	}

	get := func(sess *transcode.Session) int {
		r := httptest.NewRequest("GET", "/api/channels/12/hls/"+sess.ID+"/init.mp4", nil)
		r.SetPathValue("id", "12")
		r.SetPathValue("session", sess.ID)
		r.SetPathValue("name", "init.mp4")
		rec := httptest.NewRecorder()
		s.channelHLSSegment(rec, r)
		return rec.Code
	}
	if got := get(channel); got != http.StatusOK {
		t.Errorf("the channel's own session: status = %d, want 200", got)
	}
	if got := get(film); got != http.StatusNotFound {
		t.Errorf("a film's session through a channel route: status = %d, want 404", got)
	}
}

/*
 * `GET /api/transcode` lists every session for an administrator and only the
 * caller's own for anybody else. The whole list is who is watching what, with
 * the session ids that used to be all a member needed (R03-1).
 */
func TestTheTranscodeListShowsAMemberOnlyTheirOwn(t *testing.T) {
	h := newHarness(t)
	item := h.addFile(t, "film.mkv", []byte("x"))
	h.secure(t, "a good long password")
	member := h.addMember(t, "viewer", "another long password")
	memberID := userIDOf(t, h, "viewer")

	adminSess := plantSession(t, h.srvAPI, item, h.ownID(t))
	memberSess := plantSession(t, h.srvAPI, item, memberID)

	list := func(resp *http.Response) []string {
		t.Helper()
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		var body struct {
			Available *bool `json:"available"`
			Sessions  []struct {
				ID string `json:"id"`
			} `json:"sessions"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Available == nil {
			t.Error("available is missing")
		}
		var ids []string
		for _, s := range body.Sessions {
			ids = append(ids, s.ID)
		}
		return ids
	}

	if got := list(h.doAs(t, member, "GET", "/api/transcode", nil)); len(got) != 1 || got[0] != memberSess.ID {
		t.Errorf("a member sees %v, want only their own %s", got, memberSess.ID)
	}
	if got := list(h.authed(t, "GET", "/api/transcode", nil)); len(got) != 2 {
		t.Errorf("an administrator sees %v, want both %s and %s", got, adminSess.ID, memberSess.ID)
	}
}

func userIDOf(t *testing.T, h *harness, name string) string {
	t.Helper()
	u, err := h.st.UserByName(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}
