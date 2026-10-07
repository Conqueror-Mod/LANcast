package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"lancast/internal/identity"
	"lancast/internal/store"
	"lancast/internal/together"
)

/*
 * A room crossing to another server, from the host's side (Phase 5, step 3).
 *
 * Most of what is tested here is refusal. The flow working is one test; each
 * way it must not work is another, because the host's answer, the presence
 * grant and the room are three separate consents and any one of them going
 * away has to close the door.
 */

const georgiaPerson = "u_georgia"

type roomFixture struct {
	fedFixture
	host string
	item int64
}

// newRoomFixture: Chris's server, paired with Georgia's, Georgia on its
// roster, and a film on Chris's disk that is not shared with her.
func newRoomFixture(t *testing.T) roomFixture {
	t.Helper()
	f := newFedFixture(t)
	ctx := context.Background()
	if err := f.h.st.ReplaceRemotePeople(ctx, f.peerFP, []store.RemotePerson{
		{ID: georgiaPerson, Name: "Georgia"},
	}); err != nil {
		t.Fatal(err)
	}
	u, err := f.h.st.UserByName(ctx, testUser)
	if err != nil {
		t.Fatal(err)
	}
	item := f.h.addFile(t, "blade runner.mkv", []byte("not really a film"))
	return roomFixture{fedFixture: f, host: u.ID, item: item}
}

func (f roomFixture) grant(t *testing.T) {
	t.Helper()
	if err := f.h.st.GrantPresence(context.Background(), f.host, f.peerFP, georgiaPerson, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// watching puts the host in front of their own film, as playback would.
func (f roomFixture) watching(t *testing.T) {
	t.Helper()
	f.h.srvAPI.presence.WatchingHere(f.host, "Blade Runner")
}

// peerCall is a federation request from a server, by its key, as a person.
func peerCall(t *testing.T, id identity.Identity, method, target string, body any) *http.Request {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, target, rd)
	r.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{PublicKey: id.Public()}},
	}
	return r
}

type askAnswer struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	RoomID string `json:"room_id"`
}

func (f roomFixture) ask(t *testing.T, id identity.Identity) (*httptest.ResponseRecorder, askAnswer) {
	t.Helper()
	w := f.call(f.h.srvAPI.federationAskTogether, peerCall(t, id, http.MethodPost,
		"/api/federation/together/requests?person="+georgiaPerson, map[string]any{"host": f.host}))
	var a askAnswer
	_ = json.Unmarshal(w.Body.Bytes(), &a)
	return w, a
}

func (f roomFixture) status(t *testing.T, reqID string) askAnswer {
	t.Helper()
	r := peerCall(t, f.georgia, http.MethodGet,
		"/api/federation/together/requests/"+reqID+"?person="+georgiaPerson, nil)
	r.SetPathValue("id", reqID)
	w := f.call(f.h.srvAPI.federationTogetherRequest, r)
	var a askAnswer
	_ = json.Unmarshal(w.Body.Bytes(), &a)
	return a
}

func (f roomFixture) roomCall(t *testing.T, h http.HandlerFunc, method, suffix, room string) *httptest.ResponseRecorder {
	t.Helper()
	r := peerCall(t, f.georgia, method,
		"/api/federation/together/"+room+suffix+"?person="+georgiaPerson, nil)
	r.SetPathValue("room", room)
	return f.call(h, r)
}

// playAs asks the gated item route for a film, as Georgia's server, with or
// without naming her. It stands for every playback route: they share the gate.
func (f roomFixture) playAs(t *testing.T, item int64, person string) int {
	t.Helper()
	target := "/api/federation/item/" + itoa64(item)
	if person != "" {
		target += "?person=" + person
	}
	r := peerCall(t, f.georgia, http.MethodGet, target, nil)
	r.SetPathValue("item", itoa64(item))
	return f.call(f.h.srvAPI.federationPlay(f.h.srvAPI.federationItem), r).Code
}

// openRoom is the host's client opening a room around the film, and
// acceptRequest is it answering yes into that room.
func (f roomFixture) openRoom(t *testing.T) string {
	t.Helper()
	resp := f.h.authed(t, "POST", "/api/together", map[string]any{"item_id": f.item})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("open room: %d", resp.StatusCode)
	}
	var s together.Session
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatal(err)
	}
	return s.ID
}

