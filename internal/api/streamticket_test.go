package api

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

/*
 * Stream tickets (ADR 0068). Each test is a promise the design makes: one
 * item, one path, reads only, and never outliving the credential that minted
 * it.
 */

func mintTicket(t *testing.T, h *harness, id int64) string {
	t.Helper()
	resp := h.authed(t, "POST", "/api/items/"+itoa(id)+"/stream-ticket", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("mint: status = %d %s", resp.StatusCode, b)
	}
	var st StreamTicket
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st.Ticket == "" || st.ItemID != id {
		t.Fatalf("ticket = %+v", st)
	}
	if left := time.Until(time.Unix(st.ExpiresAt, 0)); left < 23*time.Hour {
		t.Errorf("expires in %v; an overnight pause must not outlive the ticket", left)
	}
	return st.Ticket
}

func withTicket(t *testing.T, h *harness, method, path, ticket string) *http.Response {
	t.Helper()
	return h.request(t, method, path, "", "Ticket "+ticket, false, nil)
}

func TestTicketOpensItsOwnStream(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "movie.mkv", []byte("some bytes"))
	h.secure(t, "a good long password")
	tk := mintTicket(t, h, id)

	resp := withTicket(t, h, "GET", "/api/stream/"+itoa(id), tk)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if b, _ := io.ReadAll(resp.Body); string(b) != "some bytes" {
		t.Errorf("body = %q", b)
	}

	// Range is how mpv seeks; it must work under a ticket like anywhere else.
	req, _ := http.NewRequest("GET", h.srv.URL+"/api/stream/"+itoa(id), nil)
	req.Header.Set("Authorization", "Ticket "+tk)
	req.Header.Set("Range", "bytes=5-")
	ranged, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer ranged.Body.Close()
	if ranged.StatusCode != http.StatusPartialContent {
		t.Errorf("range status = %d, want 206", ranged.StatusCode)
	}
}

func TestTicketDoesNotOpenAnotherItem(t *testing.T) {
	h := newHarness(t)
	a := h.addFile(t, "a.mkv", []byte("a"))
	b := h.addFile(t, "b.mkv", []byte("b"))
	h.secure(t, "a good long password")
	tk := mintTicket(t, h, a)

	resp := withTicket(t, h, "GET", "/api/stream/"+itoa(b), tk)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for a different item", resp.StatusCode)
	}
}

func TestTicketIsNotACredentialAnywhereElse(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "movie.mkv", []byte("x"))
	h.secure(t, "a good long password")
	tk := mintTicket(t, h, id)

	for _, c := range []struct{ method, path string }{
		{"GET", "/api/libraries"},
		{"GET", "/api/items/" + itoa(id)},
		{"GET", "/api/items/" + itoa(id) + "/download"},
		{"GET", "/api/stream/" + itoa(id) + "/transcode"},
		{"GET", "/api/stream/" + itoa(id) + "/"},
		{"GET", "/api/stream/0" + itoa(id)},
		{"POST", "/api/items/" + itoa(id) + "/stream-ticket"},
	} {
		resp := withTicket(t, h, c.method, c.path, tk)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s = %d, want refused", c.method, c.path, resp.StatusCode)
		}
		if resp.StatusCode == http.StatusOK {
			t.Errorf("%s %s accepted a ticket", c.method, c.path)
		}
	}
}

func TestSigningOutEndsTheTicket(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "movie.mkv", []byte("x"))
	h.secure(t, "a good long password")
	tk := mintTicket(t, h, id)

	out := h.authed(t, "POST", "/api/auth/logout", nil)
	out.Body.Close()

	resp := withTicket(t, h, "GET", "/api/stream/"+itoa(id), tk)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 — the ticket outlived its session", resp.StatusCode)
	}
}

func TestPasswordChangeEndsTheTicket(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "movie.mkv", []byte("x"))
	h.secure(t, "a good long password")
	tk := mintTicket(t, h, id)

	resp := h.authed(t, "POST", "/api/auth/password", map[string]any{
		"current_password": "a good long password",
		"new_password":     "an even better password",
	})
	resp.Body.Close()

	after := withTicket(t, h, "GET", "/api/stream/"+itoa(id), tk)
	defer after.Body.Close()
	if after.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 — every session means every ticket too", after.StatusCode)
	}
}

func TestRubbishTicketIsUnauthorized(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "movie.mkv", []byte("x"))
	h.secure(t, "a good long password")

	resp := withTicket(t, h, "GET", "/api/stream/"+itoa(id), "not-a-ticket")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestMintingNeedsTheItemToBeVisible(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	resp := h.authed(t, "POST", "/api/items/999999/stream-ticket", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestTicketBookExpiresAndStaysBounded(t *testing.T) {
	b := newTicketBook()
	now := time.Now()
	b.add("old", ticket{itemID: 1, expires: now.Add(-time.Second)}, now)
	if _, ok := b.get("old", now); ok {
		t.Error("an expired ticket still resolved")
	}
	for i := 0; i < maxTickets+10; i++ {
		b.add(itoa(int64(i)), ticket{itemID: 1, expires: now.Add(time.Duration(i+1) * time.Second)}, now)
	}
	if len(b.m) > maxTickets {
		t.Errorf("book holds %d, cap is %d", len(b.m), maxTickets)
	}
	if _, ok := b.get(itoa(int64(maxTickets+9)), now); !ok {
		t.Error("the newest ticket was evicted instead of the oldest")
	}
}

func TestRevokingTheKeyEndsItsTicket(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "movie.mkv", []byte("x"))
	h.secure(t, "a good long password")
	secret, keyID := mint(t, h, "native player")

	resp := h.doKey(t, "POST", "/api/items/"+itoa(id)+"/stream-ticket", secret, nil)
	var st StreamTicket
	_ = json.NewDecoder(resp.Body).Decode(&st)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || st.Ticket == "" {
		t.Fatalf("mint by key: status = %d", resp.StatusCode)
	}

	ok := withTicket(t, h, "GET", "/api/stream/"+itoa(id), st.Ticket)
	ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 before revoking", ok.StatusCode)
	}

	del := h.authed(t, "DELETE", "/api/keys/"+keyID, nil)
	del.Body.Close()

	after := withTicket(t, h, "GET", "/api/stream/"+itoa(id), st.Ticket)
	defer after.Body.Close()
	if after.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 — the ticket outlived its key", after.StatusCode)
	}
}
