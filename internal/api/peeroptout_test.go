package api

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

// askedAbout reports whether any heard request asked the far server's
// presence on behalf of person.
func askedAbout(heard []string, person string) bool {
	want := "/api/federation/presence?person=" + url.QueryEscape(person)
	for _, u := range heard {
		if strings.HasPrefix(u, want) {
			return true
		}
	}
	return false
}

/*
 * R02-4. A person who opted out of being listed to peers is not named to them.
 *
 * The question "what may u_7k2 see?" tells the far server that u_7k2 exists
 * and has a window open, every ten seconds. The positive control is the same
 * person asked about while still listed, over the same wire.
 */
func TestTheWatcherDoesNotNameAnOptedOutPerson(t *testing.T) {
	x := newTwoServers(t)
	ctx := context.Background()
	stream(t, x.guest) // a window open: the watcher has somebody to ask for

	x.guest.srvAPI.peerTick(ctx)
	if heard := x.heard.take(); !askedAbout(heard, x.guestUser) {
		t.Fatalf("control: a listed person with a window open was never asked about; heard %v", heard)
	}

	if err := x.guest.st.SetVisibleToPeers(ctx, x.guestUser, false); err != nil {
		t.Fatal(err)
	}
	x.guest.srvAPI.peerTick(ctx)
	if heard := x.heard.take(); askedAbout(heard, x.guestUser) {
		t.Fatalf("an opted-out person was named to the peer: %v", heard)
	}
}

// The People page asks on behalf of the person reading it, and must not
// either — while still saying whether the peer answers.
func TestPeopleDoesNotNameAnOptedOutReader(t *testing.T) {
	x := newTwoServers(t)
	ctx := context.Background()
	if err := x.guest.st.SetVisibleToPeers(ctx, x.guestUser, false); err != nil {
		t.Fatal(err)
	}

	resp := x.guest.authed(t, "GET", "/api/people/peers", nil)
	var body struct {
		Peers []struct {
			Reachable bool `json:"reachable"`
		} `json:"peers"`
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	if heard := x.heard.take(); askedAbout(heard, x.guestUser) {
		t.Fatalf("People named an opted-out reader to the peer: %v", heard)
	}
	if len(body.Peers) != 1 || !body.Peers[0].Reachable {
		t.Fatalf("peers %+v: the host answered its roster, so it is reachable", body.Peers)
	}
}
