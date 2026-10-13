package api

import (
	"fmt"
	"net/http"
	"testing"
)

// The watchlist at the boundary (ADR 0085): the caller's own, refused for a
// title they cannot see, and emptied by finishing.
func TestTheWatchlistIsYoursAndEmptiesItself(t *testing.T) {
	a := newAccessHarness(t)
	list := func(as func(string, string, any) *http.Response) []int64 {
		var page struct {
			Items []struct {
				ID int64 `json:"id"`
			} `json:"items"`
		}
		decode(t, as("GET", "/api/watchlist", nil), &page)
		out := []int64{}
		for _, it := range page.Items {
			out = append(out, it.ID)
		}
		return out
	}
	admin := func(m, p string, b any) *http.Response { return a.authed(t, m, p, b) }
	member := func(m, p string, b any) *http.Response { return a.doAs(t, a.member, m, p, b) }

	film := fmt.Sprintf("/api/items/%d/watchlist", a.openFilm)
	if code := status(t, admin("PUT", film, nil)); code != http.StatusNoContent {
		t.Fatalf("add = %d", code)
	}
	if got := list(admin); len(got) != 1 || got[0] != a.openFilm {
		t.Fatalf("admin's watchlist = %v", got)
	}
	if got := list(member); len(got) != 0 {
		t.Errorf("another account sees the admin's watchlist: %v", got)
	}

	// A title in a library kept from the member cannot be put on their list.
	if code := status(t, a.hide(t, a.private)); code != http.StatusOK {
		t.Fatal("hide failed")
	}
	if code := status(t, member("PUT", fmt.Sprintf("/api/items/%d/watchlist", a.privateFilm), nil)); code != http.StatusNotFound {
		t.Errorf("member adding a hidden title = %d, want 404", code)
	}

	// Finishing takes it off.
	if code := status(t, admin("PUT", fmt.Sprintf("/api/items/%d/progress", a.openFilm),
		map[string]any{"position_ms": 0, "watched": true})); code >= 300 {
		t.Fatalf("progress = %d", code)
	}
	if got := list(admin); len(got) != 0 {
		t.Errorf("a finished film is still on the watchlist: %v", got)
	}

	// Removing works, and removing again is not an error.
	_ = status(t, admin("PUT", film, nil))
	for i := 0; i < 2; i++ {
		if code := status(t, admin("DELETE", film, nil)); code != http.StatusNoContent {
			t.Errorf("remove #%d = %d", i+1, code)
		}
	}
}
