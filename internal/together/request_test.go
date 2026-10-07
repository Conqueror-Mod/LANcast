package together

import (
	"errors"
	"testing"
	"time"
)

const (
	fp     = "FP-GEORGIA"
	person = "u_g1"
)

// open is the common start: Alice hosting item 42, Georgia asking to join.
func open(t *testing.T) (*Manager, func(time.Duration), Session, Request) {
	t.Helper()
	m := New()
	advance := atClock(m)
	room := m.Create(42, "alice", "Alice", 0)
	req := m.Ask("alice", fp, person, "Georgia", "Utopia")
	if req.State != RequestPending {
		t.Fatalf("a first ask is %q, want pending", req.State)
	}
	return m, advance, room, req
}

func TestAcceptPutsTheAskerInTheRoom(t *testing.T) {
	m, _, room, req := open(t)

	s, err := m.Accept(req.ID, "alice", room.ID)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	var guest *Member
	for i := range s.Members {
		if s.Members[i].UserID == RemoteID(fp, person) {
			guest = &s.Members[i]
		}
	}
	if guest == nil {
		t.Fatalf("members = %+v, want Georgia", s.Members)
	}
	if guest.Peer != fp || guest.Server != "Utopia" || guest.Name != "Georgia" || guest.Host {
		t.Errorf("guest = %+v, want Georgia from Utopia, not host", *guest)
	}
	if got := m.Status(req.ID, fp, person); got.State != RequestAccepted || got.RoomID != room.ID {
		t.Errorf("asker sees %+v, want accepted into %s", got, room.ID)
	}
}

// Being paired with the host's server is not an invitation to every room on
// it. Only an accepted request admits a remote member.
func TestJoinRemoteRefusesSomebodyNotAdmitted(t *testing.T) {
	m := New()
	atClock(m)
	room := m.Create(42, "alice", "Alice", 0)

	if _, err := m.JoinRemote(room.ID, fp, person); !errors.Is(err, ErrNotMember) {
		t.Errorf("JoinRemote without an accepted request = %v, want ErrNotMember", err)
	}
}

func TestJoinRemoteLetsAnAdmittedMemberBackIn(t *testing.T) {
	m, _, room, req := open(t)
	if _, err := m.Accept(req.ID, "alice", room.ID); err != nil {
		t.Fatal(err)
	}
	s, err := m.JoinRemote(room.ID, fp, person)
	if err != nil {
		t.Fatalf("rejoin: %v", err)
	}
	if len(s.Members) != 2 {
		t.Errorf("members = %d after a rejoin, want 2 (no duplicate)", len(s.Members))
	}
}

// The boundary belongs to the host: Accept pressed as the countdown reaches
// zero is a yes.
func TestAcceptAtExactlyTheTimeoutIsInTime(t *testing.T) {
	m, advance, room, req := open(t)
	advance(requestTimeout)

	if _, err := m.Accept(req.ID, "alice", room.ID); err != nil {
		t.Errorf("Accept at exactly %v: %v, want accepted", requestTimeout, err)
	}
}

// Silence is a no. A host who is asleep has not agreed to anything.
func TestAnUnansweredRequestIsDeclined(t *testing.T) {
	m, advance, room, req := open(t)
	advance(requestTimeout + time.Millisecond)

	if _, err := m.Accept(req.ID, "alice", room.ID); !errors.Is(err, ErrRequestClosed) {
		t.Errorf("Accept after the timeout = %v, want ErrRequestClosed", err)
	}
	if got := m.Status(req.ID, fp, person); got.State != "not_now" {
		t.Errorf("asker sees %q, want not_now", got.State)
	}
	if p := m.Pending("alice"); len(p) != 0 {
		t.Errorf("host still has %d prompts after the timeout", len(p))
	}
}

