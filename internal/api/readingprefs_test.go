package api

import (
	"net/http"
	"testing"
)

// Reading and listening defaults follow the account (settings-3-plan step 4):
// nothing chosen at first, each field changed on its own, the caller's own,
// and nothing stored that the players could not use.
type readingPrefsBody struct {
	Account     bool     `json:"account"`
	BookSpeed   *float64 `json:"book_speed"`
	BookSleep   *string  `json:"book_sleep"`
	ComicLayout *string  `json:"comic_layout"`
	ComicFit    *string  `json:"comic_fit"`
}

func TestReadingPreferencesArePerAccountAndPartial(t *testing.T) {
	h := langHarness(t)
	member := h.addMember(t, "viewer", "correct horse battery")

	var got readingPrefsBody
	decode(t, h.authed(t, "GET", "/api/profile/reading", nil), &got)
	if !got.Account || got.BookSpeed != nil || got.BookSleep != nil || got.ComicLayout != nil || got.ComicFit != nil {
		t.Fatalf("a fresh account has chosen nothing: %+v", got)
	}

	decode(t, h.authed(t, "PUT", "/api/profile/reading", map[string]any{
		"book_speed": 1.5, "comic_layout": "spread",
	}), &got)
	if got.BookSpeed == nil || *got.BookSpeed != 1.5 || got.ComicLayout == nil || *got.ComicLayout != "spread" {
		t.Fatalf("PUT answered %+v", got)
	}

	decode(t, h.authed(t, "PUT", "/api/profile/reading", map[string]any{"book_sleep": "chapter"}), &got)
	if got.BookSpeed == nil || *got.BookSpeed != 1.5 || got.BookSleep == nil || *got.BookSleep != "chapter" {
		t.Errorf("a sleep-only PUT changed other fields: %+v", got)
	}

	var theirs readingPrefsBody
	decode(t, h.doAs(t, member, "GET", "/api/profile/reading", nil), &theirs)
	if theirs.BookSpeed != nil || theirs.BookSleep != nil || theirs.ComicLayout != nil {
		t.Errorf("one account's choice reached another: %+v", theirs)
	}
}

func TestReadingPreferencesRefuseWhatThePlayersCannotUse(t *testing.T) {
	h := langHarness(t)
	for _, body := range []map[string]any{
		{},
		{"book_speed": 1.1},
		{"book_speed": 3},
		{"book_sleep": "90"},
		{"comic_layout": "scroll"},
		{"comic_fit": "height"},
	} {
		resp := h.authed(t, "PUT", "/api/profile/reading", body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("PUT %v = %d, want 400", body, resp.StatusCode)
		}
	}
	var got readingPrefsBody
	decode(t, h.authed(t, "GET", "/api/profile/reading", nil), &got)
	if got.BookSpeed != nil || got.BookSleep != nil || got.ComicLayout != nil || got.ComicFit != nil {
		t.Errorf("a refused PUT stored something: %+v", got)
	}
}

func TestReadingPreferencesWithNoAccounts(t *testing.T) {
	h := newHarness(t)
	var got readingPrefsBody
	decode(t, h.do(t, "GET", "/api/profile/reading", nil), &got)
	if got.Account {
		t.Error("account = true on a server with no accounts")
	}
	resp := h.do(t, "PUT", "/api/profile/reading", map[string]any{"comic_fit": "width"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("PUT with no accounts = %d, want 409", resp.StatusCode)
	}
}
