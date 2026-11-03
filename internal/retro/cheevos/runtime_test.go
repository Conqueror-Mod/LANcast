package cheevos

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"lancast/internal/retro/libretro"
)

/*
 * The runtime's own rules, with no DLL: a fake engine that records calls and
 * calls back into the runtime as the shim would. The real DLL is
 * TestRealRcheevosUnlocksFromMemory's.
 */

type fakeEngine struct {
	h        Handler
	calls    []string
	game     GameInfo
	restored [][]byte
	// respond, if set, runs inside Respond: what rcheevos would do with that
	// answer, calling back into the handler.
	respond func(id uint32, body []byte, status int)
}

func (f *fakeEngine) log(s string)            { f.calls = append(f.calls, s) }
func (f *fakeEngine) Close()                  { f.log("close") }
func (f *fakeEngine) UserAgentClause() string { return "rcheevos/12.5" }
func (f *fakeEngine) Respond(id uint32, body []byte, status int) {
	f.log("respond")
	if f.respond != nil {
		f.respond(id, body, status)
	}
}
func (f *fakeEngine) Login(u, tok string) error { f.log("login " + u + " " + tok); return nil }
func (f *fakeEngine) SetMemory(Console, []libretro.MemoryDescriptor, []byte, []byte) bool {
	f.log("memory")
	return true
}
func (f *fakeEngine) LoadGame(Console, string, []byte) { f.log("load") }
func (f *fakeEngine) UnloadGame()                      { f.log("unload") }
func (f *fakeEngine) DoFrame()                         { f.log("frame") }
func (f *fakeEngine) Idle()                            { f.log("idle") }
func (f *fakeEngine) Reset()                           { f.log("reset") }
func (f *fakeEngine) Progress() ([]byte, error)        { return []byte("P"), nil }
func (f *fakeEngine) RestoreProgress(b []byte) error {
	f.restored = append(f.restored, b)
	return nil
}
func (f *fakeEngine) Game() (GameInfo, bool) { return f.game, f.game.ID != 0 }
func (f *fakeEngine) Achievements() []Listed {
	return []Listed{
		{ID: 101000001, Title: "Warning: Unknown Emulator", Description: "Hardcore unlocks cannot be earned using this emulator.", State: Unlocked},
		{ID: 1, Title: "A", Badge: "11", State: Unlocked},
		{ID: 2, Title: "B", Badge: "22", State: Locked, Progress: "3/10"},
	}
}
func (f *fakeEngine) RichPresence() string { return "Level 3" }
func (f *fakeEngine) SetHardcore(on bool) {
	if on {
		f.log("hardcore on")
	} else {
		f.log("hardcore off")
	}
}
func (f *fakeEngine) Hardcore() bool { return f.count("hardcore on") > f.count("hardcore off") }

func (f *fakeEngine) count(call string) int {
	n := 0
	for _, c := range f.calls {
		if c == call {
			n++
		}
	}
	return n
}

type runtimeRig struct {
	rt      *Runtime
	eng     *fakeEngine
	mu      sync.Mutex
	notices []Notice
	sent    chan string
}

func newRig(t *testing.T, send Sender) *runtimeRig {
	t.Helper()
	g := &runtimeRig{eng: &fakeEngine{game: GameInfo{ID: 9, Title: "Sample", Total: 2, Unlocked: 1}}, sent: make(chan string, 8)}
	if send == nil {
		send = func(_ context.Context, body, clause string) (int, []byte, error) {
			g.sent <- body + " " + clause
			return 200, []byte(`{"Success":true}`), nil
		}
	}
	g.rt = newRuntime(func(h Handler) (engine, error) { g.eng.h = h; return g.eng, nil }, send, func(n Notice) {
		g.mu.Lock()
		g.notices = append(g.notices, n)
		g.mu.Unlock()
	}, nil)
	t.Cleanup(g.rt.Stop)
	return g
}

func (g *runtimeRig) kinds() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var k []string
	for _, n := range g.notices {
		k = append(k, n.Kind)
	}
	return strings.Join(k, ",")
}

// frameUntil runs frames until respond has been called n times.
func (g *runtimeRig) frameUntil(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for g.eng.count("respond") < n {
		if time.Now().After(deadline) {
			t.Fatalf("respond called %d times, want %d; calls %v", g.eng.count("respond"), n, g.eng.calls)
		}
		g.rt.Frame()
		time.Sleep(time.Millisecond)
	}
}

func TestNoConsoleStartsNothing(t *testing.T) {
	g := newRig(t, nil)
	if err := g.rt.Start(0, Memory{}, "", nil); err != nil {
		t.Fatal(err)
	}
	if g.eng.h != nil || g.kinds() != "none" {
		t.Errorf("opened %v, notices %s", g.eng.h != nil, g.kinds())
	}
	g.rt.Frame() // and does nothing
}

