package remote

import (
	"errors"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTest() (*Manager, *clock) {
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	m := New()
	m.now = c.now
	return m, c
}

func ms(v int64) *int64 { return &v }

func TestReportRegistersAndListsOnlyTheOwner(t *testing.T) {
	m, _ := newTest()
	if _, changed := m.Report("u_a", "tv", Report{Name: "Living room", Kind: KindDesktop}); !changed {
		t.Fatal("a new player is a change")
	}
	m.Report("u_b", "phone", Report{Name: "Phone"})

	got := m.List("u_a")
	if len(got) != 1 || got[0].ID != "tv" || got[0].Name != "Living room" || got[0].Kind != KindDesktop {
		t.Fatalf("u_a sees %+v", got)
	}
	if got := m.List("u_c"); len(got) != 0 {
		t.Fatalf("an account with no players sees %+v", got)
	}
}

// The same id under two accounts is two players. A window cannot take over
// another account's player by guessing its id.
func TestPlayersAreKeyedByAccount(t *testing.T) {
	m, _ := newTest()
	m.Report("u_a", "same", Report{Name: "A's"})
	m.Report("u_b", "same", Report{Name: "B's"})

	if err := m.Send("u_b", "same", Command{Action: ActionPause}); err != nil {
		t.Fatal(err)
	}
	got, err := m.Take("u_a", "same")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("u_b's command reached u_a's player: %+v", got)
	}
	if m.List("u_a")[0].Name != "A's" {
		t.Fatal("u_b's report renamed u_a's player")
	}
}

