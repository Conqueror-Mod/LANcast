package api

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

// Home follows the account (ADR 0120): nothing chosen at first, each field
// changed on its own, the caller's own, and nothing stored that Home could
// not use.
type homePrefsBody struct {
	Account       bool     `json:"account"`
	Spotlight     *string  `json:"spotlight"`
	SpotlightItem *int64   `json:"spotlight_item"`
	Spoilers      *string  `json:"spoilers"`
	HiddenShelves []string `json:"hidden_shelves"`
}

func TestHomePreferencesArePerAccountAndPartial(t *testing.T) {
	h := langHarness(t)
	member := h.addMember(t, "viewer", "correct horse battery")

	var got homePrefsBody
	decode(t, h.authed(t, "GET", "/api/profile/home", nil), &got)
	if !got.Account || got.Spotlight != nil || got.Spoilers != nil || got.SpotlightItem != nil {
		t.Fatalf("a fresh account has chosen nothing: %+v", got)
	}
	if got.HiddenShelves == nil {
		t.Error("hidden_shelves is null; it must be a list")
	}

	unwatched := fmt.Sprintf("unwatched:%d", h.lib.ID)
	decode(t, h.authed(t, "PUT", "/api/profile/home", map[string]any{
		"spotlight": "pinned", "spotlight_item": 7, "hidden_shelves": []string{"rate", unwatched},
	}), &got)
	if got.Spotlight == nil || *got.Spotlight != "pinned" || got.SpotlightItem == nil || *got.SpotlightItem != 7 {
		t.Fatalf("PUT answered %+v", got)
	}
	if want := []string{"rate", unwatched}; !reflect.DeepEqual(got.HiddenShelves, want) {
		t.Errorf("hidden = %v, want %v", got.HiddenShelves, want)
	}

	// Spoilers alone leaves everything else as it was.
	decode(t, h.authed(t, "PUT", "/api/profile/home", map[string]any{"spoilers": "all"}), &got)
	if got.Spotlight == nil || *got.Spotlight != "pinned" || len(got.HiddenShelves) != 2 {
		t.Errorf("a spoilers-only PUT changed other fields: %+v", got)
	}

	// Another account is untouched.
	var theirs homePrefsBody
	decode(t, h.doAs(t, member, "GET", "/api/profile/home", nil), &theirs)
	if theirs.Spotlight != nil || theirs.Spoilers != nil || len(theirs.HiddenShelves) != 0 {
		t.Errorf("one account's choice reached another: %+v", theirs)
	}
}

func TestHomePreferencesRefuseWhatHomeCannotUse(t *testing.T) {
	h := langHarness(t)
	for _, body := range []map[string]any{
		{},
		{"spotlight": "everything"},
		{"spoilers": "some"},
		{"spotlight_item": -1},
		{"hidden_shelves": []string{"continue"}},
		{"hidden_shelves": []string{"unwatched:abc"}},
		{"hidden_shelves": []string{"unwatched:007"}},
		{"hidden_shelves": []string{"unwatched:999999"}},
	} {
		resp := h.authed(t, "PUT", "/api/profile/home", body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("PUT %v = %d, want 400", body, resp.StatusCode)
		}
	}
	var got homePrefsBody
	decode(t, h.authed(t, "GET", "/api/profile/home", nil), &got)
	if got.Spotlight != nil || got.Spoilers != nil || len(got.HiddenShelves) != 0 {
		t.Errorf("a refused PUT stored something: %+v", got)
	}
}

func TestHomePreferencesWithNoAccounts(t *testing.T) {
	h := newHarness(t)
	var got homePrefsBody
	decode(t, h.do(t, "GET", "/api/profile/home", nil), &got)
	if got.Account {
		t.Error("account = true on a server with no accounts; the client must keep choices on the device")
	}
	resp := h.do(t, "PUT", "/api/profile/home", map[string]any{"spoilers": "all"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("PUT with no accounts = %d, want 409", resp.StatusCode)
	}
}