func TestAMissingDLLIsAnError(t *testing.T) {
	rt := newRuntime(func(Handler) (engine, error) { return nil, errors.New("no dll") }, nil, nil, nil)
	if err := rt.Start(NES, Memory{}, "", nil); err == nil {
		t.Fatal("no error")
	}
	rt.Frame()
	rt.Stop()
}

// The player signs in as the placeholder, and answers reach rcheevos only in
// Frame: never on the goroutine that fetched them.
func TestAnswersAreHandedOverOnTheCoreThread(t *testing.T) {
	g := newRig(t, nil)
	if err := g.rt.Start(NES, Memory{SystemRAM: make([]byte, 8)}, "a.nes", nil); err != nil {
		t.Fatal(err)
	}
	if strings.Join(g.eng.calls, ",") != "memory,login lancast lancast,load" {
		t.Fatalf("start: %v", g.eng.calls)
	}
	g.eng.h.Request(Request{ID: 4, PostData: "r=login2&u=lancast"})
	if got := <-g.sent; got != "r=login2&u=lancast rcheevos/12.5" {
		t.Errorf("sent %q", got)
	}
	time.Sleep(20 * time.Millisecond)
	if g.eng.count("respond") != 0 {
		t.Fatal("answered outside a frame")
	}
	g.frameUntil(t, 1)
}

// A request that never got an answer is retried by rcheevos; an answer too
// big to be RetroAchievements' is not.
func TestFailedSendsAreRetryable(t *testing.T) {
	var statuses []int
	var mu sync.Mutex
	g := newRig(t, func(_ context.Context, body, _ string) (int, []byte, error) {
		if body == "big" {
			return 200, make([]byte, maxAnswer+1), nil
		}
		return 0, nil, errors.New("server unreachable")
	})
	g.eng.respond = func(_ uint32, _ []byte, status int) { mu.Lock(); statuses = append(statuses, status); mu.Unlock() }
	_ = g.rt.Start(NES, Memory{}, "", nil)
	g.eng.h.Request(Request{ID: 1, PostData: "r=ping"})
	g.frameUntil(t, 1)
	g.eng.h.Request(Request{ID: 2, PostData: "big"})
	g.frameUntil(t, 2)
	mu.Lock()
	defer mu.Unlock()
	if len(statuses) != 2 || statuses[0] != StatusRetry || statuses[1] != StatusFail {
		t.Errorf("statuses %v", statuses)
	}
}

// Continue loads its state before the set has arrived; the progress waits
// for it, and "ready" carries the game's counts.
func TestEarlyProgressWaitsForTheSet(t *testing.T) {
	g := newRig(t, nil)
	_ = g.rt.Start(NES, Memory{}, "", nil)
	g.rt.SetProgress([]byte("early"))
	if g.rt.Progress() != nil {
		t.Error("progress before the set")
	}
	// The load finishes inside the answer, as rcheevos does it.
	g.eng.respond = func(uint32, []byte, int) { g.eng.h.Done(OpLoadGame, OK, "") }
	g.eng.h.Request(Request{ID: 1, PostData: "r=achievementsets"})
	g.frameUntil(t, 1)
	if len(g.eng.restored) != 1 || string(g.eng.restored[0]) != "early" {
		t.Errorf("restored %q", g.eng.restored)
	}
	g.mu.Lock()
	ready := g.notices[len(g.notices)-1]
	g.mu.Unlock()
	if ready.Kind != "ready" || ready.Title != "Sample" || ready.Total != 2 || ready.Unlocked != 1 {
		t.Errorf("ready %+v", ready)
	}
	g.rt.Frame()
	if g.eng.count("frame") == 0 {
		t.Error("no frame evaluated once ready")
	}
	if string(g.rt.Progress()) != "P" {
		t.Error("no progress once ready")
	}
}

// Earning off is a failed sign-in: said once, and nothing is held for a set
// that will not come.
func TestASignInRefusedIsOff(t *testing.T) {
	g := newRig(t, nil)
	_ = g.rt.Start(NES, Memory{}, "", nil)
	g.rt.SetProgress([]byte("early"))
	g.eng.h.Done(OpLogin, InvalidCredentials, "Earning is off for this account.")
	g.eng.h.Done(OpLoadGame, LoginRequired, "Login required")
	if g.kinds() != "off" {
		t.Fatalf("notices %s", g.kinds())
	}
	if g.notices[0].Text != "Earning is off for this account." || g.rt.hasEarly {
		t.Errorf("off %+v, still holding %v", g.notices[0], g.rt.hasEarly)
	}
}