func TestSendToSomebodyElsesPlayerIsNotFound(t *testing.T) {
	m, _ := newTest()
	m.Report("u_a", "tv", Report{Name: "TV"})
	if err := m.Send("u_b", "tv", Command{Action: ActionStop}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if _, err := m.Take("u_b", "tv"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("take err = %v, want ErrNotFound", err)
	}
}

func TestPositionAloneIsNotAChange(t *testing.T) {
	m, _ := newTest()
	m.Report("u", "tv", Report{Name: "TV", ItemID: 5, PositionMS: 1000})
	if _, changed := m.Report("u", "tv", Report{Name: "TV", ItemID: 5, PositionMS: 6000}); changed {
		t.Fatal("a moving position announced a change")
	}
	if _, changed := m.Report("u", "tv", Report{Name: "TV", ItemID: 5, PositionMS: 6000, Paused: true}); !changed {
		t.Fatal("pausing is a change")
	}
	if _, changed := m.Report("u", "tv", Report{Name: "TV", ItemID: 6, Paused: true}); !changed {
		t.Fatal("a new item is a change")
	}
}

func TestCommandsAreTakenOnceInOrder(t *testing.T) {
	m, _ := newTest()
	m.Report("u", "tv", Report{})
	_ = m.Send("u", "tv", Command{Action: ActionPlay, ItemID: 9, Queue: []int64{9, 10}})
	_ = m.Send("u", "tv", Command{Action: ActionSeek, PositionMS: ms(60_000)})

	if p := m.List("u")[0]; p.Pending != 2 {
		t.Fatalf("pending = %d, want 2", p.Pending)
	}
	got, err := m.Take("u", "tv")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Action != ActionPlay || got[1].Action != ActionSeek {
		t.Fatalf("took %+v", got)
	}
	if got[0].SentAt == 0 {
		t.Fatal("the server did not stamp the command")
	}
	again, _ := m.Take("u", "tv")
	if len(again) != 0 {
		t.Fatalf("second take returned %+v", again)
	}
}

func TestAQuietPlayerGoesAndIsAnnounced(t *testing.T) {
	m, c := newTest()
	var gone []string
	m.OnGone = func(u string) { gone = append(gone, u) }

	m.Report("u", "tv", Report{})
	c.advance(PlayerTimeout - time.Second)
	if len(m.List("u")) != 1 {
		t.Fatal("gone before the timeout")
	}
	c.advance(2 * time.Second)
	if len(m.List("u")) != 0 {
		t.Fatal("still listed after the timeout")
	}
	if len(gone) != 1 || gone[0] != "u" {
		t.Fatalf("OnGone heard %v", gone)
	}
	if err := m.Send("u", "tv", Command{Action: ActionStop}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("send to a gone player: %v", err)
	}
}

// A command nobody collected in time is dropped, not run late.
func TestAnOldCommandIsDroppedUnrun(t *testing.T) {
	m, c := newTest()
	m.Report("u", "tv", Report{})
	_ = m.Send("u", "tv", Command{Action: ActionPlay, ItemID: 1})
	c.advance(CommandTTL + time.Second)
	m.Report("u", "tv", Report{}) // still alive, just not collecting
	got, _ := m.Take("u", "tv")
	if len(got) != 0 {
		t.Fatalf("a %v-old command was handed over: %+v", CommandTTL, got)
	}
}

func TestPendingIsBoundedKeepingTheNewest(t *testing.T) {
	m, _ := newTest()
	m.Report("u", "tv", Report{})
	for i := 1; i <= MaxPending+5; i++ {
		_ = m.Send("u", "tv", Command{Action: ActionPlay, ItemID: int64(i)})
	}
	got, _ := m.Take("u", "tv")
	if len(got) != MaxPending {
		t.Fatalf("held %d commands, want %d", len(got), MaxPending)
	}
	if got[len(got)-1].ItemID != MaxPending+5 {
		t.Fatalf("newest kept is %d", got[len(got)-1].ItemID)
	}
}

func TestRemove(t *testing.T) {
	m, _ := newTest()
	m.Report("u", "tv", Report{})
	if !m.Remove("u", "tv") {
		t.Fatal("Remove found nothing")
	}
	if len(m.List("u")) != 0 {
		t.Fatal("still listed after Remove")
	}
	if m.Remove("u", "tv") {
		t.Fatal("removed twice")
	}
}

func TestValidate(t *testing.T) {
	vol := func(v float64) *float64 { return &v }
	cases := []struct {
		name string
		c    Command
		ok   bool
	}{
		{"play", Command{Action: ActionPlay, ItemID: 1}, true},
		{"play without an item", Command{Action: ActionPlay}, false},
		{"play at a negative position", Command{Action: ActionPlay, ItemID: 1, PositionMS: ms(-1)}, false},
		{"play with too long a queue", Command{Action: ActionPlay, ItemID: 1, Queue: make([]int64, MaxQueue+1)}, false},
		{"seek", Command{Action: ActionSeek, PositionMS: ms(0)}, true},
		{"seek without a position", Command{Action: ActionSeek}, false},
		{"volume", Command{Action: ActionVolume, Volume: vol(0.5)}, true},
		{"volume above one", Command{Action: ActionVolume, Volume: vol(1.5)}, false},
		{"volume missing", Command{Action: ActionVolume}, false},
		{"pause", Command{Action: ActionPause}, true},
		{"resume", Command{Action: ActionResume}, true},
		{"stop", Command{Action: ActionStop}, true},
		{"next", Command{Action: ActionNext}, true},
		{"previous", Command{Action: ActionPrevious}, true},
		{"unknown", Command{Action: "explode"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.c.Validate()
			if tc.ok && err != nil {
				t.Fatalf("rejected: %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrBadCommand) {
				t.Fatalf("err = %v, want ErrBadCommand", err)
			}
		})
	}
}

func TestReportCleansWhatItIsTold(t *testing.T) {
	m, _ := newTest()
	p, _ := m.Report("u", "tv", Report{Name: "  \x00Den\n  ", Kind: "toaster", Volume: 7})
	if p.Name != "Den" {
		t.Fatalf("name = %q", p.Name)
	}
	if p.Kind != KindBrowser {
		t.Fatalf("kind = %q", p.Kind)
	}
	if p.Volume != 1 {
		t.Fatalf("volume = %v", p.Volume)
	}
	p, _ = m.Report("u", "tv", Report{Name: "   "})
	if p.Name != "Unnamed device" {
		t.Fatalf("empty name = %q", p.Name)
	}
}

func TestAgeIsMeasuredByTheServer(t *testing.T) {
	m, c := newTest()
	m.Report("u", "tv", Report{ItemID: 1, PositionMS: 10_000})
	c.advance(3 * time.Second)
	if age := m.List("u")[0].AgeMS; age != 3000 {
		t.Fatalf("age = %d, want 3000", age)
	}
}
