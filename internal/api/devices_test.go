package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"lancast/internal/auth"
)

const phoneUA = "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Mobile Safari/537.36"

// loginFrom signs in as a device with a given user agent, and returns its
// cookie.
func (h *harness) loginFrom(t *testing.T, username, password, ua string) *http.Cookie {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"username": username, "password": password})
	req, err := http.NewRequest("POST", h.srv.URL+"/api/auth/login", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", ua)
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c
		}
	}
	t.Fatalf("login from %q set no cookie (status %d)", ua, resp.StatusCode)
	return nil
}

type sessionsBody struct {
	Sessions []struct {
		ID       string `json:"id"`
		UserName string `json:"user_name"`
		Device   string `json:"device"`
		Address  string `json:"address"`
		Current  bool   `json:"current"`
	} `json:"sessions"`
}

/*
 * Authorized devices: a person sees every device signed in as them, named so
 * they can recognise it, with the one they are using marked — and can sign
 * one out, which really ends it.
 */
func TestYourDevicesAreListedAndCanBeSignedOut(t *testing.T) {
	h := langHarness(t) // an admin, signed in once by setup
	phone := h.loginFrom(t, testUser, "a long enough password", phoneUA)

	var mine sessionsBody
	decode(t, h.doAs(t, phone, "GET", "/api/profile/sessions", nil), &mine)
	if len(mine.Sessions) != 2 {
		t.Fatalf("sessions = %+v, want the setup session and the phone", mine.Sessions)
	}
	var phoneID, otherID string
	for _, s := range mine.Sessions {
		if s.Current {
			phoneID = s.ID
			if s.Device != "Chrome on Android" {
				t.Errorf("the phone is listed as %q", s.Device)
			}
			if s.Address == "" {
				t.Error("no address recorded")
			}
		} else {
			otherID = s.ID
		}
	}
	if phoneID == "" || otherID == "" || len(phoneID) != 16 {
		t.Fatalf("current not marked, or handles wrong: %+v", mine.Sessions)
	}

	// Sign out the other device from the phone; the other is really gone.
	resp := h.doAs(t, phone, "DELETE", "/api/profile/sessions/"+otherID, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("sign out status = %d", resp.StatusCode)
	}
	gone := h.authed(t, "GET", "/api/profile/sessions", nil)
	gone.Body.Close()
	if gone.StatusCode != http.StatusUnauthorized {
		t.Errorf("the signed-out device still works: %d", gone.StatusCode)
	}

	// Signing out the device asking is signing out: its cookie is cleared.
	resp = h.doAs(t, phone, "DELETE", "/api/profile/sessions/"+phoneID, nil)
	resp.Body.Close()
	cleared := false
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if resp.StatusCode != http.StatusNoContent || !cleared {
		t.Errorf("signing out this device: status %d, cookie cleared %v", resp.StatusCode, cleared)
	}
}

func TestAnotherAccountsDeviceIsNotYours(t *testing.T) {
	h := langHarness(t)
	member := h.addMember(t, "viewer", "correct horse battery")

	var all sessionsBody
	decode(t, h.authed(t, "GET", "/api/sessions", nil), &all)
	var adminID string
	for _, s := range all.Sessions {
		if s.UserName == testUser {
			adminID = s.ID
		}
	}
	if adminID == "" || len(all.Sessions) < 2 {
		t.Fatalf("the admin's list = %+v", all.Sessions)
	}

	// A member cannot reach another account's device, nor the server-wide list.
	resp := h.doAs(t, member, "DELETE", "/api/profile/sessions/"+adminID, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("a member signed out the admin's device: %d", resp.StatusCode)
	}
	resp = h.doAs(t, member, "GET", "/api/sessions", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a member read every account's devices: %d", resp.StatusCode)
	}

	// The admin's own session still works after that attempt.
	ok := h.authed(t, "GET", "/api/profile/sessions", nil)
	ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Errorf("the admin was signed out by a member's request: %d", ok.StatusCode)
	}
}
