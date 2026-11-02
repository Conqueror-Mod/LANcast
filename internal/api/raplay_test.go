package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"lancast/internal/retro/ra"
	"lancast/internal/secret"
)

/*
 * Earning achievements while playing (ADR 0112), against a fake
 * RetroAchievements. What matters most: the password is used once and not
 * kept, the token never reaches the player, only softcore requests for the
 * person's own account go through, and an unlock made while
 * RetroAchievements is down is kept and sent later with its real time.
 */

const raRealToken = "real-token-9f8e7d"

type fakeConnect struct {
	mu    sync.Mutex
	down  bool
	seen  []url.Values
	agent []string
}

func (f *fakeConnect) handler(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/API/") {
		if r.URL.Query().Get("y") != raGoodKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"User":"Player1","ULID":"01FAKEULID0000000000000000","TotalPoints":1}`))
		return
	}
	b, _ := io.ReadAll(r.Body)
	v, _ := url.ParseQuery(string(b))
	f.mu.Lock()
	f.seen = append(f.seen, v)
	f.agent = append(f.agent, r.Header.Get("User-Agent"))
	down := f.down
	f.mu.Unlock()
	if down {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if v.Get("r") == "login2" && v.Has("p") {
		if v.Get("p") != "right password" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"Success":false,"Error":"Invalid User/Password combination."}`))
			return
		}
		_, _ = w.Write([]byte(`{"Success":true,"User":"Player1","Token":"` + raRealToken + `"}`))
		return
	}
	if v.Get("t") != raRealToken {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"Success":false,"Error":"Invalid token"}`))
		return
	}
	switch v.Get("r") {
	case "login2":
		_, _ = w.Write([]byte(`{"Success":true,"User":"Player1","Token":"` + raRealToken + `","Score":1}`))
	case "awardachievement":
		_, _ = w.Write([]byte(`{"Success":true,"Score":6,"AchievementID":` + v.Get("a") + `}`))
	default:
		_, _ = w.Write([]byte(`{"Success":true}`))
	}
}

func (f *fakeConnect) last() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen[len(f.seen)-1]
}

func raPlayFixture(t *testing.T) (romFixture, *fakeConnect) {
	t.Helper()
	f := newROMFixture(t)
	f.h.secure(t, "a good long password")
	fc := &fakeConnect{}
	srv := httptest.NewServer(http.HandlerFunc(fc.handler))
	t.Cleanup(srv.Close)
	sealer, err := secret.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f.h.srvAPI.SetRetroAchievements(&RetroAchievements{
		Sealer: sealer, Client: ra.Client{Base: srv.URL, UserAgent: "LANcast/test"},
		MediaDir: t.TempDir(), MediaURL: srv.URL,
	})
	resp := f.h.authed(t, "PUT", "/api/profile/retroachievements", map[string]any{"username": "Player1", "api_key": raGoodKey})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("connect: %d", resp.StatusCode)
	}
	return f, fc
}

// send posts one rcheevos request with the game's ticket, as the player does.
func (f romFixture) send(t *testing.T, ticket, form string) (int, raConnectAnswer, string) {
	t.Helper()
	b, _ := json.Marshal(raConnectRequest{Body: form, UserAgentClause: "rcheevos/12.5"})
	resp := f.raw(t, "POST", "/api/items/"+itoa(f.cart)+"/retroachievements", b, ticket)
	raw := body(t, resp)
	var a raConnectAnswer
	_ = json.Unmarshal([]byte(raw), &a)
	return resp.StatusCode, a, raw
}

func TestEarningWhilePlaying(t *testing.T) {
	f, fc := raPlayFixture(t)
	tk := "Ticket " + mintTicket(t, f.h, f.cart)

	// Before earning is on, the player is told so.
	if code, _, raw := f.send(t, tk, "r=ping&u=Player1&t=lancast&g=1"); code != http.StatusConflict ||
		!strings.Contains(raw, "earning_off") {
		t.Fatalf("before turning on: %d %s", code, raw)
	}

	// A wrong password is refused and nothing is kept.
	if code := status(t, f.h.authed(t, "PUT", "/api/profile/retroachievements/play",
		map[string]any{"password": "wrong"})); code != http.StatusBadRequest {
		t.Errorf("a wrong password = %d, want 400", code)
	}
	resp := f.h.authed(t, "PUT", "/api/profile/retroachievements/play", map[string]any{"password": "right password"})
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || strings.Contains(string(raw), raRealToken) {
		t.Fatalf("turn on: %d %s", resp.StatusCode, raw)
	}
	var view raView
	_ = json.Unmarshal(raw, &view)
	if !view.Earning {
		t.Errorf("earning not shown on: %s", raw)
	}

	// The player signs in as the placeholder; RetroAchievements gets the
	// person's name and real token, and the player gets the placeholder back.
	code, ans, raw2 := f.send(t, tk, "r=login2&u=lancast&t=lancast")
	if code != http.StatusOK || ans.Status != http.StatusOK || strings.Contains(raw2, raRealToken) ||
		!strings.Contains(ans.Body, `"Token":"lancast"`) {
		t.Fatalf("login2: %d %s", code, raw2)
	}
	if got := fc.last(); got.Get("t") != raRealToken || got.Get("u") != "Player1" {
		t.Errorf("RetroAchievements was sent u=%q t=%q", got.Get("u"), got.Get("t"))
	}
	if agent := fc.agent[len(fc.agent)-1]; !strings.HasPrefix(agent, "LANcast/") || !strings.HasSuffix(agent, " rcheevos/12.5") {
		t.Errorf("user agent %q", agent)
	}

	// Softcore and this account only.
	for why, form := range map[string]string{
		"hardcore":      "r=awardachievement&u=Player1&t=lancast&a=7&h=1",
		"someone else":  "r=ping&u=Player2&t=lancast",
		"a leaderboard": "r=submitlbentry&u=Player1&t=lancast&i=1&s=5",
	} {
		if code, _, _ := f.send(t, tk, form); code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", why, code)
		}
	}

	// RetroAchievements down: the award is kept, and the player is told it
	// unlocked.
	fc.mu.Lock()
	fc.down = true
	fc.mu.Unlock()
	award := "r=awardachievement&u=Player1&t=lancast&a=12345&h=0&m=abc&v=" +
		ra.AwardSignature("12345", "Player1", "0", 0)
	code, ans, raw2 = f.send(t, tk, award)
	if code != http.StatusOK || !ans.Queued || !strings.Contains(ans.Body, `"Success":true`) {
		t.Fatalf("award while down: %d %s", code, raw2)
	}
	queued, err := f.h.st.DueRetroUnlocks(context.Background(), time.Now(), 10)
	if err != nil || len(queued) != 1 || strings.Contains(queued[0].Body, raRealToken) || strings.Contains(queued[0].Body, "t=") {
		t.Fatalf("queue: %+v %v", queued, err)
	}
	decode(t, f.h.authed(t, "GET", "/api/profile/retroachievements", nil), &view)
	if view.UnlocksWaiting != 1 {
		t.Errorf("unlocks waiting = %d", view.UnlocksWaiting)
	}

	// Still down: it waits longer, and is not lost.
	f.h.srvAPI.sendRetroUnlocks(context.Background(), time.Now())
	if n, _ := f.h.st.CountRetroUnlocks(context.Background(), queued[0].UserID); n != 1 {
		t.Fatalf("a failed retry lost the unlock (%d left)", n)
	}

	// Back up: it goes, with the time it waited, signed as rcheevos would.
	fc.mu.Lock()
	fc.down = false
	fc.mu.Unlock()
	later := time.Unix(queued[0].QueuedAt, 0).Add(2 * time.Hour)
	f.h.srvAPI.sendRetroUnlocks(context.Background(), later)
	sent := fc.last()
	if sent.Get("r") != "awardachievement" || sent.Get("t") != raRealToken || sent.Get("o") != "7200" ||
		sent.Get("v") != ra.AwardSignature("12345", "Player1", "0", 7200) {
		t.Errorf("sent %v", sent)
	}
	if n, _ := f.h.st.CountRetroUnlocks(context.Background(), queued[0].UserID); n != 0 {
		t.Errorf("%d still waiting after it was sent", n)
	}

	// Off: the token goes, and the player is told earning is off.
	if code := status(t, f.h.authed(t, "DELETE", "/api/profile/retroachievements/play", nil)); code != http.StatusNoContent {
		t.Errorf("turn off = %d", code)
	}
	if code, _, _ := f.send(t, tk, "r=ping&u=Player1&t=lancast"); code != http.StatusConflict {
		t.Errorf("after turning off: %d, want 409", code)
	}
}

// Another game's ticket, or none, cannot send on this game's behalf.
func TestOnlyThisGamesTicketSends(t *testing.T) {
	f, _ := raPlayFixture(t)
	other := "Ticket " + mintTicket(t, f.h, f.disc)
	if code, _, _ := f.send(t, other, "r=ping&u=Player1&t=lancast"); code != http.StatusUnauthorized {
		t.Errorf("another game's ticket: %d, want 401", code)
	}
	if code, _, _ := f.send(t, "Ticket nonsense", "r=ping&u=Player1&t=lancast"); code != http.StatusUnauthorized {
		t.Errorf("a made-up ticket: %d, want 401", code)
	}
}
