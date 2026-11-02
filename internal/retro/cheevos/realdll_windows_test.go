//go:build windows

package cheevos

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

/*
 * The runtime against the real DLL, built by buildDLL as the binding's own
 * tests build it.
 *
 * An NES's 2 KiB of RAM, and a fake RetroAchievements that answers with a
 * one-achievement set whose condition is "byte 1 is 3". Writing that byte
 * must unlock it and send a casual award: the whole path, from memory
 * through rcheevos to the request the server would forward.
 */

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
	path := buildDLL(t)
	ra := &fakeRA{}
	var mu sync.Mutex
	var notices []Notice
	rt := newRuntime(func(h Handler) (engine, error) {
		c, err := Open(path, h)
		if err != nil {
			return nil, err
		}
		return c, nil
	}, ra.send, func(n Notice) {
		mu.Lock()
		notices = append(notices, n)
		mu.Unlock()
	}, nil)
	saw := func(kind string) *Notice {
		mu.Lock()
		defer mu.Unlock()
		for i := range notices {
			if notices[i].Kind == kind {
				return &notices[i]
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
			t.Fatalf("no %q notice; notices: %+v; requests: %v", until, notices, ra.seen)
		}
	}

	ram := make([]byte, 0x800)
	game := append([]byte("NES\x1a"), make([]byte, 16*1024+12)...)
	if err := rt.Start(NES, Memory{SystemRAM: ram}, `C:\games\Sample.nes`, game); err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			rt.Stop()
		}
	}()
	frames("ready", 400)
	if n := saw("ready"); n.Title != "Sample Game" || n.Total != 1 || n.Unlocked != 0 {
		t.Errorf("ready: %+v", *n)
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
	before := rt.List()
	if len(before.Achievements) != 1 || before.Achievements[0].State != "locked" ||
		before.Achievements[0].Badge != "/api/retroachievements/media/Badge/00234_lock.png" {
		t.Errorf("list before: %+v", before)
	}
	ram[1] = 3
	frames("unlocked", 100)
	if n := saw("unlocked"); n.ID != 5501 || n.Points != 5 || n.Badge != "/api/retroachievements/media/Badge/00234.png" {
		t.Errorf("unlocked: %+v", *n)
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
	if sum.GameID != 1234 || sum.Title != "Sample Game" || sum.Total != 1 || sum.Unlocked != 1 ||
		sum.Points != 5 || sum.PointsUnlocked != 5 || len(sum.Achievements) != 1 ||
		sum.Achievements[0].State != "unlocked" || sum.Achievements[0].Title != "Ach1" ||
		!strings.HasSuffix(sum.Badge, "/Images/112233.png") {
		t.Errorf("list: %+v", sum)
	}
	if p := rt.Progress(); len(p) == 0 {
		t.Error("no progress to keep with a state")
	} else {
		rt.SetProgress(p)
	}

	/*
	 * Stopping now, with the set just completed and no frame since the
	 * award's answer, is the moment rcheevos 12.5 crashed the process in
	 * unload (a pending mastery event read a game it had already cleared).
	 * The shim runs one frame first, which also means the person hears it.
	 */
	stopped = true
	rt.Stop()
	if saw("mastered") == nil {
		t.Error("completing the set was never announced")
	}
}
