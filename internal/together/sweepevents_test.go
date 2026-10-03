package together

import (
	"testing"
	"time"
)

/*
 * The sweep says what it did.
 *
 * Nobody presses "leave": they close the laptop. The sweep drops them, and a
 * room whose host went quiet ends, inside whichever unrelated call arrives
 * next. Those were the two events nobody could see.
 */
func TestSweepReportsTimeoutsAndEndedRooms(t *testing.T) {
	m := New()
	tick := atClock(m)
	var got []Event
	m.OnSweep = func(e Event) { got = append(got, e) }

	room := m.Create(42, "alice", "Alice", 0)
	if _, err := m.Join(room.ID, "bob", "Bob"); err != nil {
		t.Fatal(err)
	}

	// Alice keeps polling; Bob goes quiet.
	tick(60 * time.Second)
	if _, err := m.Poll(room.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	tick(60 * time.Second)
	if _, err := m.Poll(room.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != EventMemberTimedOut || got[0].Name != "Bob" || got[0].ItemID != 42 {
		t.Fatalf("after Bob went quiet, events = %+v; want one timeout for Bob", got)
	}

	// Now the host goes quiet too; the next call anywhere ends the room.
	tick(2 * idleTimeout)
	m.List()
	last := got[len(got)-1]
	if last.Kind != EventRoomEnded || last.RoomID != room.ID {
		t.Fatalf("after the host went quiet, last event = %+v; want the room ended", last)
	}
}

// Without a listener the sweep is unchanged: OnSweep is optional.
func TestSweepWithoutAListener(t *testing.T) {
	m := New()
	tick := atClock(m)
	m.Create(1, "alice", "Alice", 0)
	tick(2 * idleTimeout)
	if n := len(m.List()); n != 0 {
		t.Fatalf("%d rooms survived their host going quiet", n)
	}
}

// LeaveRoom tells a person leaving apart from a room closing.
func TestLeaveRoomSaysWhetherTheRoomEnded(t *testing.T) {
	m := New()
	atClock(m)
	room := m.Create(7, "alice", "Alice", 0)
	_, _ = m.Join(room.ID, "bob", "Bob")

	if ended, err := m.LeaveRoom(room.ID, "bob"); err != nil || ended {
		t.Fatalf("a member leaving: ended=%v err=%v; want the room to stay open", ended, err)
	}
	if ended, err := m.LeaveRoom(room.ID, "alice"); err != nil || !ended {
		t.Fatalf("the host leaving: ended=%v err=%v; want the room closed", ended, err)
	}
}
