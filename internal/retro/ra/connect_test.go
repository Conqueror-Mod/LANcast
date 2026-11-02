package ra

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func form(t *testing.T, s string) url.Values {
	t.Helper()
	v, err := url.ParseQuery(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// The signatures, against requests rcheevos itself built: its own test suite
// at the pinned tag (test/rapi/test_rc_api_runtime.c). A signature computed
// differently here would be an unlock RetroAchievements throws away.
func TestSignatureIsRcheevosOwn(t *testing.T) {
	for _, post := range []string{
		"r=awardachievement&u=Username&t=API_TOKEN&a=1234&h=1&m=ABCDEF0123456789&v=b8aefaad6f9659e2164bc60da0c3b64d",
		"r=awardachievement&u=Username&t=API_TOKEN&a=1234&h=0&m=ABABCBCBDEDEFFFF&v=ed81d6ecf825f8cbe3ae1edace098892",
		"r=awardachievement&u=Username&t=API_TOKEN&a=5432&h=1&v=31048257ab1788386e71ab0c222aa5c8",
		"r=awardachievement&u=Username&t=API_TOKEN&a=1234&h=1&m=ABCDEF0123456789&o=17&v=b2326b09d61e9264eb5d3607d947317d",
		"r=submitlbentry&u=Username&t=API_TOKEN&i=1234&s=10999&m=ABCDEF0123456789&v=e13c9132ee651256f9d2ee8f06f75d76",
		"r=submitlbentry&u=Username&t=API_TOKEN&i=1111&s=0&m=ABCDEF0123456789&v=9c2ac665157d68b8a26e83bb71dd8aaf",
		"r=submitlbentry&u=Username&t=API_TOKEN&i=1111&s=-234781&m=ABCDEF0123456789&v=fbe290266f2d121a7a37942e1e90f453",
		"r=submitlbentry&u=Username&t=API_TOKEN&i=1234&s=10999&m=ABCDEF0123456789&o=33&v=7971fc37cf38026f99dd4bae84360ac1",
	} {
		f := form(t, post)
		got, err := Signature(f)
		if err != nil || got != f.Get("v") {
			t.Errorf("%s:\n  signature %q %v, rcheevos made %q", post, got, err, f.Get("v"))
		}
	}
}

// A queued unlock goes out later with the wait added to its delay, signed
// again: the delayed request is exactly what rcheevos would have built had
// it waited itself.
func TestADelayedUnlockIsSignedAgain(t *testing.T) {
	made := form(t, "r=awardachievement&u=Username&a=1234&h=1&m=ABCDEF0123456789&v=b8aefaad6f9659e2164bc60da0c3b64d")
	got, err := Delayed(made, 17)
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("o") != "17" || got.Get("v") != "b2326b09d61e9264eb5d3607d947317d" {
		t.Fatalf("delayed by 17s: o=%s v=%s, want rcheevos' o=17 v=b2326b09…", got.Get("o"), got.Get("v"))
	}
	// A request that was already delayed adds to its delay.
	lb := form(t, "r=submitlbentry&u=Username&i=1234&s=10999&m=ABCDEF0123456789&o=30&v=x")
	got, err = Delayed(lb, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("o") != "33" || got.Get("v") != "7971fc37cf38026f99dd4bae84360ac1" {
		t.Fatalf("delayed again: o=%s v=%s, want o=33 v=7971fc37…", got.Get("o"), got.Get("v"))
	}
	if made.Get("o") != "" {
		t.Fatal("Delayed changed the form it was given")
	}
}

func TestTheDoorAdmitsOnlyPlay(t *testing.T) {
	ok := []string{
		"r=login2&u=Player&t=lancast",
		"r=achievementsets&u=player&t=lancast&m=abc", // RetroAchievements names are case-insensitive
		"r=startsession&u=Player&t=lancast&g=1&h=0",
		"r=awardachievement&u=Player&t=lancast&a=1&h=0&v=x",
		"r=gameid&m=abc", // rcheevos looks a hash up signed out
	}
	for _, s := range ok {
		if _, err := Admit(form(t, s), "Player"); err != nil {
			t.Errorf("%s refused: %v", s, err)
		}
	}
	refused := map[string]string{
		"r=uploadachievement&u=Player&t=lancast":        "an editor's request",
		"r=allprogress&u=Player&t=lancast":              "not something play asks",
		"r=awardachievement&u=Someone&t=lancast&a=1":    "another account",
		"r=awardachievement&u=Player&t=real&a=1":        "a token of the player's own",
		"r=login2&u=Player&p=hunter2":                   "a password through the player",
		"r=awardachievement&u=Player&t=lancast&a=1&h=1": "hardcore",
		"r=startsession&u=Player&t=lancast&g=1&h=1":     "a hardcore session",
		"u=Player&t=lancast":                            "no request at all",
	}
	for s, why := range refused {
		var de *DoorError
		if _, err := Admit(form(t, s), "Player"); !errors.As(err, &de) {
			t.Errorf("%s (%s) was admitted", s, why)
		}
	}
}

func TestTheTokenIsSwappedInAndOut(t *testing.T) {
	if lookup := WithToken(form(t, "r=gameid&m=abc"), "Player", "REAL"); lookup.Has("t") || lookup.Has("u") {
		t.Fatalf("a signed-out lookup was given the account: %v", lookup)
	}
	f := form(t, "r=ping&u=player&t=lancast&g=1")
	sent := WithToken(f, "Player", "REAL")
	if sent.Get("t") != "REAL" || sent.Get("u") != "Player" || f.Get("t") != "lancast" {
		t.Fatalf("with token: %v (from %v)", sent, f)
	}
	if kept := WithoutToken(sent); kept.Has("t") {
		t.Fatalf("the queue's copy kept the token: %v", kept)
	}
	reply := []byte(`{"Success":true,"User":"Player","Token":"REAL\"ish","Score":5,"SoftcoreScore":7}`)
	hidden := string(HideToken(reply))
	if strings.Contains(hidden, "REAL") || !strings.Contains(hidden, `"Token":"lancast"`) {
		t.Fatalf("login reply after hiding: %s", hidden)
	}
	if s, sc, ok := Scores(reply); !ok || s != 5 || sc != 7 {
		t.Fatalf("scores = %d %d %v", s, sc, ok)
	}
}

func TestUserAgents(t *testing.T) {
	for _, ua := range []string{
		"LANcast/0.10.2 (Windows 10.0) mGBA/0.10.3 rcheevos/12.5",
		"LANcast/0.10.2-dev (Windows 10.0.26300) Mupen64Plus-Next/2.6",
	} {
		if !ValidUserAgent(ua) {
			t.Errorf("%q refused", ua)
		}
	}
	for _, ua := range []string{"", "Mozilla/5.0", "LANcast/0.10.2", "LANcast/0.10.2 (Linux) mGBA/1", "LANcast-Client (retro)"} {
		if ValidUserAgent(ua) {
			t.Errorf("%q accepted", ua)
		}
	}
}

func TestRepliesAreReadAsRetroAchievementsMeansThem(t *testing.T) {
	cases := []struct {
		r            Reply
		taken, retry bool
	}{
		{Reply{200, []byte(`{"Success":true,"AchievementID":1}`)}, true, false},
		{Reply{200, []byte(`{"Success":false,"Error":"User already has this achievement unlocked."}`)}, true, false},
		{Reply{200, []byte(`{"Success":false,"Error":"Achievement not found"}`)}, false, false},
		{Reply{503, []byte(`<html>`)}, false, true},
		{Reply{429, nil}, false, true},
		{Reply{401, []byte(`{"Success":false,"Code":"invalid_credentials"}`)}, false, false},
	}
	for _, c := range cases {
		if c.r.Taken() != c.taken || c.r.Retry() != c.retry {
			t.Errorf("%d %s: taken %v retry %v, want %v %v", c.r.Status, c.r.Body, c.r.Taken(), c.r.Retry(), c.taken, c.retry)
		}
	}
	award := string(QueuedReply(form(t, "r=awardachievement&a=77"), 10, 20))
	for _, want := range []string{`"AchievementID":77`, `"Success":true`, `"Score":10`, `"SoftcoreScore":20`} {
		if !strings.Contains(award, want) {
			t.Errorf("queued award reply %s lacks %s", award, want)
		}
	}
	lb := string(QueuedReply(form(t, "r=submitlbentry&i=5&s=-12"), 0, 0))
	for _, want := range []string{`"Score":-12`, `"BestScore":-12`, `"NumEntries":"0"`, `"TopEntries":[]`} {
		if !strings.Contains(lb, want) {
			t.Errorf("queued leaderboard reply %s lacks %s", lb, want)
		}
	}
}

// Login sends the password once and keeps nothing but the token.
func TestLoginExchangesAPasswordForAToken(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ConnectPath || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		got, _ = url.ParseQuery(string(b))
		if got.Get("p") != "right" {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"Success":false,"Error":"Invalid user/password combination. Please try again.","Code":"invalid_credentials"}`)
			return
		}
		io.WriteString(w, `{"Success":true,"User":"Player","Token":"TOKEN","Score":1,"SoftcoreScore":2}`)
	}))
	defer srv.Close()
	c := Client{Base: srv.URL}
	token, name, err := c.Login(context.Background(), "player", "right")
	if err != nil || token != "TOKEN" || name != "Player" {
		t.Fatalf("login = %q %q %v", token, name, err)
	}
	if got.Get("r") != "login2" || got.Get("u") != "player" {
		t.Fatalf("login form %v", got)
	}
	if _, _, err := c.Login(context.Background(), "player", "wrong"); !errors.Is(err, ErrBadPassword) {
		t.Fatalf("wrong password: %v", err)
	}
}
