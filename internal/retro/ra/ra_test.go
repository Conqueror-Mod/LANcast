package ra

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A fake RetroAchievements answering the three endpoints as the docs show.
func fakeRA(t *testing.T, key string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("y") != key {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("User-Agent") != "LANcast/0.9.88" {
			t.Errorf("user agent %q", r.Header.Get("User-Agent"))
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "API_GetUserProfile.php"):
			if r.URL.Query().Get("u") != "Player1" {
				_, _ = w.Write([]byte(`{}`))
				return
			}
			_, _ = w.Write([]byte(`{"User":"Player1","ULID":"01FAKEULID0000000000000000","UserPic":"/UserPic/Player1.png",
				"TotalPoints":1200,"TotalSoftcorePoints":40,"TotalTruePoints":3000,"Untracked":0}`))
		case strings.HasSuffix(r.URL.Path, "API_GetUserRecentAchievements.php"):
			if r.URL.Query().Get("m") != "20160" {
				t.Errorf("m = %q", r.URL.Query().Get("m"))
			}
			_, _ = w.Write([]byte(`[{"Date":"2026-10-17 21:00:00","HardcoreMode":1,"AchievementID":9,"Title":"First",
				"BadgeURL":"/Badge/123.png","Points":5,"GameTitle":"A Game","GameIcon":"/Images/1.png","GameID":3,"ConsoleName":"SNES"},
				{"Date":"2026-10-16 20:00:00","HardcoreMode":false,"AchievementID":10,"Title":"Second","Points":10}]`))
		case strings.HasSuffix(r.URL.Path, "API_GetUserCompletionProgress.php"):
			_, _ = w.Write([]byte(`{"Count":1,"Total":1,"Results":[{"GameID":3,"Title":"A Game","ImageIcon":"/Images/1.png",
				"ConsoleName":"SNES","MaxPossible":20,"NumAwarded":5,"NumAwardedHardcore":4,"HighestAwardKind":"beaten-hardcore"}]}`))
		}
	}))
}

func TestReadingSomeonesProgress(t *testing.T) {
	srv := fakeRA(t, "good-key")
	defer srv.Close()
	c := Client{Base: srv.URL, UserAgent: "LANcast/0.9.88"}
	ctx := context.Background()

	p, err := c.Profile(ctx, "Player1", "good-key")
	if err != nil || p.ULID != "01FAKEULID0000000000000000" || p.TotalPoints != 1200 {
		t.Fatalf("profile = %+v, %v", p, err)
	}
	recent, err := c.Recent(ctx, p.ULID, "good-key", 14*24*time.Hour)
	if err != nil || len(recent) != 2 || !bool(recent[0].HardcoreMode) || bool(recent[1].HardcoreMode) {
		t.Fatalf("recent = %+v, %v", recent, err)
	}
	games, err := c.Completion(ctx, p.ULID, "good-key", 100)
	if err != nil || len(games) != 1 || games[0].NumAwarded != 5 || games[0].HighestAwardKind != "beaten-hardcore" {
		t.Fatalf("games = %+v, %v", games, err)
	}
}

func TestAWrongKeyOrNameIsRefused(t *testing.T) {
	srv := fakeRA(t, "good-key")
	defer srv.Close()
	c := Client{Base: srv.URL, UserAgent: "LANcast/0.9.88"}
	if _, err := c.Profile(context.Background(), "Player1", "bad-key"); !errors.Is(err, ErrRefused) {
		t.Errorf("bad key: %v", err)
	}
	if _, err := c.Profile(context.Background(), "Nobody", "good-key"); !errors.Is(err, ErrRefused) {
		t.Errorf("unknown user: %v", err)
	}
}

// An unreachable RetroAchievements is reported without the URL, which
// carries the person's key.
func TestErrorsNeverCarryTheKey(t *testing.T) {
	c := Client{Base: "http://127.0.0.1:1", HTTP: &http.Client{Timeout: time.Second}}
	_, err := c.Profile(context.Background(), "Player1", "secret-key-123")
	if err == nil || strings.Contains(err.Error(), "secret-key-123") {
		t.Errorf("err = %v", err)
	}
}

func TestOnlyPicturePathsAreFetched(t *testing.T) {
	for p, want := range map[string]bool{
		"/UserPic/Player1.png":       true,
		"/Badge/123.png":           true,
		"/Images/000001.png":       true,
		"/Badge/123.jpg":           false,
		"/Badge/../secret.png":     false,
		"/API/API_GetUser.php":     false,
		"/UserPic/a b.png":         false,
		"https://evil/Badge/1.png": false,
	} {
		if got := MediaPath(p); got != want {
			t.Errorf("MediaPath(%q) = %v, want %v", p, got, want)
		}
	}
}
