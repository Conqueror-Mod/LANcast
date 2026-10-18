package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lancast/internal/retro/ra"
	"lancast/internal/secret"
)

/*
 * RetroAchievements on the profile page (ADR 0090), against a fake
 * RetroAchievements. What matters most: the key is checked before it is
 * kept, never comes back, and pictures come through LANcast.
 */

const raGoodKey = "fake-key-0123456789"

func fakeRAServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/Badge/") || strings.HasPrefix(r.URL.Path, "/UserPic/") {
			_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nfake"))
			return
		}
		calls++
		if r.URL.Query().Get("y") != raGoodKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "API_GetUserProfile.php"):
			_, _ = w.Write([]byte(`{"User":"Player1","ULID":"01FAKEULID0000000000000000","UserPic":"/UserPic/Player1.png","TotalPoints":120}`))
		case strings.HasSuffix(r.URL.Path, "API_GetUserRecentAchievements.php"):
			_, _ = w.Write([]byte(`[{"Date":"2026-10-17 21:00:00","HardcoreMode":1,"AchievementID":9,"Title":"First","BadgeURL":"/Badge/123.png","Points":5,"GameTitle":"A Game","GameID":3,"ConsoleName":"SNES"}]`))
		case strings.HasSuffix(r.URL.Path, "API_GetUserCompletionProgress.php"):
			_, _ = w.Write([]byte(`{"Results":[{"GameID":3,"Title":"A Game","ImageIcon":"/Images/1.png","ConsoleName":"SNES","MaxPossible":20,"NumAwarded":5}]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func raHarness(t *testing.T) (*accessHarness, *int) {
	t.Helper()
	a := newAccessHarness(t)
	srv, calls := fakeRAServer(t)
	sealer, err := secret.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.srvAPI.SetRetroAchievements(&RetroAchievements{
		Sealer: sealer, Client: ra.Client{Base: srv.URL, UserAgent: "LANcast/test"},
		MediaDir: t.TempDir(), MediaURL: srv.URL,
	})
	return &a, calls
}

func TestConnectingRetroAchievements(t *testing.T) {
	a, calls := raHarness(t)

	var before raView
	decode(t, a.authed(t, "GET", "/api/profile/retroachievements", nil), &before)
	if !before.Available || before.Connected || *calls != 0 {
		t.Fatalf("before connecting: %+v, %d calls to RetroAchievements", before, *calls)
	}

	// A wrong key is refused and nothing is kept.
	if code := status(t, a.authed(t, "PUT", "/api/profile/retroachievements",
		map[string]any{"username": "Player1", "api_key": "wrong"})); code != http.StatusBadRequest {
		t.Errorf("a wrong key = %d, want 400", code)
	}

	resp := a.authed(t, "PUT", "/api/profile/retroachievements",
		map[string]any{"username": " Player1 ", "api_key": raGoodKey})
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("connect = %d %s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), raGoodKey) {
		t.Fatal("the API key came back in the answer")
	}
	var got raView
	decode(t, a.authed(t, "GET", "/api/profile/retroachievements", nil), &got)
	if !got.Connected || got.User != "Player1" || got.Points != 120 || len(got.Recent) != 1 || len(got.Games) != 1 {
		t.Fatalf("connected view = %+v", got)
	}
	// Pictures are LANcast's own URLs, never RetroAchievements'.
	if got.Avatar != "/api/retroachievements/media/UserPic/Player1.png" || got.Recent[0].Badge != "/api/retroachievements/media/Badge/123.png" {
		t.Errorf("pictures: avatar %q badge %q", got.Avatar, got.Recent[0].Badge)
	}

	// Stored sealed: the row holds no readable key.
	acct, _ := a.st.GetRetroAccount(context.Background(), "local")
	if acct == nil || strings.Contains(string(acct.APIKeySealed), raGoodKey) {
		t.Fatalf("stored: %+v", acct)
	}

	// Cached: another look does not ask RetroAchievements again.
	n := *calls
	decode(t, a.authed(t, "GET", "/api/profile/retroachievements", nil), &got)
	if *calls != n {
		t.Errorf("a second look asked RetroAchievements again (%d calls, then %d)", n, *calls)
	}

	// Somebody else's profile is theirs: the member has nothing connected.
	var theirs raView
	decode(t, a.doAs(t, a.member, "GET", "/api/profile/retroachievements", nil), &theirs)
	if theirs.Connected || theirs.User != "" {
		t.Errorf("another account sees %+v", theirs)
	}

	if code := status(t, a.authed(t, "DELETE", "/api/profile/retroachievements", nil)); code != http.StatusNoContent {
		t.Errorf("disconnect = %d", code)
	}
	if acct, _ := a.st.GetRetroAccount(context.Background(), "local"); acct != nil {
		t.Error("still stored after disconnecting")
	}
}

func TestRetroAchievementsPicturesComeThroughLANcast(t *testing.T) {
	a, _ := raHarness(t)
	resp := a.authed(t, "GET", "/api/retroachievements/media/Badge/123.png", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(string(body), "\x89PNG") || resp.Header.Get("Content-Type") != "image/png" {
		t.Errorf("badge = %d %q %s", resp.StatusCode, body, resp.Header.Get("Content-Type"))
	}
	for _, p := range []string{
		"/api/retroachievements/media/API/API_GetUserProfile.php",
		"/api/retroachievements/media/Badge/123.jpg",
		"/api/retroachievements/media/Other/1.png",
	} {
		if code := status(t, a.authed(t, "GET", p, nil)); code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", p, code)
		}
	}
}
