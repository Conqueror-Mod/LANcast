package api

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"lancast/internal/identity"
	"lancast/internal/peer"
	"lancast/internal/store"
)

/*
 * A peer whose first recorded address is dead is still reached.
 *
 * Found between two real servers on 2026-10-09: each held the other's old
 * ZeroTier address first, an address that accepts nothing and refuses
 * nothing. Every call spent its three-second budget there and failed with
 * "context deadline exceeded" naming the LAN address that would have answered
 * in ten milliseconds — so presence and sharing were dead in both directions
 * for nine days. Two servers over the real peer TLS, the guest holding a
 * silent address ahead of the host's real one.
 */
func TestAPeerBehindADeadAddressIsStillReached(t *testing.T) {
	ctx := context.Background()
	host := newHarness(t)
	host.secure(t, "a good long password")
	guest := newHarness(t)
	guest.secure(t, "a good long password")

	cfg, err := peer.ServerConfig(host.srvAPI.ident)
	if err != nil {
		t.Fatal(err)
	}
	peerSrv := httptest.NewUnstartedServer(host.srvAPI.Handler())
	peerSrv.TLS = cfg
	peerSrv.StartTLS()
	t.Cleanup(peerSrv.Close)

	// The dead address: accepts, then silence, as a VPN interface the peer
	// has left behaves from outside.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var held []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, c := range held {
			c.Close()
		}
		mu.Unlock()
	})

	inv, err := peer.Encode(host.srvAPI.ident, "Aither",
		[]string{ln.Addr().String(), peerSrv.Listener.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	guest.authed(t, "POST", "/api/peers", map[string]any{"invite": inv}).Body.Close()
	hostFP := identity.Normalize(host.srvAPI.ident.Fingerprint())
	if err := guest.st.SetPeerState(ctx, hostFP, store.PeerPaired); err != nil {
		t.Fatal(err)
	}
	pairedPeer(t, host, guest.srvAPI.ident, "Utopia")

	// The positive control is the status itself: a 200 here is the host's
	// own answer, relayed. Before the race this was a 502 after three
	// seconds, every time.
	start := time.Now()
	resp := guest.authed(t, "GET", "/api/peers/"+hostFP+"/libraries", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d after %v: the dead address still blocked the live one",
			resp.StatusCode, time.Since(start))
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Errorf("took %v", el)
	}
}
