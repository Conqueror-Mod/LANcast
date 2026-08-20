package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

const fpA = "AAAABBBBCCCCDDDDEEEEFFFFGGGGHHHHIIIIJJJJKKKKLLLLMMMM"
const fpB = "NNNNOOOOPPPPQQQQRRRRSSSSTTTTUUUUVVVVWWWWXXXXYYYYZZZZ"

func samplePeer(fp string) Peer {
	return Peer{
		Fingerprint: fp,
		Name:        "Utopia",
		Addrs:       []string{"10.121.240.21:8080", "192.168.1.9:8080"},
	}
}

func TestAddAndReadPeer(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	if err := st.AddPeer(ctx, samplePeer(fpA)); err != nil {
		t.Fatal(err)
	}

	got, err := st.PeerByFingerprint(ctx, fpA)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Utopia" {
		t.Errorf("name = %q", got.Name)
	}
	// A new peer is added, not paired: this side accepting an invite is not the
	// other side having accepted ours (ADR 0044 section 3).
	if got.State != PeerAdded {
		t.Errorf("state = %q, want %q for a peer we merely added", got.State, PeerAdded)
	}
	if got.LastSeen != 0 {
		t.Errorf("last_seen = %d, want 0 for a peer never reached", got.LastSeen)
	}
	// Order is information: the sender listed the one they expect to work first.
	if len(got.Addrs) != 2 || got.Addrs[0] != "10.121.240.21:8080" {
		t.Errorf("addrs = %v, want them in the order given", got.Addrs)
	}
}

