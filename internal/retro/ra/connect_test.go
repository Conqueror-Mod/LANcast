package ra

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Both vectors are what rcheevos v12.5's own rc_api_init_award_achievement_
// request printed, built and run on 2026-11-01: if Delay signs differently,
// RetroAchievements refuses the award.
const (
	awardNoDelay = "r=awardachievement&u=Chrisdemo&t=tok&a=12345&h=0&m=abcdef0123456789abcdef0123456789&v=8e7ce8a62f943e4d2ddfd800072e3191"
	award95s     = "r=awardachievement&u=Chrisdemo&t=tok&a=12345&h=0&m=abcdef0123456789abcdef0123456789&o=95&v=7a83eb58bf1ccb095da2b961625ba266"
)

func TestTheSignatureIsRcheevosOwn(t *testing.T) {
	for _, form := range []string{awardNoDelay, award95s} {
		v, _ := url.ParseQuery(form)
		var secs int64
		if o := v.Get("o"); o != "" {
			secs = 95
		}
		if got := AwardSignature(v.Get("a"), v.Get("u"), v.Get("h"), secs); got != v.Get("v") {
			t.Errorf("%s: signed %s", form, got)
		}
	}
}

// An award that waited 95 seconds is sent as rcheevos would have sent it 95
// seconds after the unlock.
func TestAWaitedAwardKeepsItsTime(t *testing.T) {
	v, _ := url.ParseQuery(awardNoDelay)
	got := Delay(v, 95*time.Second)
	want, _ := url.ParseQuery(award95s)
	for _, k := range []string{"o", "v", "a", "u", "h", "m"} {
		if got.Get(k) != want.Get(k) {
			t.Errorf("%s = %q, want %q", k, got.Get(k), want.Get(k))
		}
	}
	// Waiting again adds to what was already there.
	if again := Delay(got, 5*time.Second); again.Get("o") != "100" {
		t.Errorf("o = %s", again.Get("o"))
	}
}

func TestCheckLetsOnlyTheSoftcorePlayersRequestsThrough(t *testing.T) {
	ok := []string{
		"r=login2&u=Chrisdemo&t=lancast",
		"r=gameid&m=abc",
		"r=achievementsets&u=chrisDEMO&t=lancast&m=abc",
		"r=startsession&u=Chrisdemo&t=lancast&g=1&h=0",
		awardNoDelay,
	}
	for _, f := range ok {
		if !strings.Contains(f, "u=") {
			f += "&u=Chrisdemo"
		}
		v, _, err := Check(f, "Chrisdemo")
		if err != nil {
			t.Errorf("%s refused: %v", f, err)
			continue
		}
		if v.Has("t") {
			t.Errorf("%s: the token field was kept", f)
		}
	}
	// Signing in as the placeholder is signing in as the connected account.
	if v, _, err := Check("r=login2&u=lancast&t=lancast", "Player1"); err != nil || v.Get("u") != "Player1" {
		t.Errorf("placeholder sign-in: %v %v", v, err)
	}
	refused := map[string]string{
		"the placeholder on anything but sign-in": "r=ping&u=lancast&t=lancast",

		"a password":          "r=login2&u=Chrisdemo&p=hunter2",
		"someone else":        "r=ping&u=Somebody&t=lancast",
		"hardcore":            strings.Replace(awardNoDelay, "h=0", "h=1", 1),
		"a leaderboard entry": "r=submitlbentry&u=Chrisdemo&t=lancast&i=1&s=100",
		"an editor upload":    "r=uploadachievement&u=Chrisdemo&t=lancast",
		"nothing":             "",
	}
	for why, f := range refused {
		if _, _, err := Check(f, "Chrisdemo"); err == nil {
			t.Errorf("%s was let through", why)
		}
	}
}

func TestTheRealTokenNeverGoesBack(t *testing.T) {
	got := string(RedactToken([]byte(`{"Success":true,"User":"Chrisdemo","Token":"r3al-t0ken","Score":5}`)))
	if strings.Contains(got, "r3al-t0ken") || !strings.Contains(got, `"Token":"lancast"`) || !strings.Contains(got, `"Score":5`) {
		t.Errorf("got %s", got)
	}
	if got := string(RedactToken([]byte(`{"Success":true}`))); got != `{"Success":true}` {
		t.Errorf("an answer with no token was changed: %s", got)
	}
}

func TestLoginExchangesThePasswordOnce(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, r.URL.Path+" "+string(b)+" "+r.Header.Get("User-Agent"))
		v, _ := url.ParseQuery(string(b))
		if v.Get("p") != "right" {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"Success":false,"Error":"Invalid User/Password combination. Please try again","Code":"invalid_credentials","Status":401}`)
			return
		}
		io.WriteString(w, `{"Success":true,"User":"Chrisdemo","Token":"tok123","Score":10}`)
	}))
	defer srv.Close()
	c := Client{Base: srv.URL, UserAgent: "LANcast/test"}

	name, token, err := c.Login(context.Background(), "chrisdemo", "right")
	if err != nil || name != "Chrisdemo" || token != "tok123" {
		t.Fatalf("got %q %q %v", name, token, err)
	}
	if _, _, err := c.Login(context.Background(), "chrisdemo", "wrong"); err != ErrLoginRefused {
		t.Errorf("wrong password: %v", err)
	}
	if !strings.HasPrefix(seen[0], "/dorequest.php ") || !strings.Contains(seen[0], "r=login2") ||
		!strings.HasSuffix(seen[0], " LANcast/test") {
		t.Errorf("sent %s", seen[0])
	}
}

func TestAnUnavailableServerIsTryLater(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	if _, _, err := (Client{Base: srv.URL}).Do(context.Background(), "r=ping", ""); err != ErrUnreachable {
		t.Errorf("502: %v", err)
	}
	srv.Close()
	if _, _, err := (Client{Base: srv.URL}).Do(context.Background(), "r=ping", ""); err != ErrUnreachable {
		t.Errorf("closed: %v", err)
	}
}

func TestAQueuedAwardLooksUnlockedToThePlayer(t *testing.T) {
	if got := string(QueuedAnswer("12345")); got != `{"AchievementID":12345,"Success":true}` {
		t.Errorf("got %s", got)
	}
}
