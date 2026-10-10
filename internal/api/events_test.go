package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"lancast/internal/identity"
	"lancast/internal/store"
)

/*
 * The event stream and the peer watcher (ADR 0079).
 *
 * What these prove is wiring: that a change made by the server reaches an open
 * window without that window asking, and that a pairing becomes mutual without
 * anybody opening People. Both were broken on the first remote evening, and
 * nothing failed either time: the server was right and the screen was stale.
 */

// stream opens GET /api/events as the harness's signed-in user and returns a
// reader of its lines. The response is closed when the test ends.
func stream(t *testing.T, h *harness) *bufio.Reader {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, "GET", h.srv.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.cookie != nil {
		req.AddCookie(h.cookie)
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/events: %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type %q", ct)
	}
	r := bufio.NewReader(resp.Body)
	// The preamble: retry, then a comment, then a blank line. Reading it
	// also proves the handler subscribed before a test publishes.
	waitForLine(t, r, ": connected")
	waitForSubscriber(t, h)
	return r
}

func waitForSubscriber(t *testing.T, h *harness) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(h.srvAPI.events.Users()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the stream never subscribed")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitForLine(t *testing.T, r *bufio.Reader, want string) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				done <- err
				return
			}
			if strings.TrimRight(line, "\n") == want {
				done <- nil
				return
			}
		}
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stream ended before %q: %v", want, err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("no %q on the stream", want)
	}
}

// nextTopics reads until the next changed event and returns its topics.
func nextTopics(t *testing.T, r *bufio.Reader) []string {
	t.Helper()
	got := make(chan []string, 1)
	errc := make(chan error, 1)
	go func() {
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				errc <- err
				return
			}
			if data, ok := strings.CutPrefix(strings.TrimRight(line, "\n"), "data: "); ok {
				var body struct {
					Topics []string `json:"topics"`
				}
				if err := json.Unmarshal([]byte(data), &body); err != nil {
					errc <- err
					return
				}
				got <- body.Topics
				return
			}
		}
	}()
	select {
	case topics := <-got:
		return topics
	case err := <-errc:
		t.Fatalf("stream: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("no changed event")
	}
	return nil
}

func has(topics []string, want string) bool {
	for _, t := range topics {
		if t == want {
			return true
		}
	}
	return false
}

func TestTheStreamRefusesAStranger(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	resp, err := h.srv.Client().Get(h.srv.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /api/events with no session: %d, want 401", resp.StatusCode)
	}
}

// The store announces its own writes, so a pairing added by any caller
// reaches every open window, not only the one that pasted the invite.
func TestAddingAPeerReachesAnOpenWindow(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	r := stream(t, h)

	other, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairedPeer(t, h, other, "Utopia")

	if got := nextTopics(t, r); !has(got, TopicPeers) {
		t.Fatalf("got %v, want %q", got, TopicPeers)
	}
}

