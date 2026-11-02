package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"lancast/internal/retro/ra"
	"lancast/internal/secret"
	"lancast/internal/store"
)

/*
 * Earning achievements through the server (ADR 0112 section 3), against a
 * fake RetroAchievements Connect endpoint. What matters: the password is
 * used once and kept nowhere, the token never reaches the player, only the
 * requests play makes get through, and an unlock RetroAchievements can't
 * take is kept and sent later, in order, dated when it was earned.
 */

const (
	raPassword = "the right password"
	raToken    = "REAL-CONNECT-TOKEN"
	raPlayerUA = "LANcast/0.10.2 (Windows 10.0) mGBA/0.10.3 rcheevos/12.5"
)

// fakeConnect is RetroAchievements' dorequest.php, recording what it gets.
type fakeConnect struct {
	mu   sync.Mutex
	down bool
	got  []fakeCall
}

type fakeCall struct {
	form url.Values
	ua   string
}

func (f *fakeConnect) calls(request string) []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeCall
	for _, c := range f.got {
		if request == "" || c.form.Get("r") == request {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeConnect) setDown(down bool) {
	f.mu.Lock()
	f.down = down
	f.mu.Unlock()
}

func (f *fakeConnect) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != ra.ConnectPath || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	b, _ := io.ReadAll(r.Body)
	form, _ := url.ParseQuery(string(b))
	f.mu.Lock()
	f.got = append(f.got, fakeCall{form, r.UserAgent()})
	down := f.down
	f.mu.Unlock()
	if down {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	authed := form.Get("t") == raToken || (form.Get("r") == "login2" && form.Get("p") == raPassword)
	if form.Get("r") != "gameid" && !authed {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"Success":false,"Error":"Invalid user/password combination. Please try again.","Code":"invalid_credentials"}`)
		return
	}
	switch form.Get("r") {
	case "login2":
		_, _ = io.WriteString(w, `{"Success":true,"User":"Player","Token":"`+raToken+`","Score":50,"SoftcoreScore":60,"Messages":0}`)
	case "awardachievement":
		if form.Get("a") == "999" {
			_, _ = io.WriteString(w, `{"Success":false,"Error":"Data not found for achievement 999"}`)
			return
		}
		_, _ = io.WriteString(w, `{"Success":true,"AchievementID":`+form.Get("a")+`,"Score":55,"SoftcoreScore":65,"AchievementsRemaining":3}`)
	default:
		_, _ = io.WriteString(w, `{"Success":true}`)
	}
}

type raEarnFixture struct {
	romFixture
	fake   *fakeConnect
	ticket string
}

// newRAEarn is a server with a game, a connected RetroAchievements account,
// and a ticket for the game, as the desktop player holds one.
func newRAEarn(t *testing.T) raEarnFixture {
	t.Helper()
	f := raEarnFixture{romFixture: newROMFixture(t), fake: &fakeConnect{}}
	srv := httptest.NewServer(f.fake)
	t.Cleanup(srv.Close)
	sealer, err := secret.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f.h.srvAPI.SetRetroAchievements(&RetroAchievements{
		Sealer: sealer, Client: ra.Client{Base: srv.URL, UserAgent: "LANcast/test"},
		MediaDir: t.TempDir(), MediaURL: srv.URL,
	})
	key, _ := sealer.Seal([]byte("web-api-key"))
	if err := f.h.st.SetRetroAccount(context.Background(), store.RetroAccount{
		UserID: "local", RAUser: "Player", RAULID: "01PLAYER", APIKeySealed: key,
	}); err != nil {
		t.Fatal(err)
	}
	f.ticket = mintTicket(t, f.h, f.cart)
	return f
}

func (f raEarnFixture) earnOn(t *testing.T) {
	t.Helper()
	resp := f.h.do(t, "PUT", "/api/profile/retroachievements/earning", map[string]any{"password": raPassword})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("turning earning on = %d %s", resp.StatusCode, b)
	}
}

// door sends what the player's rcheevos would, under the game's ticket.
func (f raEarnFixture) door(t *testing.T, ua, post string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", f.h.srv.URL+"/api/items/"+itoa(f.cart)+"/retroachievements", strings.NewReader(post))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Ticket "+f.ticket)
	req.Header.Set("User-Agent", ua)
	resp, err := f.h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (f raEarnFixture) view(t *testing.T) raView {
	t.Helper()
	var v raView
	decode(t, f.h.do(t, "GET", "/api/profile/retroachievements", nil), &v)
	return v
}

// award is an unlock as rcheevos signs it.
func award(id int) string {
	f := url.Values{"r": {"awardachievement"}, "u": {"Player"}, "t": {ra.PlaceholderToken},
		"a": {strconv.Itoa(id)}, "h": {"0"}, "m": {"0123456789abcdef0123456789abcdef"}}
	v, _ := ra.Signature(f)
	f.Set("v", v)
	return f.Encode()
}

func TestEarningIsTurnedOnWithThePasswordOnce(t *testing.T) {
	f := newRAEarn(t)
	if v := f.view(t); v.Earning {
		t.Fatal("earning before it was turned on")
	}
	if code := status(t, f.h.do(t, "PUT", "/api/profile/retroachievements/earning",
		map[string]any{"password": "wrong"})); code != http.StatusBadRequest {
		t.Fatalf("a wrong password = %d, want 400", code)
	}
	if acct, _ := f.h.st.GetRetroAccount(context.Background(), "local"); acct.ConnectTokenSealed != nil {
		t.Fatal("a refused login left a token behind")
	}
	f.earnOn(t)
	if v := f.view(t); !v.Earning || v.Queued != 0 {
		t.Fatalf("after turning earning on: earning %v queued %d", v.Earning, v.Queued)
	}
	acct, _ := f.h.st.GetRetroAccount(context.Background(), "local")
	if len(acct.ConnectTokenSealed) == 0 || strings.Contains(string(acct.ConnectTokenSealed), raToken) {
		t.Fatalf("the token is not stored sealed: %q", acct.ConnectTokenSealed)
	}
	// The password went to RetroAchievements in the login and nowhere else.
	for _, c := range f.fake.calls("") {
		if c.form.Has("p") && c.form.Get("r") != "login2" {
			t.Fatalf("the password went out in %v", c.form)
		}
	}
	if code := status(t, f.h.do(t, "DELETE", "/api/profile/retroachievements/earning", nil)); code != http.StatusNoContent {
		t.Fatalf("turning earning off = %d", code)
	}
	if v := f.view(t); v.Earning {
		t.Fatal("still earning after turning it off")
	}
}

func TestEarningNeedsAConnectedAccount(t *testing.T) {
	f := newRAEarn(t)
	if err := f.h.st.DeleteRetroAccount(context.Background(), "local"); err != nil {
		t.Fatal(err)
	}
	if code := status(t, f.h.do(t, "PUT", "/api/profile/retroachievements/earning",
		map[string]any{"password": raPassword})); code != http.StatusConflict {
		t.Fatalf("earning with nothing connected = %d, want 409", code)
	}
	if n := len(f.fake.calls("")); n != 0 {
		t.Fatalf("RetroAchievements was contacted %d times for an account that isn't connected", n)
	}
}

// The player logs in with the placeholder; RetroAchievements sees the real
// token and the player's own user agent; the player never sees the token.
func TestTheDoorCarriesTheTokenThePlayerNeverSees(t *testing.T) {
	f := newRAEarn(t)
	f.earnOn(t)
	code, body := f.door(t, raPlayerUA, "r=login2&u=player&t="+ra.PlaceholderToken)
	if code != http.StatusOK {
		t.Fatalf("login through the door = %d %s", code, body)
	}
	if strings.Contains(body, raToken) || !strings.Contains(body, `"Token":"`+ra.PlaceholderToken+`"`) {
		t.Fatalf("the player was shown %s", body)
	}
	logins := f.fake.calls("login2")
	last := logins[len(logins)-1]
	if last.form.Get("t") != raToken || last.form.Get("u") != "Player" || last.ua != raPlayerUA {
		t.Fatalf("RetroAchievements got %v as %q", last.form, last.ua)
	}
	if code, _ := f.door(t, raPlayerUA, "r=ping&u=Player&t=lancast&g=1&m=Level+1"); code != http.StatusOK {
		t.Fatalf("a ping = %d", code)
	}
	if code, _ := f.door(t, raPlayerUA, "r=gameid&m=0123"); code != http.StatusOK {
		t.Fatalf("a hash lookup = %d", code)
	}
	if lookup := f.fake.calls("gameid"); len(lookup) != 1 || lookup[0].form.Has("t") {
		t.Fatalf("the lookup went out as %v", lookup)
	}
}

func TestTheDoorLetsOnlyPlayThrough(t *testing.T) {
	f := newRAEarn(t)
	if code, _ := f.door(t, raPlayerUA, "r=ping&u=Player&t=lancast&g=1"); code != http.StatusConflict {
		t.Fatalf("with earning off = %d, want 409", code)
	}
	f.earnOn(t)
	before := len(f.fake.calls(""))
	refused := map[string]struct {
		ua, post string
		code     int
	}{
		"a browser":           {"Mozilla/5.0", "r=ping&u=Player&t=lancast&g=1", 400},
		"no core named":       {"LANcast-Client (retro)", "r=ping&u=Player&t=lancast&g=1", 400},
		"an editor's request": {raPlayerUA, "r=uploadachievement&u=Player&t=lancast", 403},
		"another account":     {raPlayerUA, "r=ping&u=Someone&t=lancast&g=1", 403},
		"hardcore":            {raPlayerUA, "r=startsession&u=Player&t=lancast&g=1&h=1", 403},
		"a password":          {raPlayerUA, "r=login2&u=Player&p=x", 400},
		"a token of its own":  {raPlayerUA, "r=ping&u=Player&t=guess&g=1", 400},
		"not a form":          {raPlayerUA, "%zz", 400},
	}
	for why, c := range refused {
		if code, body := f.door(t, c.ua, c.post); code != c.code {
			t.Errorf("%s = %d %s, want %d", why, code, body, c.code)
		}
	}
	if n := len(f.fake.calls("")); n != before {
		t.Fatalf("%d refused requests reached RetroAchievements", n-before)
	}

	// The ticket is one game's: shown on another game's door it opens nothing.
	req, _ := http.NewRequest("POST", f.h.srv.URL+"/api/items/"+itoa(f.disc)+"/retroachievements", strings.NewReader("r=ping&u=Player&t=lancast"))
	req.Header.Set("Authorization", "Ticket "+f.ticket)
	req.Header.Set("User-Agent", raPlayerUA)
	f.h.secure(t, "a good long password")
	resp, err := f.h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("another game's door with this ticket = %d, want 401", resp.StatusCode)
	}
}

// RetroAchievements down: the unlock is kept, the player told it worked, and
// what's earned next waits behind it. Up again: both go, in order.
func TestAnUnlockRetroAchievementsCantTakeIsKept(t *testing.T) {
	f := newRAEarn(t)
	f.earnOn(t)
	f.door(t, raPlayerUA, "r=login2&u=Player&t=lancast") // learns the points
	f.fake.setDown(true)

	code, body := f.door(t, raPlayerUA, award(11))
	if code != http.StatusOK {
		t.Fatalf("an unlock while RetroAchievements is down = %d %s", code, body)
	}
	var reply struct {
		Success       bool
		Queued        bool
		AchievementID int
		Score         int
		SoftcoreScore int
	}
	if err := json.Unmarshal([]byte(body), &reply); err != nil || !reply.Success || !reply.Queued ||
		reply.AchievementID != 11 || reply.Score != 50 || reply.SoftcoreScore != 60 {
		t.Fatalf("the player was told %s", body)
	}

	f.fake.setDown(false)
	sentBefore := len(f.fake.calls("awardachievement"))
	if code, _ := f.door(t, raPlayerUA, award(12)); code != http.StatusOK {
		t.Fatal("the second unlock was refused")
	}
	if n := len(f.fake.calls("awardachievement")); n != sentBefore {
		t.Fatal("an unlock overtook the one queued before it")
	}
	if v := f.view(t); v.Queued != 2 {
		t.Fatalf("queued = %d, want 2", v.Queued)
	}

	if waiting := f.h.srvAPI.sendRetroQueue(context.Background()); waiting {
		t.Fatal("the queue says it is still waiting with RetroAchievements up")
	}
	sent := f.fake.calls("awardachievement")[sentBefore:]
	if len(sent) != 2 || sent[0].form.Get("a") != "11" || sent[1].form.Get("a") != "12" {
		t.Fatalf("sent %v", sent)
	}
	for _, c := range sent {
		if c.form.Get("t") != raToken || c.ua != raPlayerUA {
			t.Errorf("sent as %q with token %q", c.ua, c.form.Get("t"))
		}
		if v, _ := ra.Signature(c.form); v != c.form.Get("v") {
			t.Errorf("unlock %s went out with a signature RetroAchievements would reject", c.form.Get("a"))
		}
	}
	if v := f.view(t); v.Queued != 0 {
		t.Fatalf("queued after sending = %d", v.Queued)
	}
}

// An unlock that waited is dated when it was earned: its delay is the wait,
// and it is signed again with it.
func TestAQueuedUnlockIsDatedWhenItWasEarned(t *testing.T) {
	f := newRAEarn(t)
	f.earnOn(t)
	form, _ := url.ParseQuery(award(21))
	if _, err := f.h.st.EnqueueRetro(context.Background(), store.RetroQueued{
		UserID: "local", RAUser: "Player", Request: "awardachievement",
		Form: ra.WithoutToken(form).Encode(), UserAgent: raPlayerUA,
		CreatedAt: time.Now().Add(-2 * time.Minute).Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	f.h.srvAPI.sendRetroQueue(context.Background())
	sent := f.fake.calls("awardachievement")
	if len(sent) != 1 {
		t.Fatalf("sent %d", len(sent))
	}
	o, _ := strconv.Atoi(sent[0].form.Get("o"))
	if o < 119 || o > 125 {
		t.Fatalf("delay = %ds, want about 120", o)
	}
	if v, _ := ra.Signature(sent[0].form); v != sent[0].form.Get("v") {
		t.Fatal("the delayed unlock was not signed again")
	}
}

// While RetroAchievements is down only an account's first row is tried; one
// RetroAchievements turns down for good is dropped rather than holding up
// the rest; one for an account that no longer earns as that user is never
// sent at all.
func TestTheQueueHoldsOrderAndLetsGoOfWhatCantBeSent(t *testing.T) {
	f := newRAEarn(t)
	f.earnOn(t)
	ctx := context.Background()
	enqueue := func(id int, raUser string) {
		form, _ := url.ParseQuery(award(id))
		if _, err := f.h.st.EnqueueRetro(ctx, store.RetroQueued{
			UserID: "local", RAUser: raUser, Request: "awardachievement",
			Form: ra.WithoutToken(form).Encode(), UserAgent: raPlayerUA,
		}); err != nil {
			t.Fatal(err)
		}
	}
	enqueue(31, "Player")
	enqueue(32, "Player")
	f.fake.setDown(true)
	if waiting := f.h.srvAPI.sendRetroQueue(ctx); !waiting {
		t.Fatal("the queue doesn't know it is waiting")
	}
	if n := len(f.fake.calls("awardachievement")); n != 1 {
		t.Fatalf("%d tries while down, want only the first row's", n)
	}
	q, _ := f.h.st.RetroQueue(ctx, 10)
	if len(q) != 2 || q[0].Attempts != 1 || q[0].LastError == "" || q[1].Attempts != 0 {
		t.Fatalf("queue after a failed try: %+v", q)
	}

	f.fake.setDown(false)
	enqueue(999, "Player")    // RetroAchievements has no such achievement
	enqueue(33, "SomeoneOld") // made under a connection since replaced
	f.h.srvAPI.sendRetroQueue(ctx)
	if q, _ := f.h.st.RetroQueue(ctx, 10); len(q) != 0 {
		t.Fatalf("left behind: %+v", q)
	}
	var ids []string
	for _, c := range f.fake.calls("awardachievement") {
		ids = append(ids, c.form.Get("a"))
	}
	if got := strings.Join(ids, ","); got != "31,31,32,999" {
		t.Fatalf("sent %s; want 31 (down), then 31, 32, 999, and never 33", got)
	}
}
