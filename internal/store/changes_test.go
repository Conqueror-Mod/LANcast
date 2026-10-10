package store

import (
	"context"
	"testing"
)

/*
 * The store's change notices (ADR 0079).
 *
 * Each notice makes every open window refetch, and the peer lists it refetches
 * call the peer, so a notice for nothing is a real cost repeated on a timer.
 * These assert both halves: a change is announced, and a non-change is not.
 */

func countAnnouncements(s *Store) *[]string {
	var got []string
	s.OnChange(func(topic string) { got = append(got, topic) })
	return &got
}

func TestAPeerWriteIsAnnounced(t *testing.T) {
	s := newStore(t)
	got := countAnnouncements(s)
	ctx := context.Background()
	if err := s.AddPeer(ctx, Peer{Fingerprint: "FP1", Name: "Utopia"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPeerState(ctx, "FP1", PeerPaired); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 2 || (*got)[0] != ChangePeers || (*got)[1] != ChangePeers {
		t.Fatalf("announced %v, want [peers peers]", *got)
	}
}

// The watcher and the People page re-fetch rosters on timers. Only a roster
// that actually differs may wake every window.
func TestAnUnchangedRosterIsNotAnnounced(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.AddPeer(ctx, Peer{Fingerprint: "FP1", Name: "Utopia"}); err != nil {
		t.Fatal(err)
	}
	people := []RemotePerson{{ID: "u_g", Name: "Georgia"}}
	got := countAnnouncements(s)

	if err := s.ReplaceRemotePeople(ctx, "FP1", people); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 1 {
		t.Fatalf("a new roster announced %d times, want 1", len(*got))
	}
	if err := s.ReplaceRemotePeople(ctx, "FP1", people); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 1 {
		t.Fatalf("an unchanged roster announced again: %v", *got)
	}
	// A rename is a change.
	if err := s.ReplaceRemotePeople(ctx, "FP1", []RemotePerson{{ID: "u_g", Name: "G"}}); err != nil {
		t.Fatal(err)
	}
	// So is somebody leaving.
	if err := s.ReplaceRemotePeople(ctx, "FP1", nil); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 3 {
		t.Fatalf("a rename and a departure announced %d in all, want 3", len(*got))
	}
}

func TestALibraryWriteIsAnnounced(t *testing.T) {
	s := newStore(t)
	got := countAnnouncements(s)
	ctx := context.Background()
	lib, err := s.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RenameLibrary(ctx, lib.ID, "Movies"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteLibrary(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 3 {
		t.Fatalf("announced %v, want three libraries notices", *got)
	}
	for _, topic := range *got {
		if topic != ChangeLibraries {
			t.Fatalf("announced %q, want %q", topic, ChangeLibraries)
		}
	}
}