// A no, a timeout and a cooldown all read as the same "not now", and the host
// is not shown a prompt during the cooldown.
func TestADeclineStartsACooldownTheAskerCannotSee(t *testing.T) {
	m, advance, _, req := open(t)
	if err := m.Decline(req.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if got := m.Status(req.ID, fp, person); got.State != "not_now" {
		t.Errorf("after a decline the asker sees %q, want not_now", got.State)
	}

	advance(requestCooldown - time.Second)
	again := m.Ask("alice", fp, person, "Georgia", "Utopia")
	if got := m.Status(again.ID, fp, person); got.State != "not_now" {
		t.Errorf("asking inside the cooldown reads %q, want not_now", got.State)
	}
	if p := m.Pending("alice"); len(p) != 0 {
		t.Errorf("host was prompted %d times inside the cooldown", len(p))
	}

	advance(time.Second)
	after := m.Ask("alice", fp, person, "Georgia", "Utopia")
	if after.State != RequestPending || len(m.Pending("alice")) != 1 {
		t.Errorf("after the cooldown the ask is %q with %d prompts, want pending and 1",
			after.State, len(m.Pending("alice")))
	}
}

func TestATimeoutStartsTheCooldownToo(t *testing.T) {
	m, advance, _, _ := open(t)
	advance(requestTimeout + time.Second)

	again := m.Ask("alice", fp, person, "Georgia", "Utopia")
	if again.State != RequestDeclined || len(m.Pending("alice")) != 0 {
		t.Errorf("asking straight after a timeout gave %q with %d prompts, want declined and none",
			again.State, len(m.Pending("alice")))
	}
}

// Asking again replaces the open request rather than stacking a second prompt.
func TestAskingAgainReplacesTheOpenRequest(t *testing.T) {
	m, advance, _, first := open(t)
	advance(5 * time.Second)
	second := m.Ask("alice", fp, person, "Georgia", "Utopia")

	p := m.Pending("alice")
	if len(p) != 1 || p[0].ID != second.ID {
		t.Fatalf("pending = %+v, want only the second request", p)
	}
	if got := m.Status(first.ID, fp, person); got.State != "not_now" {
		t.Errorf("the replaced request reads %q, want not_now", got.State)
	}
}

// Different askers are different prompts, oldest first.
func TestPendingListsEachAskerOldestFirst(t *testing.T) {
	m, advance, _, first := open(t)
	advance(time.Second)
	second := m.Ask("alice", fp, "u_g2", "Sam", "Utopia")

	p := m.Pending("alice")
	if len(p) != 2 || p[0].ID != first.ID || p[1].ID != second.ID {
		t.Errorf("pending = %+v, want Georgia then Sam", p)
	}
	if len(m.Pending("bob")) != 0 {
		t.Error("another host was shown Alice's prompts")
	}
}

// Only the asker may read the answer.
func TestStatusIsOnlyForTheAsker(t *testing.T) {
	m, _, room, req := open(t)
	if _, err := m.Accept(req.ID, "alice", room.ID); err != nil {
		t.Fatal(err)
	}
	if got := m.Status(req.ID, fp, "u_someone_else"); got.State != "not_now" || got.RoomID != "" {
		t.Errorf("another person reading the request got %+v", got)
	}
	if got := m.Status(req.ID, "FP-OTHER", person); got.State != "not_now" || got.RoomID != "" {
		t.Errorf("another server reading the request got %+v", got)
	}
}

func TestAHostCannotAnswerAnotherHostsRequest(t *testing.T) {
	m, _, room, req := open(t)
	if _, err := m.Accept(req.ID, "bob", room.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Bob accepting Alice's request = %v, want ErrNotFound", err)
	}
	if err := m.Decline(req.ID, "bob"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Bob declining Alice's request = %v, want ErrNotFound", err)
	}
}

// A host cannot admit somebody into a room another person is driving.
func TestAcceptIntoSomebodyElsesRoomIsRefused(t *testing.T) {
	m, _, _, req := open(t)
	bobs := m.Create(7, "bob", "Bob", 0)

	if _, err := m.Accept(req.ID, "alice", bobs.ID); !errors.Is(err, ErrNotHost) {
		t.Errorf("Accept into Bob's room = %v, want ErrNotHost", err)
	}
	if got := m.Status(req.ID, fp, person); got.State != RequestPending {
		t.Errorf("a refused accept left the request %q, want still pending", got.State)
	}
}

func TestARequestIsAnsweredOnce(t *testing.T) {
	m, _, room, req := open(t)
	if _, err := m.Accept(req.ID, "alice", room.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Accept(req.ID, "alice", room.ID); !errors.Is(err, ErrRequestClosed) {
		t.Errorf("second Accept = %v, want ErrRequestClosed", err)
	}
	if err := m.Decline(req.ID, "alice"); !errors.Is(err, ErrRequestClosed) {
		t.Errorf("Decline after Accept = %v, want ErrRequestClosed", err)
	}
}

// The answer stays readable long enough for a polling asker to learn it, and
// is then cleared, after which it reads as not now.
func TestAnAnsweredRequestIsKeptForTheAskerThenCleared(t *testing.T) {
	m, advance, room, req := open(t)
	if _, err := m.Accept(req.ID, "alice", room.ID); err != nil {
		t.Fatal(err)
	}
	advance(requestRetention)
	if got := m.Status(req.ID, fp, person); got.State != RequestAccepted {
		t.Errorf("at the retention boundary the asker sees %q, want accepted", got.State)
	}
	advance(time.Second)
	if got := m.Status(req.ID, fp, person); got.State != "not_now" {
		t.Errorf("after retention the asker sees %q, want not_now", got.State)
	}
}

// Playing is the whole of a guest's right to the room's film, and it ends
// with the room, the item or the guest's attention.
func TestPlayingFollowsTheRoom(t *testing.T) {
	m, advance, room, req := open(t)
	if m.Playing(fp, person, 42) {
		t.Fatal("an asker who has not been accepted may play the film")
	}
	if _, err := m.Accept(req.ID, "alice", room.ID); err != nil {
		t.Fatal(err)
	}
	if !m.Playing(fp, person, 42) {
		t.Error("an admitted guest may not play the room's film")
	}
	if m.Playing(fp, person, 43) {
		t.Error("an admitted guest may play a film the room is not playing")
	}
	if m.Playing("FP-OTHER", person, 42) {
		t.Error("the same person id from another server may play the film")
	}

	// The guest stops polling and the host keeps reporting: the sweep drops
	// the guest and the permission goes with them.
	advance(idleTimeout / 2)
	if _, err := m.Report(room.ID, "alice", 1000, false); err != nil {
		t.Fatal(err)
	}
	advance(idleTimeout/2 + time.Second)
	if _, err := m.Report(room.ID, "alice", 2000, false); err != nil {
		t.Fatal(err)
	}
	if m.Playing(fp, person, 42) {
		t.Error("a guest the sweep dropped may still play the film")
	}
}

func TestPlayingEndsWhenTheHostLeaves(t *testing.T) {
	m, _, room, req := open(t)
	if _, err := m.Accept(req.ID, "alice", room.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Leave(room.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if m.Playing(fp, person, 42) {
		t.Error("a guest may play the film after the room ended")
	}
}

// A remote member polling exactly at the sweep boundary is still here: the
// record-before-sweep rule holds for guests as it does for accounts.
func TestARemoteMemberPollingOnTimeIsKept(t *testing.T) {
	m, advance, room, req := open(t)
	if _, err := m.Accept(req.ID, "alice", room.ID); err != nil {
		t.Fatal(err)
	}
	advance(idleTimeout)
	if _, err := m.Report(room.ID, "alice", 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Poll(room.ID, RemoteID(fp, person)); err != nil {
		t.Errorf("guest polling at the boundary: %v, want kept", err)
	}
}
