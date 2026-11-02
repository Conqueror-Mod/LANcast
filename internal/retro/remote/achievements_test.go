package remote

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The server's answers, as rcheevos must see them (ADR 0112).
func TestAchievementsAnswers(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		reply      string
		wantStatus int
		wantBody   string // substring
		wantErr    bool
	}{
		{"passed through", 200, `{"status":200,"body":"{\"Success\":true}"}`, 200, `{"Success":true}`, false},
		{"RetroAchievements refused", 200, `{"status":401,"body":"{\"Success\":false}"}`, 401, `"Success":false`, false},
		{"queued award", 200, `{"status":200,"body":"{\"Success\":true,\"AchievementID\":5}","queued":true}`, 200, `"AchievementID":5`, false},
		{"earning off is a failed sign-in", 409, `{"error":{"code":"earning_off","message":"earning achievements is off"}}`, 401, `"Error":"earning achievements is off"`, false},
		{"refused by the server", 400, `{"error":{"code":"bad_request","message":"hardcore is not offered"}}`, 400, `hardcore is not offered`, false},
		{"upstream down is retried", 502, `{"error":{"code":"upstream","message":"unreachable"}}`, 0, "", true},
		{"unreadable answer is retried", 200, `not json`, 0, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got struct {
				Body   string `json:"body"`
				Clause string `json:"user_agent_clause"`
			}
			var auth, path, ctype string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				auth, path, ctype = r.Header.Get("Authorization"), r.URL.Path, r.Header.Get("Content-Type")
				b, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(b, &got)
				w.WriteHeader(c.status)
				io.WriteString(w, c.reply)
			}))
			defer srv.Close()
			g, err := New(srv.URL, "", 7, "secret")
			if err != nil {
				t.Fatal(err)
			}
			status, body, err := g.Achievements(context.Background(), "r=ping&u=lancast", "rcheevos/12.5")
			if auth != "Ticket secret" || path != "/api/items/7/retroachievements" || ctype != "application/json" {
				t.Errorf("request: auth %q path %q type %q", auth, path, ctype)
			}
			if got.Body != "r=ping&u=lancast" || got.Clause != "rcheevos/12.5" {
				t.Errorf("sent %+v", got)
			}
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v", err)
			}
			if status != c.wantStatus || !strings.Contains(string(body), c.wantBody) {
				t.Errorf("got %d %s", status, body)
			}
		})
	}
}