func TestCreatingALibraryReachesAnOpenWindow(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	r := stream(t, h)

	if _, err := h.st.CreateLibrary(context.Background(), "Films", "movie", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if got := nextTopics(t, r); !has(got, TopicLibraries) {
		t.Fatalf("got %v, want %q", got, TopicLibraries)
	}
}

// Shutdown must not wait out its grace period on open windows.
func TestCloseEventsEndsAnOpenStream(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	r := stream(t, h)
	h.srvAPI.CloseEvents()

	done := make(chan error, 1)
	go func() {
		for {
			if _, err := r.ReadString('\n'); err != nil {
				done <- err
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the stream stayed open after CloseEvents")
	}
}

/*
 * The fault from the first remote evening, end to end.
 *
 * Both servers hold each other and can reach each other, and the guest's
 * server still says `added`. Before the watcher, only the People page could
 * fix that. Now one tick of the watcher must, with no page involved.
 */
func TestTheWatcherMakesAPairingMutual(t *testing.T) {
	x := newTwoServers(t)
	ctx := context.Background()
	if err := x.guest.st.SetPeerState(ctx, x.hostFP, store.PeerAdded); err != nil {
		t.Fatal(err)
	}
	r := stream(t, x.guest)

	x.guest.srvAPI.peerTick(ctx)

	p, err := x.guest.st.PeerByFingerprint(ctx, x.hostFP)
	if err != nil {
		t.Fatal(err)
	}
	if p.State != store.PeerPaired {
		t.Fatalf("after one watcher tick the peer is %q, want %q", p.State, store.PeerPaired)
	}
	if got := nextTopics(t, r); !has(got, TopicPeers) {
		t.Fatalf("the open window heard %v, want %q", got, TopicPeers)
	}
}

/*
 * A friend starting a film reaches the People page of the person allowed to
 * see it, without the page asking. The second half is the positive control's
 * twin: an unchanged answer must not publish, or every window would refetch
 * People every ten seconds for nothing.
 */
func TestTheWatcherPublishesOnlyAChangeInPresence(t *testing.T) {
	x := newTwoServers(t)
	ctx := context.Background()
	r := stream(t, x.guest)

	// The first answer for a newly open window counts as a change.
	x.guest.srvAPI.peerTick(ctx)
	if got := nextTopics(t, r); !has(got, TopicPresence) {
		t.Fatalf("first tick: heard %v, want %q", got, TopicPresence)
	}

	// Nothing changed on the host.
	sub := x.guest.srvAPI.events.Subscribe(x.guestUser)
	defer x.guest.srvAPI.events.Unsubscribe(sub)
	x.guest.srvAPI.peerTick(ctx)
	quiet, cancel := context.WithTimeout(ctx, 600*time.Millisecond)
	if got := sub.Next(quiet); got != nil {
		cancel()
		t.Fatalf("an unchanged answer published %v", got)
	}
	cancel()

	// The host's person stops watching: a change.
	x.host.srvAPI.presence.WatchingHere(x.hostUser, "")
	x.guest.srvAPI.peerTick(ctx)
	if got := nextTopics(t, r); !has(got, TopicPresence) {
		t.Fatalf("after the film stopped: heard %v, want %q", got, TopicPresence)
	}
}

/*
 * A friend's server going quiet reaches People too. People draws "not
 * answering" differently from "idle", and without a notice it would go on
 * showing a film for up to a minute after the far machine went off.
 */
func TestThePeerGoingQuietIsAChangeInPresence(t *testing.T) {
	x := newTwoServers(t)
	ctx := context.Background()
	r := stream(t, x.guest)

	x.guest.srvAPI.peerTick(ctx)
	if got := nextTopics(t, r); !has(got, TopicPresence) {
		t.Fatalf("first tick: heard %v, want %q", got, TopicPresence)
	}

	// Point the guest's record of the host at a port nobody listens on, and
	// forget the address that last answered, which would be tried first.
	if err := x.guest.st.AddPeer(ctx, store.Peer{
		Fingerprint: x.hostFP, Name: "Chris's", Addrs: []string{"127.0.0.1:1"},
	}); err != nil {
		t.Fatal(err)
	}
	x.guest.srvAPI.rosterMu.Lock()
	delete(x.guest.srvAPI.goodAddr, x.hostFP)
	x.guest.srvAPI.rosterMu.Unlock()
	// AddPeer itself announces peers; read past it.
	if got := nextTopics(t, r); !has(got, TopicPeers) {
		t.Fatalf("repointing: heard %v", got)
	}

	x.guest.srvAPI.peerTick(ctx)
	if got := nextTopics(t, r); !has(got, TopicPresence) {
		t.Fatalf("after the peer went quiet: heard %v, want %q", got, TopicPresence)
	}
}

/*
 * People reports the state a peer ended in, not the one read before its
 * refresh. Otherwise the very request that promoted a pairing still answered
 * "added", and only the next one told the truth.
 */
func TestPeopleReportsThePromotionItCaused(t *testing.T) {
	x := newTwoServers(t)
	ctx := context.Background()
	if err := x.guest.st.SetPeerState(ctx, x.hostFP, store.PeerAdded); err != nil {
		t.Fatal(err)
	}

	resp := x.guest.authed(t, "GET", "/api/people/peers", nil)
	defer resp.Body.Close()
	var body struct {
		Peers []struct {
			Fingerprint string `json:"fingerprint"`
			State       string `json:"state"`
		} `json:"peers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Peers) != 1 {
		t.Fatalf("got %d peers, want 1", len(body.Peers))
	}
	if body.Peers[0].State != store.PeerPaired {
		t.Fatalf("the answer that promoted the pairing said %q, want %q",
			body.Peers[0].State, store.PeerPaired)
	}
}

// A call in from a peer still marked `added` asks for its roster at once.
func TestACallFromAnAddedPeerKicksTheWatcher(t *testing.T) {
	x := newTwoServers(t)
	ctx := context.Background()
	guestFP := identity.Normalize(x.guest.srvAPI.ident.Fingerprint())
	if err := x.host.st.SetPeerState(ctx, guestFP, store.PeerAdded); err != nil {
		t.Fatal(err)
	}
	// Drain kicks left by the harness's own pairing.
	for len(x.host.srvAPI.peerKick) > 0 {
		<-x.host.srvAPI.peerKick
	}

	x.guest.srvAPI.peerTick(ctx)

	select {
	case fp := <-x.host.srvAPI.peerKick:
		if fp != guestFP {
			t.Fatalf("kicked %s, want %s", fp, guestFP)
		}
	default:
		t.Fatal("a call from an added peer did not kick the watcher")
	}
}
