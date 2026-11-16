package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"lancast/internal/identity"
	"lancast/internal/peer"
	"lancast/internal/store"
)

/*
 * R02-5. What a paired server sends back is bounded and checked.
 *
 * These need a peer that misbehaves, which the two-server harness cannot be:
 * its host is a real LANcast. So the far side here is a bare handler behind
 * the real peer TLS, under an identity the guest's server has paired with,
 * reached by the same pinned client every federation call uses.
 */
func misbehavingPeer(t *testing.T, h *harness, handler http.Handler) store.Peer {
	t.Helper()
	id := anotherServer(t)
	cfg, err := peer.ServerConfig(id)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = cfg
	srv.StartTLS()
	t.Cleanup(srv.Close)

	inv, err := peer.Encode(id, "Hostile", []string{srv.Listener.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	h.authed(t, "POST", "/api/peers", map[string]any{"invite": inv}).Body.Close()
	fp := identity.Normalize(id.Fingerprint())
	ctx := context.Background()
	if err := h.st.SetPeerState(ctx, fp, store.PeerPaired); err != nil {
		t.Fatal(err)
	}
	p, err := h.st.PeerByFingerprint(ctx, fp)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A roster that never ends is cut off at the cap, not read until the
// deadline. Measured from the far side: how much it managed to send before
// the reader hung up. Unbounded, three seconds over loopback is hundreds of
// megabytes, every one of them decoded into memory.
func TestAnEndlessPeerAnswerIsCutOff(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	var sent atomic.Int64
	p := misbehavingPeer(t, h, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		n, _ := w.Write([]byte(`{"people":[`))
		sent.Add(int64(n))
		chunk := []byte(strings.Repeat(`{"id":"u_1","name":"a"},`, 2048))
		for r.Context().Err() == nil {
			n, err := w.Write(chunk)
			sent.Add(int64(n))
			if err != nil {
				return
			}
		}
	}))

	if err := h.srvAPI.refreshPeer(context.Background(), p); err == nil {
		t.Fatal("an endless roster decoded without error")
	}
	// Kernel and TLS buffers sit between the cap and what the far side
	// counts as sent, so the bound is loose; unbounded is far past it.
	if got := sent.Load(); got > 8*maxPeerJSON {
		t.Fatalf("the peer sent %d MiB before it was cut off (cap %d MiB)",
			got>>20, maxPeerJSON>>20)
	}
}

// A fetched roster passes the checks a pushed one does, and one that fails
// them is not stored. The control is a well-formed roster from the same peer.
func TestAFetchedRosterIsCheckedLikeAPushedOne(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	ctx := context.Background()

	var roster atomic.Value
	p := misbehavingPeer(t, h, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"people": roster.Load()})
	}))
	fresh := func() {
		h.srvAPI.rosterMu.Lock()
		delete(h.srvAPI.rosterAt, p.Fingerprint)
		h.srvAPI.rosterMu.Unlock()
	}

	roster.Store([]store.RemotePerson{{ID: "u_1", Name: "Ada"}})
	if err := h.srvAPI.refreshPeer(ctx, p); err != nil {
		t.Fatalf("control: a well-formed roster: %v", err)
	}
	if got, _ := h.st.RemotePeople(ctx, p.Fingerprint); len(got) != 1 {
		t.Fatalf("control: stored %v, want Ada", got)
	}

	tooMany := make([]store.RemotePerson, maxRosterPeople+1)
	for i := range tooMany {
		tooMany[i] = store.RemotePerson{ID: "u_x", Name: "n"}
	}
	for name, bad := range map[string][]store.RemotePerson{
		"a slash in an id": {{ID: "../u_1", Name: "Ada"}},
		"an empty id":      {{ID: "", Name: "Ada"}},
		"an overlong name": {{ID: "u_1", Name: strings.Repeat("a", 201)}},
		"too many people":  tooMany,
	} {
		fresh()
		roster.Store(bad)
		if err := h.srvAPI.refreshPeer(ctx, p); err == nil {
			t.Errorf("%s: the roster was accepted", name)
		}
		got, err := h.st.RemotePeople(ctx, p.Fingerprint)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].ID != "u_1" || got[0].Name != "Ada" {
			t.Errorf("%s: stored %d people, want Ada alone", name, len(got))
		}
	}
}