func TestEventsBecomeNotices(t *testing.T) {
	g := newRig(t, nil)
	_ = g.rt.Start(NES, Memory{}, "", nil)
	h := g.eng.h
	h.Event(Event{Type: EventAchievementTriggered, ID: 101000001, Title: "Warning: Unknown Emulator"}) // not an unlock
	h.Event(Event{Type: EventAchievementTriggered, ID: 5, Title: "First", Points: 10, Badge: "00234"})
	h.Event(Event{Type: EventProgressIndicatorUpdate, Title: "Coins", Text: "3/10"})
	h.Event(Event{Type: EventProgressIndicatorHide})
	h.Event(Event{Type: EventGameCompleted})
	h.Event(Event{Type: EventDisconnected})
	h.Event(Event{Type: EventReconnected})
	h.Event(Event{Type: EventServerError, Title: "award", Text: "Unknown achievement"})
	h.Event(Event{Type: EventTrackerShow}) // not shown yet
	if got := g.kinds(); got != "unlocked,progress,progress-hide,mastered,offline,online,problem" {
		t.Fatalf("notices %s", got)
	}
	if n := g.notices[0]; n.Badge != "/api/retroachievements/media/Badge/00234.png" || n.Points != 10 {
		t.Errorf("unlocked %+v", n)
	}
	if g.notices[1].Progress != "3/10" || g.notices[6].Text != "Unknown achievement" {
		t.Errorf("progress %+v, problem %+v", g.notices[1], g.notices[6])
	}
	// A badge name that is not a plain name is dropped, not sent to the page.
	if badgeURL("../x", true) != "" {
		t.Error("an odd badge name became a URL")
	}
}

func TestTheListForTheMenu(t *testing.T) {
	g := newRig(t, nil)
	if s := g.rt.List(); s.Achievements == nil || len(s.Achievements) != 0 {
		t.Errorf("before ready: %+v", s)
	}
	_ = g.rt.Start(NES, Memory{}, "", nil)
	g.eng.h.Done(OpLoadGame, OK, "")
	g.rt.Frame()
	s := g.rt.List()
	// RetroAchievements' warning is a note on the page, not an achievement.
	if s.GameID != 9 || s.RichPresence != "Level 3" || len(s.Achievements) != 2 ||
		s.Warning != "Hardcore unlocks cannot be earned using this emulator." {
		t.Fatalf("list %+v", s)
	}
	a, b := s.Achievements[0], s.Achievements[1]
	if a.State != "unlocked" || a.Badge != "/api/retroachievements/media/Badge/11.png" ||
		b.State != "locked" || b.Badge != "/api/retroachievements/media/Badge/22_lock.png" || b.Progress != "3/10" {
		t.Errorf("achievements %+v", s.Achievements)
	}
}

// Stop unloads before it closes, and is safe twice and before any Start.
func TestStopUnloadsThenCloses(t *testing.T) {
	g := newRig(t, nil)
	_ = g.rt.Start(NES, Memory{}, "", nil)
	g.rt.Stop()
	calls := strings.Join(g.eng.calls, ",")
	if !strings.HasSuffix(calls, "unload,close") {
		t.Errorf("calls %s", calls)
	}
	g.rt.Stop()
	if g.eng.count("close") != 1 {
		t.Error("closed twice")
	}
	newRuntime(nil, nil, nil, nil).Stop()
}

// Casual unless asked; asked before Start, the set loads in hardcore, and
// later the switch goes straight to rcheevos. A reset request is the
// session's to act on, so it is no notice.
func TestHardcoreFollowsTheSession(t *testing.T) {
	g := newRig(t, nil)
	_ = g.rt.Start(NES, Memory{}, "", nil)
	if g.eng.count("hardcore on") != 0 {
		t.Fatal("hardcore without being asked")
	}
	g.rt.SetHardcore(true)
	if !g.eng.Hardcore() {
		t.Error("hardcore on did not reach rcheevos")
	}
	g.eng.h.Event(Event{Type: EventReset})
	if g.kinds() != "" {
		t.Errorf("notices %s", g.kinds())
	}
	g.rt.SetHardcore(false)
	if g.eng.Hardcore() {
		t.Error("hardcore off did not reach rcheevos")
	}

	h := newRig(t, nil)
	h.rt.SetHardcore(true)
	_ = h.rt.Start(NES, Memory{}, "", nil)
	if strings.Join(h.eng.calls, ",") != "memory,hardcore on,login lancast lancast,load" {
		t.Errorf("start: %v", h.eng.calls)
	}
}

func TestConsoleFor(t *testing.T) {
	for platform, want := range map[string]Console{"nes": NES, "gba": GameBoyAdv, "genesis": MegaDrive, "ps1": PlayStation, "dos": 0} {
		if got := ConsoleFor(platform); got != want {
			t.Errorf("%s = %d, want %d", platform, got, want)
		}
	}
}
