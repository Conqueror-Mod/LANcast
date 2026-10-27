package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"lancast/internal/auth"
)

/*
 * Invites (ADR 0104), through the real router: the public paths, the admin
 * gate and the session a redemption starts are all middleware, which a call
 * to a handler would not see.
 */

type madeInvite struct {
	Invite struct {
		ID              string  `json:"id"`
		Note            string  `json:"note"`
		HiddenLibraries []int64 `json:"hidden_libraries"`
	} `json:"invite"`
	Token      string `json:"token"`
	PublicName string `json:"public_name"`
}

func makeInvite(t *testing.T, h *harness, body map[string]any) madeInvite {
	t.Helper()
	resp := h.authed(t, "POST", "/api/invites", body)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("make invite: %d %s", resp.StatusCode, raw)
	}
	var m madeInvite
	decode(t, resp, &m)
	if len(m.Token) < 40 {
		t.Fatalf("a secret of %d characters", len(m.Token))
	}
	return m
}

// The whole of it: an administrator makes a link, somebody with no account
// opens it, chooses a name and password, and is signed in as a member with
// the invite's limits already in force.
func TestAnInviteMakesAMemberAndSignsThemIn(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	ctx := context.Background()
	open, err := h.st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	private, err := h.st.CreateLibrary(ctx, "Home videos", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := makeInvite(t, h, map[string]any{
		"note": "for Georgia", "max_content_rating": "PG-13", "hidden_libraries": []int64{private.ID},
	})

	// Listed for the administrator, without the secret.
	resp := h.authed(t, "GET", "/api/invites", nil)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(raw), "for Georgia") || strings.Contains(string(raw), m.Token) ||
		strings.Contains(string(raw), auth.HashToken(m.Token)) {
		t.Fatalf("the list: %s", raw)
	}

	// Checked with no session: it works, and says who it is from.
	resp = h.do(t, "POST", "/api/invites/check", map[string]any{"token": m.Token})
	raw, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"from":"`+testUser+`"`) ||
		strings.Contains(string(raw), "Georgia") {
		t.Fatalf("check: %d %s", resp.StatusCode, raw)
	}

	// Redeemed with no session: a member, and a session cookie with it.
	resp = h.do(t, "POST", "/api/invites/redeem", map[string]any{
		"token": m.Token, "username": "georgia", "password": "her own good password",
	})
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("redeem: %d %s", resp.StatusCode, raw)
	}
	var session *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			session = c
		}
	}
	resp.Body.Close()
	if session == nil {
		t.Fatal("redeeming did not sign them in")
	}

	// As Georgia: a member, the private library kept from her.
	as := func(method, path string) (int, []byte) {
		req, _ := http.NewRequest(method, h.srv.URL+path, nil)
		req.AddCookie(session)
		resp, err := h.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, raw
	}
	code, raw := as("GET", "/api/auth/status")
	var st struct {
		Authenticated bool `json:"authenticated"`
		User          struct {
			Name string `json:"name"`
			Role string `json:"role"`
		} `json:"user"`
	}
	_ = json.Unmarshal(raw, &st)
	if code != http.StatusOK || !st.Authenticated || st.User.Name != "georgia" || st.User.Role != "member" {
		t.Fatalf("status as georgia: %d %s", code, raw)
	}
	_, raw = as("GET", "/api/libraries")
	if !strings.Contains(string(raw), `"Films"`) || strings.Contains(string(raw), "Home videos") {
		t.Fatalf("georgia's libraries: %s", raw)
	}
	u, _ := h.st.UserByName(ctx, "georgia")
	if u == nil || u.MaxContentRating != "PG-13" {
		t.Fatalf("georgia's ceiling: %+v", u)
	}
	_ = open

	// Spent: the same link again is the one unusable answer.
	resp = h.do(t, "POST", "/api/invites/redeem", map[string]any{
		"token": m.Token, "username": "someone", "password": "another good password",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a spent link: %d", resp.StatusCode)
	}
}

// Only an administrator makes, lists or withdraws invites; a member is
// refused, and nobody without a session gets near them.
func TestOnlyAnAdministratorInvites(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	m := h.member(t, "sam", "a good long password")
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/invites"}, {"GET", "/api/invites"}, {"DELETE", "/api/invites/i123"},
	} {
		resp := h.asUser(t, m, c.method, c.path, map[string]any{})
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("member %s %s: %d", c.method, c.path, resp.StatusCode)
		}
		resp = h.do(t, c.method, c.path, map[string]any{})
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("nobody %s %s: %d", c.method, c.path, resp.StatusCode)
		}
	}
}

// Bad requests are refused before an invite exists.
func TestAnInviteIsValidatedWhenMade(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	for name, body := range map[string]map[string]any{
		"unknown rating":  {"max_content_rating": "XYZ-99"},
		"unknown library": {"hidden_libraries": []int64{987654}},
		"long note":       {"note": strings.Repeat("x", 201)},
	} {
		resp := h.authed(t, "POST", "/api/invites", body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d", name, resp.StatusCode)
		}
	}
}

// A taken name or a short password costs the link nothing: try again.
func TestAMistakeDoesNotSpendTheLink(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	m := makeInvite(t, h, map[string]any{})
	for _, c := range []struct {
		body map[string]any
		want int
	}{
		{map[string]any{"token": m.Token, "username": testUser, "password": "a good long password"}, http.StatusConflict},
		{map[string]any{"token": m.Token, "username": "georgia", "password": "short"}, http.StatusBadRequest},
		{map[string]any{"token": m.Token, "username": " ", "password": "a good long password"}, http.StatusBadRequest},
		{map[string]any{"token": m.Token, "username": "georgia", "password": "a good long password"}, http.StatusCreated},
	} {
		resp := h.do(t, "POST", "/api/invites/redeem", c.body)
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Fatalf("%v: %d, want %d", c.body["username"], resp.StatusCode, c.want)
		}
	}
}

// Withdrawn means it stops working at once, and a run of guesses ends in 429.
func TestAWithdrawnLinkDiesAndGuessingIsThrottled(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	m := makeInvite(t, h, map[string]any{"note": "wrong person"})
	resp := h.authed(t, "DELETE", "/api/invites/"+m.Invite.ID, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("withdraw: %d", resp.StatusCode)
	}
	resp = h.do(t, "POST", "/api/invites/check", map[string]any{"token": m.Token})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a withdrawn link: %d", resp.StatusCode)
	}

	got429 := false
	for i := 0; i < 15; i++ {
		resp := h.do(t, "POST", "/api/invites/check", map[string]any{"token": "guess" + itoa(int64(i))})
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			got429 = true
			break
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("guess %d: %d", i, resp.StatusCode)
		}
	}
	if !got429 {
		t.Fatal("fifteen wrong guesses were never throttled")
	}
}
