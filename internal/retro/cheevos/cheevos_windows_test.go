//go:build windows

package cheevos

import (
	"context"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"lancast/internal/retro/libretro"
)

/*
 * Against the real rcheevos.dll, built by third_party/rcheevos/build.sh.
 * Opt in with RCHEEVOS_DLL=<path to rcheevos.dll>, as the real-core tests
 * opt in with RETRO_CORES: CI has no DLL.
 *
 * A fake core gives an NES's 2 KB of RAM, and a fake RetroAchievements
 * answers with a one-achievement set whose condition is "byte 1 is 3". Writing
 * that byte must unlock it and send a softcore award: the whole path, from
 * memory through rcheevos to the request the server would forward.
 */

type fakeCore struct{ ram []byte }

func (c *fakeCore) SystemInfo() libretro.SystemInfo             { return libretro.SystemInfo{} }
func (c *fakeCore) Init(libretro.Frontend) error                { return nil }
func (c *fakeCore) LoadGame(string, []byte) error               { return nil }
func (c *fakeCore) AVInfo() libretro.AVInfo                     { return libretro.AVInfo{} }
func (c *fakeCore) SetControllerPortDevice(port, device uint32) {}
func (c *fakeCore) Reset()                                      {}
func (c *fakeCore) Run()                                        {}
func (c *fakeCore) SerializeSize() int                          { return 0 }
func (c *fakeCore) Serialize([]byte) bool                       { return false }
func (c *fakeCore) Unserialize([]byte) bool                     { return false }
func (c *fakeCore) UnloadGame()                                 {}
func (c *fakeCore) ContextReset()                               {}
func (c *fakeCore) ContextDestroy()                             {}
func (c *fakeCore) Close()                                      {}
func (c *fakeCore) Memory(id uint32) []byte {
	if id == 2 { // RETRO_MEMORY_SYSTEM_RAM
		return c.ram
	}
	return nil
}

const oneAchievementSet = `{"Success":true,"GameId":1234,"Title":"Sample Game","ConsoleId":7,` +
	`"ImageIconUrl":"https://media.retroachievements.org/Images/112233.png",` +
	`"RichPresenceGameId":1234,"RichPresencePatch":"","Sets":[{` +
	`"AchievementSetId":1111,"GameId":1234,"Title":null,"Type":"core",` +
	`"ImageIconUrl":"https://media.retroachievements.org/Images/112233.png",` +
	`"Achievements":[{"ID":5501,"Title":"Ach1","Description":"Desc1","Flags":3,"Points":5,` +
	`"MemAddr":"0xH0001=3","Author":"User1","BadgeName":"00234","Created":1367266583,"Modified":1376929305}],` +
	`"Leaderboards":[]}]}`

type fakeRA struct {
	mu   sync.Mutex
	seen []url.Values
}

func (f *fakeRA) send(_ context.Context, body, clause string) (int, []byte, error) {
	v, _ := url.ParseQuery(body)
	f.mu.Lock()
	f.seen = append(f.seen, v)
	f.mu.Unlock()
	switch v.Get("r") {
	case "login2":
		return 200, []byte(`{"Success":true,"User":"Player1","Token":"lancast","Score":0,"SoftcoreScore":0}`), nil
	case "achievementsets":
		return 200, []byte(oneAchievementSet), nil
	case "awardachievement":
		return 200, []byte(`{"Success":true,"Score":5,"SoftcoreScore":5,"AchievementID":5501,"AchievementsRemaining":0}`), nil
	}
	return 200, []byte(`{"Success":true}`), nil
}

func (f *fakeRA) requests(kind string) []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []url.Values
	for _, v := range f.seen {
		if v.Get("r") == kind {
			out = append(out, v)
		}
	}
	return out
}

func TestRealRcheevosUnlocksFromMemory(t *testing.T) {
	path := os.Getenv("RCHEEVOS_DLL")
	if path == "" {
		t.Skip("set RCHEEVOS_DLL to the rcheevos.dll third_party/rcheevos/build.sh made")
	}
	if err := Load(path); err != nil {
		t.Fatal(err)
	}
	ra := &fakeRA{}
	var mu sync.Mutex
	var events []Event
	rt := New(ra.send, func(e Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}, nil)
	saw := func(kind string) *Event {
		mu.Lock()
		defer mu.Unlock()
		for i := range events {
			if events[i].Kind == kind {
				return &events[i]
			}
		}
		return nil
	}
	frames := func(until string, n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			rt.Frame()
			if until != "" && saw(until) != nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		if until != "" {
			t.Fatalf("no %q event; events: %+v; requests: %v", until, events, ra.seen)
		}
	}

	core := &fakeCore{ram: make([]byte, 0x800)}
	game := append([]byte("NES\x1a"), make([]byte, 16*1024+12)...)
	if err := rt.Start(core, Console("nes"), `C:\games\Sample.nes`, game); err != nil {
		t.Fatal(err)
	}
	defer rt.Stop()
	frames("ready", 400)
	if e := saw("ready"); e.Title != "Sample Game" || e.Total != 1 || e.Unlocked != 0 {
		t.Errorf("ready: %+v", *e)
	}
	for _, v := range ra.seen {
		if v.Has("p") || (v.Get("h") != "" && v.Get("h") != "0") {
			t.Errorf("rcheevos sent %v", v)
		}
	}
	if login := ra.requests("login2"); len(login) != 1 || login[0].Get("u") != "lancast" || login[0].Get("t") != "lancast" {
		t.Errorf("sign-in: %v", login)
	}

	// False first, as rcheevos requires, then true.
	frames("", 10)
	if saw("unlocked") != nil {
		t.Fatal("unlocked before the condition was met")
	}
	core.ram[1] = 3
	frames("unlocked", 100)
	if e := saw("unlocked"); e.ID != 5501 || e.Points != 5 || !strings.HasPrefix(e.Badge, "/api/retroachievements/media/Badge/") {
		t.Errorf("unlocked: %+v", *e)
	}
	for i := 0; i < 100 && len(ra.requests("awardachievement")) == 0; i++ {
		rt.Frame()
		time.Sleep(5 * time.Millisecond)
	}
	award := ra.requests("awardachievement")
	if len(award) != 1 || award[0].Get("a") != "5501" || award[0].Get("h") != "0" || award[0].Get("u") != "Player1" {
		t.Fatalf("award: %v", award)
	}

	// The menu's list, and progress for a save state.
	sum := rt.List()
	if sum.Title != "Sample Game" || sum.Total != 1 || sum.Unlocked != 1 || len(sum.Achievements) != 1 ||
		sum.Achievements[0].State != "unlocked" {
		t.Errorf("list: %+v", sum)
	}
	if p := rt.Progress(); len(p) == 0 {
		t.Error("no progress to keep with a state")
	} else {
		rt.SetProgress(p)
	}

	/*
	 * Stopping now, with the set just completed and no frame since the
	 * award's answer, is the moment rcheevos v12.5 crashed the process in
	 * unload (a pending mastery event read a game it had already cleared).
	 * The shim runs one frame first, which also means the person hears it.
	 */
	rt.Stop()
	if saw("mastered") == nil {
		t.Error("completing the set was never announced")
	}
}