func (f roomFixture) answer(t *testing.T, reqID, verb string, body any) int {
	t.Helper()
	resp := f.h.authed(t, "POST", "/api/together/requests/"+reqID+"/"+verb, body)
	resp.Body.Close()
	return resp.StatusCode
}

func (f roomFixture) pending(t *testing.T) []together.Request {
	t.Helper()
	resp := f.h.authed(t, "GET", "/api/together/requests", nil)
	defer resp.Body.Close()
	var out struct {
		Requests []together.Request `json:"requests"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Requests
}

// admitted runs the whole yes: ask, open a room, accept. Returns the room.
func (f roomFixture) admitted(t *testing.T) string {
	t.Helper()
	f.grant(t)
	f.watching(t)
	_, a := f.ask(t, f.georgia)
	room := f.openRoom(t)
	if code := f.answer(t, a.ID, "accept", map[string]any{"room_id": room}); code != http.StatusOK {
		t.Fatalf("accept: %d", code)
	}
	return room
}

// The test the phase is named for, end to end on the host.
func TestARemotePersonAsksIsAcceptedAndFollows(t *testing.T) {
	f := newRoomFixture(t)
	f.grant(t)
	f.watching(t)

	w, a := f.ask(t, f.georgia)
	if w.Code != http.StatusOK || a.State != together.RequestPending || a.ID == "" {
		t.Fatalf("ask: %d %+v, want 200 pending", w.Code, a)
	}

	p := f.pending(t)
	if len(p) != 1 || p[0].Name != "Georgia" || p[0].Server != "Utopia" {
		t.Fatalf("host sees %+v, want one request from Georgia on Utopia", p)
	}

	room := f.openRoom(t)
	if code := f.answer(t, a.ID, "accept", map[string]any{"room_id": room}); code != http.StatusOK {
		t.Fatalf("accept: %d", code)
	}
	if got := f.status(t, a.ID); got.State != together.RequestAccepted || got.RoomID != room {
		t.Fatalf("asker reads %+v, want accepted into %s", got, room)
	}

	w = f.roomCall(t, f.h.srvAPI.federationJoinTogether, http.MethodPost, "/join", room)
	if w.Code != http.StatusOK {
		t.Fatalf("join: %d %s", w.Code, w.Body)
	}
	w = f.roomCall(t, f.h.srvAPI.federationPollTogether, http.MethodGet, "", room)
	if w.Code != http.StatusOK {
		t.Fatalf("poll: %d %s", w.Code, w.Body)
	}
	var s together.Session
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if s.ItemID != f.item || len(s.Members) != 2 {
		t.Errorf("poll = item %d with %d members, want %d with 2", s.ItemID, len(s.Members), f.item)
	}

	// The film is not shared. Being in the room is what lets her play it.
	if code := f.playAs(t, f.item, georgiaPerson); code != http.StatusOK {
		t.Errorf("a member playing the room's film: %d, want 200", code)
	}
}

// A person who has not been granted presence cannot ask, and cannot tell that
// from a person who does not exist.
func TestAskingNeedsAPresenceGrant(t *testing.T) {
	f := newRoomFixture(t)
	f.watching(t)

	w, _ := f.ask(t, f.georgia)
	if w.Code != http.StatusNotFound {
		t.Errorf("ask without a grant: %d, want 404", w.Code)
	}
	if len(f.pending(t)) != 0 {
		t.Error("the host was prompted by somebody with no grant")
	}
}

// A host who is not watching their own film reads as "not now", whether they
// are idle or watching a friend's film, and is never prompted.
func TestAHostNotWatchingTheirOwnFilmIsNotNow(t *testing.T) {
	f := newRoomFixture(t)
	f.grant(t)

	for _, tc := range []struct {
		name  string
		setup func()
	}{
		{"idle", func() { f.h.srvAPI.presence.Seen(f.host) }},
		{"a friend's film", func() { f.h.srvAPI.presence.Watching(f.host, "A Friend's Film") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup()
			w, a := f.ask(t, f.georgia)
			if w.Code != http.StatusOK || a.State != "not_now" {
				t.Errorf("ask: %d %+v, want 200 not_now", w.Code, a)
			}
			if len(f.pending(t)) != 0 {
				t.Error("the host was prompted")
			}
		})
	}
}

func TestOnlyAPairedServerMayAsk(t *testing.T) {
	f := newRoomFixture(t)
	f.grant(t)
	f.watching(t)

	if w, _ := f.ask(t, f.stranger); w.Code != http.StatusForbidden {
		t.Errorf("a stranger's server asking: %d, want 403", w.Code)
	}
	r := peerCall(t, f.georgia, http.MethodPost, "/api/federation/together/requests",
		map[string]any{"host": f.host})
	if w := f.call(f.h.srvAPI.federationAskTogether, r); w.Code != http.StatusBadRequest {
		t.Errorf("asking without naming a person: %d, want 400", w.Code)
	}
}

// Paired, granted, but not admitted: the room is not theirs to join, and the
// film is not theirs to play.
func TestAPeerCannotWalkIntoARoom(t *testing.T) {
	f := newRoomFixture(t)
	f.grant(t)
	room := f.openRoom(t)

	if w := f.roomCall(t, f.h.srvAPI.federationJoinTogether, http.MethodPost, "/join", room); w.Code != http.StatusNotFound {
		t.Errorf("join without being accepted: %d, want 404", w.Code)
	}
	if w := f.roomCall(t, f.h.srvAPI.federationPollTogether, http.MethodGet, "", room); w.Code != http.StatusNotFound {
		t.Errorf("poll without being accepted: %d, want 404", w.Code)
	}
	if code := f.playAs(t, f.item, georgiaPerson); code != http.StatusNotFound {
		t.Errorf("play without being in the room: %d, want 404", code)
	}
}

// The room admits one film, for the person in it, and nothing else.
func TestRoomAdmissionIsTheRoomsFilmAndThePersonOnly(t *testing.T) {
	f := newRoomFixture(t)
	f.admitted(t)
	other := f.h.addFile(t, "another film.mkv", []byte("also not a film"))

	if code := f.playAs(t, other, georgiaPerson); code != http.StatusNotFound {
		t.Errorf("a film the room is not playing: %d, want 404", code)
	}
	if code := f.playAs(t, f.item, ""); code != http.StatusNotFound {
		t.Errorf("the room's film with no person named: %d, want 404 (the share path, unchanged)", code)
	}
	if code := f.playAs(t, f.item, "u_somebody_else"); code != http.StatusNotFound {
		t.Errorf("the room's film as another person on that server: %d, want 404", code)
	}
}

// Revoking presence mid-film takes the guest out of the room on their next
// poll, and the film with them.
func TestRevokingPresenceEndsTheRoomForTheGuest(t *testing.T) {
	f := newRoomFixture(t)
	room := f.admitted(t)

	if err := f.h.st.RevokePresence(context.Background(), f.host, f.peerFP, georgiaPerson); err != nil {
		t.Fatal(err)
	}
	if w := f.roomCall(t, f.h.srvAPI.federationPollTogether, http.MethodGet, "", room); w.Code != http.StatusNotFound {
		t.Errorf("poll after presence was revoked: %d, want 404", w.Code)
	}
	if code := f.playAs(t, f.item, georgiaPerson); code != http.StatusNotFound {
		t.Errorf("play after presence was revoked: %d, want 404", code)
	}
}

func TestLeavingEndsTheGuestsAccess(t *testing.T) {
	f := newRoomFixture(t)
	room := f.admitted(t)

	if w := f.roomCall(t, f.h.srvAPI.federationLeaveTogether, http.MethodDelete, "/members/me", room); w.Code != http.StatusNoContent {
		t.Fatalf("leave: %d", w.Code)
	}
	if code := f.playAs(t, f.item, georgiaPerson); code != http.StatusNotFound {
		t.Errorf("play after leaving: %d, want 404", code)
	}
	if w := f.roomCall(t, f.h.srvAPI.federationJoinTogether, http.MethodPost, "/join", room); w.Code != http.StatusNotFound {
		t.Errorf("rejoining after leaving: %d, want 404 (a new request is needed)", w.Code)
	}
}

// Unpairing revokes everything, with nothing to clean up per room.
func TestUnpairingClosesTheRoomToThatServer(t *testing.T) {
	f := newRoomFixture(t)
	room := f.admitted(t)

	if err := f.h.st.RemovePeer(context.Background(), f.peerFP); err != nil {
		t.Fatal(err)
	}
	if w := f.roomCall(t, f.h.srvAPI.federationPollTogether, http.MethodGet, "", room); w.Code != http.StatusForbidden {
		t.Errorf("poll from an unpaired server: %d, want 403", w.Code)
	}
	if code := f.playAs(t, f.item, georgiaPerson); code != http.StatusForbidden {
		t.Errorf("play from an unpaired server: %d, want 403", code)
	}
}

// A decline is "not now", and so is asking again straight after.
func TestADeclineIsNotNowAndStaysThatWay(t *testing.T) {
	f := newRoomFixture(t)
	f.grant(t)
	f.watching(t)
	_, a := f.ask(t, f.georgia)

	if code := f.answer(t, a.ID, "decline", nil); code != http.StatusNoContent {
		t.Fatalf("decline: %d", code)
	}
	if got := f.status(t, a.ID); got.State != "not_now" {
		t.Errorf("asker reads %q after a decline, want not_now", got.State)
	}
	if _, again := f.ask(t, f.georgia); again.State != "not_now" {
		t.Errorf("asking again straight away reads %q, want not_now", again.State)
	}
	if len(f.pending(t)) != 0 {
		t.Error("the host was prompted again inside the cooldown")
	}
	if code := f.answer(t, a.ID, "decline", nil); code != http.StatusConflict {
		t.Errorf("answering twice: %d, want 409", code)
	}
}

// The host's own client cannot accept into a room that does not exist.
func TestAcceptNeedsARealRoom(t *testing.T) {
	f := newRoomFixture(t)
	f.grant(t)
	f.watching(t)
	_, a := f.ask(t, f.georgia)

	if code := f.answer(t, a.ID, "accept", map[string]any{"room_id": "nosuchroom"}); code != http.StatusNotFound {
		t.Errorf("accept into a missing room: %d, want 404", code)
	}
	if code := f.answer(t, a.ID, "accept", map[string]any{}); code != http.StatusBadRequest {
		t.Errorf("accept naming no room: %d, want 400", code)
	}
	if got := f.status(t, a.ID); got.State != together.RequestPending {
		t.Errorf("a failed accept left the request %q, want still pending", got.State)
	}
}

// Every federation route this adds is reachable by a peer at all: it is on
// the list the session gate exempts. Handler tests cannot see that gate.
func TestTogetherFederationRoutesAreServerAuthenticated(t *testing.T) {
	for _, p := range []string{
		"/api/federation/together/requests",
		"/api/federation/together/requests/abc",
		"/api/federation/together/abc",
		"/api/federation/together/abc/join",
		"/api/federation/together/abc/members/me",
	} {
		if !isFederationPath(p) {
			t.Errorf("%s is not peer-authenticated: a peer would be asked to sign in", p)
		}
	}
}

/*
 * The file route answers the same as every other playback route.
 *
 * It used to ask its own question, and step 3 taught the room to one and not
 * the other. A desktop client plays most films as the file, so a guest would
 * have been refused exactly in the ordinary case, while every test of the
 * gated routes passed.
 */
func TestTheFileRouteAdmitsTheRoomToo(t *testing.T) {
	f := newRoomFixture(t)
	file := func(person string) int {
		target := "/api/federation/stream?item=" + itoa64(f.item)
		if person != "" {
			target += "&person=" + person
		}
		return f.call(f.h.srvAPI.federationStream, peerCall(t, f.georgia, http.MethodGet, target, nil)).Code
	}

	if code := file(georgiaPerson); code != http.StatusNotFound {
		t.Fatalf("the file before being admitted: %d, want 404", code)
	}
	f.admitted(t)
	if code := file(georgiaPerson); code != http.StatusOK {
		t.Errorf("the room's file for a member: %d, want 200", code)
	}
	if code := file(""); code != http.StatusNotFound {
		t.Errorf("the room's file with no person named: %d, want 404", code)
	}
}
