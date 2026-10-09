package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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
 * A room on a paired server, through the guest's own server (Phase 5, step 4).
 *
 * Unlike the rest of the peer tests, these run **two real servers**: the host
 * serves the actual peer TLS configuration (mutual TLS, its identity
 * certificate, a client certificate required), and the guest's server reaches
 * it with the pinned client every other federation call uses. What a handler
 * test cannot see, and these can, is everything between: the gate list, the
 * CSRF check on a POST that arrives with no Origin, the pin, and whether an
 * answer survives the hop with its status intact.
 *
 * Still one machine and one process. Nothing here proves two machines.
 */

type twoServers struct {
	host, guest *harness
	hostFP      string // the host, as the guest's server knows it
	hostUser    string
	guestUser   string
	item        int64
	film        []byte
}

func newTwoServers(t *testing.T) twoServers {
	t.Helper()
	ctx := context.Background()
	host := newHarness(t)
	host.secure(t, "a good long password")
	guest := newHarness(t)
	guest.secure(t, "a good long password")

	// The host answers peers on its own listener, exactly as the shipped
	// server does once a peer's ClientHello has been diverted to it.
	cfg, err := peer.ServerConfig(host.srvAPI.ident)
	if err != nil {
		t.Fatal(err)
	}
	peerSrv := httptest.NewUnstartedServer(host.srvAPI.Handler())
	peerSrv.TLS = cfg
	peerSrv.StartTLS()
	t.Cleanup(peerSrv.Close)

	// The guest's server pairs with the host at that listener.
	inv, err := peer.Encode(host.srvAPI.ident, "Chris's", []string{peerSrv.Listener.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	guest.authed(t, "POST", "/api/peers", map[string]any{"invite": inv}).Body.Close()
	hostFP := identity.Normalize(host.srvAPI.ident.Fingerprint())
	if err := guest.st.SetPeerState(ctx, hostFP, store.PeerPaired); err != nil {
		t.Fatal(err)
	}
	// And the host with the guest's server. Its address is never dialled here.
	pairedPeer(t, host, guest.srvAPI.ident, "Utopia")
	guestFP := identity.Normalize(guest.srvAPI.ident.Fingerprint())

	hu, err := host.st.UserByName(ctx, testUser)
	if err != nil {
		t.Fatal(err)
	}
	gu, err := guest.st.UserByName(ctx, testUser)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.st.ReplaceRemotePeople(ctx, guestFP, []store.RemotePerson{{ID: gu.ID, Name: "Georgia"}}); err != nil {
		t.Fatal(err)
	}
	if err := host.st.GrantPresence(ctx, hu.ID, guestFP, gu.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	host.srvAPI.presence.WatchingHere(hu.ID, "Blade Runner")

	film := []byte("the bytes of a film nobody shared")
	item := host.addFile(t, "blade runner.mkv", film)
	return twoServers{host: host, guest: guest, hostFP: hostFP,
		hostUser: hu.ID, guestUser: gu.ID, item: item, film: film}
}

// guestCall is the guest's client talking to the guest's own server.
func (x twoServers) guestCall(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	resp := x.guest.authed(t, method, "/api/peers/"+x.hostFP+path, body)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// hostRoom is the host's client opening a room around the film and accepting
// the one open request into it.
func (x twoServers) hostAccepts(t *testing.T) string {
	t.Helper()
	resp := x.host.authed(t, "GET", "/api/together/requests", nil)
	var list struct {
		Requests []together.Request `json:"requests"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list.Requests) != 1 {
		t.Fatalf("host has %d requests, want 1", len(list.Requests))
	}
	resp = x.host.authed(t, "POST", "/api/together", map[string]any{"item_id": x.item})
	var room together.Session
	_ = json.NewDecoder(resp.Body).Decode(&room)
	resp.Body.Close()
	resp = x.host.authed(t, "POST", "/api/together/requests/"+list.Requests[0].ID+"/accept",
		map[string]any{"room_id": room.ID})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept: %d", resp.StatusCode)
	}
	return room.ID
}

func (x twoServers) ask(t *testing.T) relayAnswer {
	t.Helper()
	code, raw := x.guestCall(t, "POST", "/together/requests", map[string]any{"person": x.hostUser})
	if code != http.StatusOK {
		t.Fatalf("ask: %d %s", code, raw)
	}
	var a relayAnswer
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	return a
}

// The phase's test sentence, through two servers.
func TestARoomCrossesTwoRealServers(t *testing.T) {
	x := newTwoServers(t)

	a := x.ask(t)
	if a.State != together.RequestPending || a.ID == "" {
		t.Fatalf("ask = %+v, want pending", a)
	}
	room := x.hostAccepts(t)

	code, raw := x.guestCall(t, "GET", "/together/requests/"+a.ID, nil)
	var answer relayAnswer
	_ = json.Unmarshal(raw, &answer)
	if code != http.StatusOK || answer.State != together.RequestAccepted || answer.RoomID != room {
		t.Fatalf("answer = %d %+v, want accepted into %s", code, answer, room)
	}

	if code, raw := x.guestCall(t, "POST", "/together/"+room+"/join", nil); code != http.StatusOK {
		t.Fatalf("join: %d %s", code, raw)
	}
	code, raw = x.guestCall(t, "GET", "/together/"+room, nil)
	var s together.Session
	_ = json.Unmarshal(raw, &s)
	if code != http.StatusOK || s.ItemID != x.item || len(s.Members) != 2 {
		t.Fatalf("poll = %d item %d with %d members, want %d with 2", code, s.ItemID, len(s.Members), x.item)
	}
	var guest *together.Member
	for i := range s.Members {
		if !s.Members[i].Host {
			guest = &s.Members[i]
		}
	}
	if guest == nil || guest.Name != "Georgia" || guest.Server != "Utopia" {
		t.Errorf("the guest in the room is %+v, want Georgia from Utopia", guest)
	}

	// The film, unshared, as a room member: the bytes arrive intact.
	code, raw = x.guestCall(t, "GET", "/stream?item="+itoa64(x.item)+"&together=1", nil)
	if code != http.StatusOK || !bytes.Equal(raw, x.film) {
		t.Errorf("the room's film through the relay: %d, %d bytes, want 200 and the film", code, len(raw))
	}
	if code, _ := x.guestCall(t, "GET", "/item/"+itoa64(x.item)+"?together=1", nil); code != http.StatusOK {
		t.Errorf("the room's item details through the relay: %d, want 200", code)
	}

	// Leaving, and then the room is gone: a 404 that stays a 404 across the
	// hop, not a gateway error that would send the player looking for a
	// network fault.
	if code, _ := x.guestCall(t, "DELETE", "/together/"+room+"/members/me", nil); code != http.StatusNoContent {
		t.Errorf("leave: %d, want 204", code)
	}
	if code, _ := x.guestCall(t, "GET", "/together/"+room, nil); code != http.StatusNotFound {
		t.Errorf("poll after leaving: %d, want 404", code)
	}
}

/*
 * Without saying it is playing as a member, the guest's server names nobody,
 * and the unshared film is refused. A person put on the query by the client
 * is stripped, so nobody in a household can play as somebody else.
 */
func TestOnlyTheCallerIsEverNamedToTheHost(t *testing.T) {
	x := newTwoServers(t)
	x.ask(t)
	x.hostAccepts(t)

	/*
	 * Both a route that builds its own query (the file) and one that forwards
	 * the client's (the delivery decision). The forged person is the member's
	 * own id, the strongest case: forwarded, it would be admitted.
	 *
	 * The positive control comes first. Without it every refusal below would
	 * pass just as well if the route were broken for everybody.
	 */
	for _, route := range []string{"/stream", "/playback"} {
		if code, _ := x.guestCall(t, "GET", route+"?item="+itoa64(x.item)+"&together=1", nil); code != http.StatusOK {
			t.Fatalf("%s as a member: %d, want 200 (the control)", route, code)
		}
		for _, q := range []string{
			"",
			"&person=" + x.guestUser,
			"&person=" + x.guestUser + "&together=",
		} {
			if code, _ := x.guestCall(t, "GET", route+"?item="+itoa64(x.item)+q, nil); code == http.StatusOK {
				t.Errorf("%s%s was served: a film admitted only by the room, without the member opt-in", route, q)
			}
		}
	}

	// The ask names the caller, whatever the client adds.
	resp := x.host.authed(t, "GET", "/api/together/requests", nil)
	defer resp.Body.Close()
	var list struct {
		Requests []together.Request `json:"requests"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&list)
	for _, r := range list.Requests {
		if r.Person != x.guestUser {
			t.Errorf("a request names %q, want the caller %q", r.Person, x.guestUser)
		}
	}
}

// A decline crosses as not now, and asking again straight away is not now too.
func TestADeclineCrossesAsNotNow(t *testing.T) {
	x := newTwoServers(t)
	a := x.ask(t)

	resp := x.host.authed(t, "POST", "/api/together/requests/"+a.ID+"/decline", nil)
	resp.Body.Close()
	_, raw := x.guestCall(t, "GET", "/together/requests/"+a.ID, nil)
	var answer relayAnswer
	_ = json.Unmarshal(raw, &answer)
	if answer.State != "not_now" || answer.RoomID != "" {
		t.Errorf("after a decline the guest reads %+v, want not_now", answer)
	}
	if again := x.ask(t); again.State != "not_now" {
		t.Errorf("asking again reads %q, want not_now", again.State)
	}
}

// Being paired is not being admitted: the room refuses through the relay too.
func TestTheRelayCannotWalkIntoARoom(t *testing.T) {
	x := newTwoServers(t)
	resp := x.host.authed(t, "POST", "/api/together", map[string]any{"item_id": x.item})
	var room together.Session
	_ = json.NewDecoder(resp.Body).Decode(&room)
	resp.Body.Close()

	if code, _ := x.guestCall(t, "POST", "/together/"+room.ID+"/join", nil); code != http.StatusNotFound {
		t.Errorf("join without being accepted: %d, want 404", code)
	}
}

// A host that is not answering is a gateway error, which is a different thing
// from a room that has ended.
func TestAnUnreachableHostIsAGatewayError(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	away := anotherServer(t)
	pairedPeer(t, h, away, "Asleep")

	resp := h.authed(t, "POST", "/api/peers/"+identity.Normalize(away.Fingerprint())+"/together/requests",
		map[string]any{"person": "u_x"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("asking an unreachable server: %d, want 502", resp.StatusCode)
	}
}

// A member's playlist sends its segments as a member too, or a room-only film
// plays its playlist and fails on the first segment.
func TestAMembersPlaylistKeepsSayingSo(t *testing.T) {
	const fp = "AAAABBBBCCCCDDDD"
	in := strings.Join([]string{
		"#EXTM3U",
		`#EXT-X-MAP:URI="/api/federation/hls/42/sess/init.mp4"`,
		"#EXTINF:6.000,",
		"/api/federation/hls/42/sess/1.m4s",
		"#EXT-X-ENDLIST",
		"",
	}, "\n")

	got := repointPlaylistAtPeer(in, fp, true)
	for _, want := range []string{
		`URI="/api/peers/` + fp + `/hls/42/sess/init.mp4?together=1"`,
		"/api/peers/" + fp + "/hls/42/sess/1.m4s?together=1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if plain := repointPlaylistAtPeer(in, fp, false); strings.Contains(plain, "together") {
		t.Error("a playlist fetched without the opt-in named a member")
	}
}

func TestNoPersonIsForwardedFromTheClient(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/peers/x/transcode?item=7&t=5&person=u_other&together=1", nil)
	if got := forwardedQuery(r); got != "&t=5" {
		t.Errorf("forwardedQuery = %q, want only t", got)
	}
}

func TestWithQuery(t *testing.T) {
	for _, c := range []struct{ path, q, want string }{
		{"/a", "", "/a"},
		{"/a", "&t=1", "/a?t=1"},
		{"/a?x=1", "&t=1", "/a?x=1&t=1"},
	} {
		if got := withQuery(c.path, c.q); got != c.want {
			t.Errorf("withQuery(%q, %q) = %q, want %q", c.path, c.q, got, c.want)
		}
	}
}

/*
 * Cancel crosses: the guest takes the ask back through its own server, the
 * host's prompt list empties, and asking again is pending at once rather
 * than not now — a withdraw is not a decline. Run over the real peer TLS, so
 * the DELETE is proven to pass the federation gate and the relay.
 */
func TestACancelledAskLeavesTheHostsPrompt(t *testing.T) {
	x := newTwoServers(t)
	a := x.ask(t)

	pending := func() int {
		t.Helper()
		resp := x.host.authed(t, "GET", "/api/together/requests", nil)
		defer resp.Body.Close()
		var list struct {
			Requests []together.Request `json:"requests"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&list)
		return len(list.Requests)
	}
	if n := pending(); n != 1 {
		t.Fatalf("the host has %d requests before the cancel, want 1 (the positive control)", n)
	}

	code, raw := x.guestCall(t, "DELETE", "/together/requests/"+a.ID, nil)
	if code != http.StatusOK {
		t.Fatalf("cancel: %d %s", code, raw)
	}
	var answer relayAnswer
	_ = json.Unmarshal(raw, &answer)
	if answer.State != "withdrawn" {
		t.Errorf("cancel answered %+v", answer)
	}
	if n := pending(); n != 0 {
		t.Errorf("the host still has %d requests after the cancel", n)
	}
	if again := x.ask(t); again.State != "pending" {
		t.Errorf("asking again after a cancel is %q, want pending", again.State)
	}
}