func TestUnknownPeerIsNotFound(t *testing.T) {
	st := newStore(t)
	if _, err := st.PeerByFingerprint(context.Background(), fpA); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

/*
 * Re-pasting an invite is how somebody tells you a peer has moved. It must
 * update the addresses and must not undo the pairing — a peer confirmed mutual
 * should not look like a stranger again because a fresh invite arrived.
 */
func TestReAddingAPeerRefreshesWithoutDemotingIt(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	if err := st.AddPeer(ctx, samplePeer(fpA)); err != nil {
		t.Fatal(err)
	}
	if err := st.SetPeerState(ctx, fpA, PeerPaired); err != nil {
		t.Fatal(err)
	}
	before, err := st.PeerByFingerprint(ctx, fpA)
	if err != nil {
		t.Fatal(err)
	}

	moved := samplePeer(fpA)
	moved.Name = "Utopia renamed"
	moved.Addrs = []string{"100.64.0.7:8080"}
	if err := st.AddPeer(ctx, moved); err != nil {
		t.Fatal(err)
	}

	after, err := st.PeerByFingerprint(ctx, fpA)
	if err != nil {
		t.Fatal(err)
	}
	if after.Name != "Utopia renamed" {
		t.Errorf("name = %q, want the new one", after.Name)
	}
	// Replaced wholesale, not merged: stale addresses are what make a
	// reachability check slow and its answer ambiguous.
	if len(after.Addrs) != 1 || after.Addrs[0] != "100.64.0.7:8080" {
		t.Errorf("addrs = %v, want only the new address", after.Addrs)
	}
	if after.State != PeerPaired {
		t.Errorf("state = %q, want the pairing to survive a re-paste", after.State)
	}
	if after.AddedAt != before.AddedAt {
		t.Error("added_at moved; when we met is not something a fresh invite knows better")
	}
}

func TestPeersListsEveryPeerWithItsOwnAddresses(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	if err := st.AddPeer(ctx, samplePeer(fpA)); err != nil {
		t.Fatal(err)
	}
	b := samplePeer(fpB)
	b.Name = "Somebody else"
	b.Addrs = []string{"10.0.0.5:8080"}
	if err := st.AddPeer(ctx, b); err != nil {
		t.Fatal(err)
	}

	peers, err := st.Peers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 2 {
		t.Fatalf("got %d peers, want 2", len(peers))
	}
	// Each peer's own addresses, not everybody's pooled together.
	for _, p := range peers {
		switch p.Fingerprint {
		case fpA:
			if len(p.Addrs) != 2 {
				t.Errorf("peer A has %d addresses, want 2", len(p.Addrs))
			}
		case fpB:
			if len(p.Addrs) != 1 || p.Addrs[0] != "10.0.0.5:8080" {
				t.Errorf("peer B addrs = %v", p.Addrs)
			}
		default:
			t.Errorf("unexpected peer %q", p.Fingerprint)
		}
	}
}

/*
 * The one that matters most in this file.
 *
 * ADR 0046 makes unpairing the single act that revokes everything "immediately,
 * with no per-person cleanup". That is only true if the cascade is real, so it
 * is asserted rather than assumed: a grant in phase 3 will hang off these rows,
 * and a remote person surviving their peer is somebody a grant could still name.
 */
func TestRemovingAPeerTakesItsPeopleAndAddresses(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	if err := st.AddPeer(ctx, samplePeer(fpA)); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceRemotePeople(ctx, fpA, []RemotePerson{
		{ID: "u1", Name: "Georgia"},
		{ID: "u2", Name: "Somebody"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := st.RemovePeer(ctx, fpA); err != nil {
		t.Fatal(err)
	}

	people, err := st.RemotePeople(ctx, fpA)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 0 {
		t.Errorf("%d remote people survived their peer", len(people))
	}
	addrs, err := st.peerAddresses(ctx, fpA)
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs[fpA]) != 0 {
		t.Errorf("%d addresses survived their peer", len(addrs[fpA]))
	}
}

func TestRemovingAnUnknownPeerIsNotFound(t *testing.T) {
	st := newStore(t)
	if err := st.RemovePeer(context.Background(), fpA); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

/*
 * A roster is the complete set as of now, so storing it must be able to take
 * somebody away. Merging would keep an account that had turned visibility off,
 * and an opt-out that cannot take back what it gave is not an opt-out.
 */
func TestReplacingARosterRemovesWhoIsNoLongerOnIt(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	if err := st.AddPeer(ctx, samplePeer(fpA)); err != nil {
		t.Fatal(err)
	}

	if err := st.ReplaceRemotePeople(ctx, fpA, []RemotePerson{
		{ID: "u1", Name: "Georgia"}, {ID: "u2", Name: "Leaving"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceRemotePeople(ctx, fpA, []RemotePerson{
		{ID: "u1", Name: "Georgia renamed"},
	}); err != nil {
		t.Fatal(err)
	}

	people, err := st.RemotePeople(ctx, fpA)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 1 || people[0].ID != "u1" {
		t.Fatalf("roster = %v, want only u1", people)
	}
	// The id is what survives a rename, which is why a grant names the id.
	if people[0].Name != "Georgia renamed" {
		t.Errorf("name = %q, want the refreshed one", people[0].Name)
	}
}

// A peer must exist before it can describe anybody. Without the check the
// foreign key would refuse anyway, with a message about a constraint rather
// than about an unknown peer.
func TestARosterNeedsAPeer(t *testing.T) {
	st := newStore(t)
	err := st.ReplaceRemotePeople(context.Background(), fpA, []RemotePerson{{ID: "u1", Name: "x"}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// One peer's roster cannot reach another's, and the same id on two peers is two
// different people.
func TestRostersAreScopedToTheirPeer(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	for _, fp := range []string{fpA, fpB} {
		if err := st.AddPeer(ctx, samplePeer(fp)); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.ReplaceRemotePeople(ctx, fpA, []RemotePerson{{ID: "u1", Name: "A's person"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceRemotePeople(ctx, fpB, []RemotePerson{{ID: "u1", Name: "B's person"}}); err != nil {
		t.Fatal(err)
	}

	a, err := st.RemotePeople(ctx, fpA)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 1 || a[0].Name != "A's person" {
		t.Errorf("peer A roster = %v; the same id on another peer is a different person", a)
	}
}

func TestPeerStateAndLastSeen(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	if err := st.AddPeer(ctx, samplePeer(fpA)); err != nil {
		t.Fatal(err)
	}

	when := time.Now().Add(-time.Minute)
	if err := st.MarkPeerSeen(ctx, fpA, when); err != nil {
		t.Fatal(err)
	}
	got, err := st.PeerByFingerprint(ctx, fpA)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastSeen != when.Unix() {
		t.Errorf("last_seen = %d, want %d", got.LastSeen, when.Unix())
	}

	if err := st.SetPeerState(ctx, fpA, PeerPaired); err != nil {
		t.Fatal(err)
	}
	if got, _ = st.PeerByFingerprint(ctx, fpA); got.State != PeerPaired {
		t.Errorf("state = %q, want %q", got.State, PeerPaired)
	}

	for _, err := range []error{
		st.SetPeerState(ctx, fpB, PeerPaired),
		st.MarkPeerSeen(ctx, fpB, when),
	} {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound for an unknown peer", err)
		}
	}
}

/*
 * Visibility to peers, and why it is its own switch.
 *
 * Default off, like share_activity in revision 22: appearing in a roster one
 * server hands another is a disclosure, and no upgrade may make somebody
 * visible as a side effect.
 */
func TestVisibilityToPeersDefaultsOff(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "", "Chris", "hash", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}

	on, err := st.VisibleToPeers(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if on {
		t.Fatal("a new account is visible to peers; the default must be off")
	}

	roster, err := st.RosterForPeers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(roster) != 0 {
		t.Errorf("roster = %v, want nobody until somebody opts in", roster)
	}
}

func TestRosterHoldsOnlyThoseWhoOptedIn(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	seen, err := st.CreateUser(ctx, "", "Willing", "hash", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, "", "Private", "hash", RoleMember); err != nil {
		t.Fatal(err)
	}
	if err := st.SetVisibleToPeers(ctx, seen.ID, true); err != nil {
		t.Fatal(err)
	}

	roster, err := st.RosterForPeers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(roster) != 1 || roster[0].Name != "Willing" {
		t.Fatalf("roster = %v, want only the account that opted in", roster)
	}

	// And off takes it back. An opt-out that cannot withdraw is not one.
	if err := st.SetVisibleToPeers(ctx, seen.ID, false); err != nil {
		t.Fatal(err)
	}
	if roster, _ = st.RosterForPeers(ctx); len(roster) != 0 {
		t.Errorf("roster = %v after opting out, want nobody", roster)
	}
}

func TestVisibilityOfAnUnknownAccount(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	if err := st.SetVisibleToPeers(ctx, "nobody", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("set: err = %v, want ErrNotFound", err)
	}
	if _, err := st.VisibleToPeers(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("get: err = %v, want ErrNotFound", err)
	}
}

// The fingerprint is the key, so a peer that moves cannot become two rows.
func TestAPeerThatMovesIsStillOnePeer(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	if err := st.AddPeer(ctx, samplePeer(fpA)); err != nil {
		t.Fatal(err)
	}
	moved := samplePeer(fpA)
	moved.Addrs = []string{"172.16.0.1:9999"}
	if err := st.AddPeer(ctx, moved); err != nil {
		t.Fatal(err)
	}

	peers, err := st.Peers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 {
		t.Errorf("a peer that moved became %d rows; the address is a hint, the fingerprint is the identity", len(peers))
	}
}
