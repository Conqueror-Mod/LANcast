package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lancast/internal/identity"
	"lancast/internal/peer"
	"lancast/internal/store"
	"lancast/internal/together"
)

/*
 * ADR 0103: a friend whose server the host cannot reach still pairs, still
 * appears in the host's roster, and still joins the host's film.
 *
 * Two real servers, as in togetherrelay_test.go, with the difference that is
 * the point: nothing about the friend is filled in on the host by hand. The
 * host holds the friend only as `added`, at an address nothing listens on,
 * and the friend's server has no peer listener at all. Everything the host
 * learns about the friend has to arrive by the friend calling in.
 */

type unreachableFriend struct {
	host, friend *harness
	hostFP       string
	friendFP     string
	hostUser     string
	friendUser   string
	item         int64
}

func newUnreachableFriend(t *testing.T) unreachableFriend {
	t.Helper()
	ctx := context.Background()
	host := newHarness(t)
	host.secure(t, "a good long password")
	friend := newHarness(t)
	friend.secure(t, "a good long password")

	// The host answers peers on its own listener. The friend has none.
	cfg, err := peer.ServerConfig(host.srvAPI.ident)
	if err != nil {
		t.Fatal(err)
	}
	peerSrv := httptest.NewUnstartedServer(host.srvAPI.Handler())
	peerSrv.TLS = cfg
	peerSrv.StartTLS()
	t.Cleanup(peerSrv.Close)

	// The friend pastes the host's invite: `added` on the friend's side.
	inv, err := peer.Encode(host.srvAPI.ident, "Dana's", []string{peerSrv.Listener.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	friend.authed(t, "POST", "/api/peers", map[string]any{"invite": inv}).Body.Close()
	// The host pastes the friend's: `added`, at 127.0.0.1:1 (inviteFrom), and
	// nothing more. No roster, no grant, no promotion.
	host.authed(t, "POST", "/api/peers", map[string]any{
		"invite": inviteFrom(t, friend.srvAPI.ident, "Utopia"),
	}).Body.Close()

	hu, err := host.st.UserByName(ctx, testUser)
	if err != nil {
		t.Fatal(err)
	}
	fu, err := friend.st.UserByName(ctx, testUser)
	if err != nil {
		t.Fatal(err)
	}
	// The friend has chosen to be listed, as anybody joining has to.
	if err := friend.st.SetVisibleToPeers(ctx, fu.ID, true); err != nil {
		t.Fatal(err)
	}
	host.srvAPI.presence.WatchingHere(hu.ID, "Blade Runner")
	item := host.addFile(t, "blade runner.mkv", []byte("the bytes of a film"))
	return unreachableFriend{
		host: host, friend: friend,
		hostFP:   identity.Normalize(host.srvAPI.ident.Fingerprint()),
		friendFP: identity.Normalize(friend.srvAPI.ident.Fingerprint()),
		hostUser: hu.ID, friendUser: fu.ID, item: item,
	}
}

func (x unreachableFriend) hostsPeer(t *testing.T) store.Peer {
	t.Helper()
	p, err := x.host.st.PeerByFingerprint(context.Background(), x.friendFP)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (x unreachableFriend) hostsRoster(t *testing.T) []store.RemotePerson {
	t.Helper()
	people, err := x.host.st.RemotePeople(context.Background(), x.friendFP)
	if err != nil {
		t.Fatal(err)
	}
	return people
}

// The test sentence of ADR 0103.
func TestAFriendWhoCannotBeReachedCanStillJoin(t *testing.T) {
	x := newUnreachableFriend(t)
	ctx := context.Background()

	// The control: the host cannot reach the friend, so the fetch fails and
	// leaves the host knowing nobody. This is the state that used to be the
	// end of it.
	if err := x.host.srvAPI.refreshPeer(ctx, x.hostsPeer(t)); err == nil {
		t.Fatal("the host reached a friend with no listener; the test proves nothing")
	}
	if got := x.hostsRoster(t); len(got) != 0 || x.hostsPeer(t).State == store.PeerPaired {
		t.Fatalf("before any push: roster %v, state %s", got, x.hostsPeer(t).State)
	}

	// One tick of the friend's own watcher.
	x.friend.srvAPI.peerTick(ctx)

	if st := x.hostsPeer(t).State; st != store.PeerPaired {
		t.Fatalf("after the push the host holds the friend as %s, want paired", st)
	}
	got := x.hostsRoster(t)
	if len(got) != 1 || got[0].ID != x.friendUser {
		t.Fatalf("host's roster of the friend: %+v", got)
	}

	// People, as the host sees it: not reachable, but reaching us, and the
	// friend listed by name. That is the difference between this and a
	// friend whose machine is off, and the page has to be able to say it.
	resp := x.host.authed(t, "GET", "/api/people/peers", nil)
	var people struct {
		Peers []struct {
			Fingerprint string           `json:"fingerprint"`
			Reachable   bool             `json:"reachable"`
			ReachesUs   bool             `json:"reaches_us"`
			People      []map[string]any `json:"people"`
		} `json:"peers"`
	}
	decode(t, resp, &people)
	if len(people.Peers) != 1 || people.Peers[0].Reachable || !people.Peers[0].ReachesUs ||
		len(people.Peers[0].People) != 1 {
		t.Fatalf("People on the host: %+v", people.Peers)
	}

	// So the host can let them see it, through its own route...
	resp = x.host.authed(t, "PUT",
		"/api/people/peers/"+x.friendFP+"/"+x.friendUser+"/presence", map[string]any{"on": true})
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("grant: %d", resp.StatusCode)
	}

	// ...and the friend asks to join, is let in, and joins: every call of it
	// from the friend to the host, none the other way.
	call := func(method, path string, body any) (int, string) {
		resp := x.friend.authed(t, method, "/api/peers/"+x.hostFP+path, body)
		defer resp.Body.Close()
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		return resp.StatusCode, b.String()
	}
	if code, raw := call("POST", "/together/requests", map[string]any{"person": x.hostUser}); code != http.StatusOK {
		t.Fatalf("ask to join: %d %s", code, raw)
	}
	tsx := twoServers{host: x.host, guest: x.friend, hostFP: x.hostFP, item: x.item}
	room := tsx.hostAccepts(t)
	if code, raw := call("POST", "/together/"+room+"/join", nil); code != http.StatusOK {
		t.Fatalf("join: %d %s", code, raw)
	}
	code, raw := call("GET", "/together/"+room, nil)
	if code != http.StatusOK || !strings.Contains(raw, `"server":"Utopia"`) {
		t.Fatalf("the room as the friend sees it: %d %s", code, raw)
	}
	_ = together.RequestAccepted
}

// An opt-out on the friend's side reaches the host by the next tick, and takes
// its grants with it, exactly as a fetched roster's does (ADR 0035).
func TestAPushedOptOutTakesBackItsGrants(t *testing.T) {
	x := newUnreachableFriend(t)
	ctx := context.Background()
	x.friend.srvAPI.peerTick(ctx)
	if err := x.host.st.GrantPresence(ctx, x.hostUser, x.friendFP, x.friendUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	if err := x.friend.st.SetVisibleToPeers(ctx, x.friendUser, false); err != nil {
		t.Fatal(err)
	}
	// A changed roster is due at once, not after pushEvery.
	x.friend.srvAPI.peerTick(ctx)
	if got := x.hostsRoster(t); len(got) != 0 {
		t.Fatalf("after opting out the host still lists %+v", got)
	}
	readers, err := x.host.st.ReadersOf(ctx, x.friendFP, x.friendUser)
	if err != nil {
		t.Fatal(err)
	}
	if len(readers) != 0 {
		t.Fatalf("a grant survived the opt-out: %v", readers)
	}
}

// A push counts as a fetch: the host does not dial out straight after being
// told, which for an unreachable friend would be a failure every tick.
func TestAPushCountsAsAFetch(t *testing.T) {
	x := newUnreachableFriend(t)
	x.friend.srvAPI.peerTick(context.Background())
	x.host.srvAPI.rosterMu.Lock()
	at, ok := x.host.srvAPI.rosterAt[x.friendFP]
	x.host.srvAPI.rosterMu.Unlock()
	if !ok || time.Since(at) > time.Minute {
		t.Fatalf("rosterAt after a push: %v %v", at, ok)
	}
}

// A server that has not paired with us is refused at the door, and a roster
// that is not a roster is refused before anything is stored.
func TestAPushIsGatedAndBounded(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	// No peer certificate at all: the gate, not the handler, says no.
	req := httptest.NewRequest("PUT", "/api/federation/roster", strings.NewReader(`{"people":[]}`))
	rec := httptest.NewRecorder()
	h.srvAPI.Handler().ServeHTTP(rec, req)
	if rec.Code < 400 || rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
		t.Fatalf("a push with no certificate: %d", rec.Code)
	}

	x := newUnreachableFriend(t)
	ctx := context.Background()
	p, err := x.friend.st.PeerByFingerprint(ctx, x.hostFP)
	if err != nil {
		t.Fatal(err)
	}
	send := func(body any) int {
		ans, err := x.friend.srvAPI.sendPeer(ctx, p, "PUT", "/api/federation/roster", body)
		if err != nil {
			t.Fatal(err)
		}
		return ans.status
	}
	many := make([]store.RemotePerson, maxRosterPeople+1)
	for i := range many {
		many[i] = store.RemotePerson{ID: "u_" + itoa(int64(i)), Name: "x"}
	}
	for name, body := range map[string]any{
		"too many":      rosterBody{People: many},
		"no id":         rosterBody{People: []store.RemotePerson{{Name: "nobody"}}},
		"a path for id": rosterBody{People: []store.RemotePerson{{ID: "../u_1", Name: "x"}}},
		"not a roster":  "hello",
	} {
		if code := send(body); code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, code)
		}
	}
	if got := x.hostsRoster(t); len(got) != 0 {
		t.Fatalf("a refused push stored %+v", got)
	}
}

