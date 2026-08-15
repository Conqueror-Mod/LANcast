package api

import (
	"net/http"
	"testing"

	"lancast/internal/store"
)

type profileBody struct {
	User    profileUser          `json:"user"`
	Stats   store.ProfileStats   `json:"stats"`
	History []store.HistoryEntry `json:"history"`
	HasMore bool                 `json:"has_more"`
}

func (h *harness) profile(t *testing.T, query string) profileBody {
	t.Helper()
	resp := h.do(t, "GET", "/api/profile"+query, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body profileBody
	decode(t, resp, &body)
	return body
}

// A server nobody has watched anything on answers with empty arrays and zeroes,
// not nulls — the client renders one shape.
func TestProfileEmpty(t *testing.T) {
	h := newHarness(t)
	body := h.profile(t, "")
	if body.History == nil {
		t.Error("history is null; want an empty array")
	}
	if body.Stats.Started != 0 || body.Stats.FirstAt != nil {
		t.Errorf("stats = %+v, want zeroes on a fresh server", body.Stats)
	}
	if body.User.Secured {
		t.Error("secured = true with no account configured")
	}
}

// Newest first, because a history read from the bottom is a list.
func TestProfileHistoryIsNewestFirst(t *testing.T) {
	h := newHarness(t)
	first := h.addFile(t, "First.mkv", []byte("x"))
	second := h.addFile(t, "Second.mkv", []byte("x"))

	h.do(t, "PUT", "/api/items/"+itoa(first)+"/progress",
		map[string]any{"position_ms": 1000}).Body.Close()
	h.do(t, "PUT", "/api/items/"+itoa(second)+"/progress",
		map[string]any{"position_ms": 2000}).Body.Close()

	body := h.profile(t, "")
	if len(body.History) != 2 {
		t.Fatalf("history has %d entries, want 2", len(body.History))
	}
	if body.History[0].Item.ID != second {
		t.Errorf("first entry is item %d, want the most recent (%d)",
			body.History[0].Item.ID, second)
	}
	if body.History[0].PositionMS != 2000 {
		t.Errorf("position_ms = %d, want 2000", body.History[0].PositionMS)
	}
}

// Something opened and abandoned is history; something never opened is not.
func TestProfileExcludesUntouchedItems(t *testing.T) {
	h := newHarness(t)
	h.addFile(t, "Never.mkv", []byte("x"))
	watched := h.addFile(t, "Watched.mkv", []byte("x"))
	h.do(t, "PUT", "/api/items/"+itoa(watched)+"/progress",
		map[string]any{"position_ms": 500}).Body.Close()

	body := h.profile(t, "")
	if len(body.History) != 1 || body.History[0].Item.ID != watched {
		t.Errorf("history = %+v, want only the item that was played", body.History)
	}
	if body.Stats.Started != 1 {
		t.Errorf("started = %d, want 1", body.Stats.Started)
	}
}

/*
 * The stat that is easy to get wrong.
 *
 * Summing the duration of everything touched would report two hours for a film
 * abandoned after ninety seconds. Time watched is how far in you actually got,
 * and only a finished item counts its whole runtime.
 */
func TestProfileCountsTimeWatchedNotTimeOwned(t *testing.T) {
	h := newHarness(t)
	abandoned := h.addFile(t, "Abandoned.mkv", []byte("x"))
	h.do(t, "PUT", "/api/items/"+itoa(abandoned)+"/progress",
		map[string]any{"position_ms": 90_000}).Body.Close()

	body := h.profile(t, "")
	if body.Stats.WatchedMS != 90_000 {
		t.Errorf("watched_ms = %d, want 90000 — the part actually watched",
			body.Stats.WatchedMS)
	}
	if body.Stats.Finished != 0 {
		t.Errorf("finished = %d, want 0", body.Stats.Finished)
	}
}

// The page must be able to tell "that is everything" from "that is the first
// fifty", and a full-looking page is not evidence either way.
func TestProfileReportsMorePages(t *testing.T) {
	h := newHarness(t)
	for _, n := range []string{"A.mkv", "B.mkv", "C.mkv"} {
		id := h.addFile(t, n, []byte("x"))
		h.do(t, "PUT", "/api/items/"+itoa(id)+"/progress",
			map[string]any{"position_ms": 100}).Body.Close()
	}

	page := h.profile(t, "?limit=2")
	if len(page.History) != 2 {
		t.Fatalf("history has %d entries, want 2", len(page.History))
	}
	if !page.HasMore {
		t.Error("has_more = false while a third entry exists")
	}
	if last := h.profile(t, "?limit=2&offset=2"); last.HasMore {
		t.Error("has_more = true on the final page")
	}
}
