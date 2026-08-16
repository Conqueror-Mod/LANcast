package together

import (
	"testing"
	"time"
)

// atClock lets a test move time without sleeping. Every rule about who is still
// in a room is a rule about elapsed time, and a test that proved them by
// sleeping ninety seconds would be a test nobody runs.
func atClock(m *Manager) func(time.Duration) {
	base := time.Unix(1_700_000_000, 0)
	now := base
	m.now = func() time.Time { return now }
	return func(d time.Duration) { now = now.Add(d) }
}

func TestCreateMakesTheCreatorHost(t *testing.T) {
	m := New()
	atClock(m)

	s := m.Create(42, "alice", "Alice", 0)
	if s.ID == "" {
		t.Fatal("no session id")
	}
	if s.HostID != "alice" {
		t.Errorf("host = %q, want alice", s.HostID)
	}
	if len(s.Members) != 1 || !s.Members[0].Host {
		t.Errorf("members = %+v, want the creator marked host", s.Members)
	}
}

func TestJoinAndFollow(t *testing.T) {
	m := New()
	atClock(m)
	s := m.Create(42, "alice", "Alice", 0)

	if _, err := m.Join(s.ID, "bob", "Bob"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	got, err := m.Poll(s.ID, "bob")
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(got.Members) != 2 {
		t.Errorf("members = %d, want 2", len(got.Members))
	}
	// Host first: a member list in map order changes on every request.
	if !got.Members[0].Host {
		t.Error("host is not first in the member list")
	}
}

// The rule the whole feature rests on: one host drives. Two people scrubbing
// the same film is not synchronised playback, it is a fight, and the loser
// cannot tell it from a bug.
func TestOnlyTheHostMayReport(t *testing.T) {
	m := New()
	atClock(m)
	s := m.Create(42, "alice", "Alice", 0)
	if _, err := m.Join(s.ID, "bob", "Bob"); err != nil {
		t.Fatal(err)
	}

	if _, err := m.Report(s.ID, "bob", 5000, false); err != ErrNotHost {
		t.Errorf("Report by a guest = %v, want ErrNotHost", err)
	}
	got, err := m.Report(s.ID, "alice", 5000, true)
	if err != nil {
		t.Fatalf("Report by host: %v", err)
	}
	if got.PositionMS != 5000 || !got.Paused {
		t.Errorf("session = %+v, want position 5000 and paused", got)
	}
}

func TestPollRejectsANonMember(t *testing.T) {
	m := New()
	atClock(m)
	s := m.Create(42, "alice", "Alice", 0)

	if _, err := m.Poll(s.ID, "stranger"); err != ErrNotMember {
		t.Errorf("Poll by a stranger = %v, want ErrNotMember", err)
	}
}

/*
 * Nobody presses "leave".
 *
 * They close the tab, or the laptop, or walk out of range — so a room has to
 * notice on its own, or the member list becomes a record of everyone who was
 * ever there, which is the wrong answer to "who is watching this with me".
 */
func TestAQuietMemberIsDropped(t *testing.T) {
	m := New()
	advance := atClock(m)
	s := m.Create(42, "alice", "Alice", 0)
	if _, err := m.Join(s.ID, "bob", "Bob"); err != nil {
		t.Fatal(err)
	}

	// Alice keeps the room alive; Bob has gone.
	advance(2 * time.Minute)
	got, err := m.Poll(s.ID, "alice")
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(got.Members) != 1 || got.Members[0].UserID != "alice" {
		t.Errorf("members = %+v, want only the member still polling", got.Members)
	}
}

// And when the host goes quiet the room is over — the same rule as the host
// leaving, for the same reason.
func TestAQuietHostEndsTheRoom(t *testing.T) {
	m := New()
	advance := atClock(m)
	s := m.Create(42, "alice", "Alice", 0)
	if _, err := m.Join(s.ID, "bob", "Bob"); err != nil {
		t.Fatal(err)
	}

	advance(30 * time.Second)
	if _, err := m.Poll(s.ID, "bob"); err != nil {
		t.Fatalf("Poll at 30s: %v", err)
	}
	// Bob keeps polling; Alice does not.
	advance(2 * time.Minute)
	if _, err := m.Poll(s.ID, "bob"); err != ErrNotFound {
		t.Errorf("Poll after the host went quiet = %v, want ErrNotFound", err)
	}
}

/*
 * The host leaving ends the room rather than promoting somebody.
 *
 * Promotion sounds generous and is worse: the film keeps playing in three
 * houses under a driver nobody chose, and the person who started it cannot stop
 * what they began.
 */
func TestHostLeavingEndsTheRoom(t *testing.T) {
	m := New()
	atClock(m)
	s := m.Create(42, "alice", "Alice", 0)
	if _, err := m.Join(s.ID, "bob", "Bob"); err != nil {
		t.Fatal(err)
	}

	if err := m.Leave(s.ID, "alice"); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if _, err := m.Poll(s.ID, "bob"); err != ErrNotFound {
		t.Errorf("the room outlived its host: %v", err)
	}
}

func TestAGuestLeavingDoesNot(t *testing.T) {
	m := New()
	atClock(m)
	s := m.Create(42, "alice", "Alice", 0)
	if _, err := m.Join(s.ID, "bob", "Bob"); err != nil {
		t.Fatal(err)
	}

	if err := m.Leave(s.ID, "bob"); err != nil {
		t.Fatal(err)
	}
	got, err := m.Poll(s.ID, "alice")
	if err != nil {
		t.Fatalf("the room died with its guest: %v", err)
	}
	if len(got.Members) != 1 {
		t.Errorf("members = %+v, want just the host", got.Members)
	}
}

// Rejoining is a refresh, a dropped connection or a second tab. None of them
// should produce two of the same person.
func TestRejoiningDoesNotDuplicate(t *testing.T) {
	m := New()
	atClock(m)
	s := m.Create(42, "alice", "Alice", 0)

	for i := 0; i < 3; i++ {
		if _, err := m.Join(s.ID, "bob", "Bob"); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := m.Poll(s.ID, "alice")
	if len(got.Members) != 2 {
		t.Errorf("members = %d, want 2 after three joins by the same person", len(got.Members))
	}
}

// The host rejoining must not lose the host flag: a refresh mid-film would
// otherwise leave a room with no driver and no way to get one.
func TestHostKeepsHostOnRejoin(t *testing.T) {
	m := New()
	atClock(m)
	s := m.Create(42, "alice", "Alice", 0)

	got, err := m.Join(s.ID, "alice", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if got.HostID != "alice" || !got.Members[0].Host {
		t.Errorf("session = %+v, want alice still hosting after a rejoin", got)
	}
	if _, err := m.Report(s.ID, "alice", 1000, false); err != nil {
		t.Errorf("the host could not drive after rejoining: %v", err)
	}
}

func TestListIsNewestFirstAndStable(t *testing.T) {
	m := New()
	advance := atClock(m)

	first := m.Create(1, "alice", "Alice", 0)
	advance(time.Second)
	second := m.Create(2, "bob", "Bob", 0)

	got := m.List()
	if len(got) != 2 {
		t.Fatalf("rooms = %d, want 2", len(got))
	}
	if got[0].ID != second.ID || got[1].ID != first.ID {
		t.Errorf("order = %v, want newest first", []string{got[0].ID, got[1].ID})
	}
}

func TestUnknownSession(t *testing.T) {
	m := New()
	atClock(m)
	if _, err := m.Poll("nope", "alice"); err != ErrNotFound {
		t.Errorf("Poll on a missing room = %v, want ErrNotFound", err)
	}
	if err := m.Leave("nope", "alice"); err != ErrNotFound {
		t.Errorf("Leave on a missing room = %v, want ErrNotFound", err)
	}
}

// Ids are what somebody reads across a room, and what an idle stranger would
// otherwise guess. Two rooms must never collide.
func TestIDsAreDistinct(t *testing.T) {
	m := New()
	atClock(m)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := m.Create(1, "alice", "Alice", 0).ID
		if seen[id] {
			t.Fatalf("duplicate session id %q", id)
		}
		seen[id] = true
		_ = m.Leave(id, "alice")
	}
}