// A roster overtaken in flight never undoes a fresher one; a clock that moved
// back a long way is believed; an old server's unstamped fetch is taken.
func TestAStaleRosterNeverUndoesAFresherOne(t *testing.T) {
	rp := newRosterPush()
	now := time.Now().UnixMilli()
	if !rp.acceptAsOf("fp", now) {
		t.Fatal("the first roster refused")
	}
	if rp.acceptAsOf("fp", now-2000) {
		t.Fatal("a roster two seconds older replaced a fresher one")
	}
	if !rp.acceptAsOf("fp", now+1) {
		t.Fatal("a fresher roster refused")
	}
	if !rp.acceptAsOf("fp", now-int64(time.Hour/time.Millisecond)) {
		t.Fatal("a clock set back an hour is never believed again")
	}
	if !rp.acceptAsOf("fp", 0) {
		t.Fatal("an old server's unstamped roster refused")
	}
	if !rp.acceptAsOf("other", now-2000) {
		t.Fatal("one peer's stamp held against another")
	}
}

// When a push is due: at once, on a change, after pushEvery, and on every
// tick only while a newly added peer has not yet taken one.
func TestWhenARosterIsPushed(t *testing.T) {
	now := time.Now()
	fresh := store.Peer{Fingerprint: "fp", AddedAt: now.Unix()}
	old := store.Peer{Fingerprint: "fp", AddedAt: now.Add(-time.Hour).Unix()}
	for _, c := range []struct {
		name string
		peer store.Peer
		rec  *pushRecord
		key  string
		want bool
	}{
		{"never sent", old, nil, "a", true},
		{"sent and taken, unchanged", old, &pushRecord{at: now, key: "a", accepted: true}, "a", false},
		{"sent and taken, changed", old, &pushRecord{at: now, key: "a", accepted: true}, "b", true},
		{"taken long ago", old, &pushRecord{at: now.Add(-pushEvery), key: "a", accepted: true}, "a", true},
		{"refused, peer just added", fresh, &pushRecord{at: now, key: "a"}, "a", true},
		{"refused, peer added long ago", old, &pushRecord{at: now, key: "a"}, "a", false},
		{"refused long ago", old, &pushRecord{at: now.Add(-pushEvery), key: "a"}, "a", true},
	} {
		rp := newRosterPush()
		if c.rec != nil {
			rp.sent["fp"] = *c.rec
		}
		if got := rp.pushDue(c.peer, c.key, now); got != c.want {
			t.Errorf("%s: due = %v, want %v", c.name, got, c.want)
		}
	}
}
