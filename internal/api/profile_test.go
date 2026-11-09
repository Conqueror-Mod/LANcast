package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"lancast/internal/store"
)

type profileBody struct {
	User       profileUser          `json:"user"`
	Stats      store.ProfileStats   `json:"stats"`
	History    []store.HistoryEntry `json:"history"`
	HasMore    bool                 `json:"has_more"`
	MostPlayed []store.PlayedItem   `json:"most_played"`
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

// The rows draw posters, so the posters come with them. Both lists shipped
// without, and every row drew the first letter of its title instead.
func TestProfileListsCarryPosters(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "Postered.mkv", []byte("x"))
	if err := h.st.PutArtwork(context.Background(), id, "posterhash", "poster", "u", 342, 513, 1); err != nil {
		t.Fatal(err)
	}
	h.do(t, "PUT", "/api/items/"+itoa(id)+"/progress",
		map[string]any{"position_ms": 1000}).Body.Close()
	h.do(t, "PUT", "/api/items/"+itoa(id)+"/rating", map[string]any{"score": 7}).Body.Close()

	body := h.profile(t, "")
	if len(body.History) != 1 {
		t.Fatalf("history has %d entries, want 1", len(body.History))
	}
	if a := body.History[0].Item.Artwork; a == nil || a.Poster != "posterhash" {
		t.Errorf("history item artwork = %+v, want poster %q", a, "posterhash")
	}

	var list struct {
		Ratings []store.RatedItem `json:"ratings"`
	}
	decode(t, h.do(t, "GET", "/api/profile/ratings", nil), &list)
	if len(list.Ratings) != 1 {
		t.Fatalf("ratings = %d, want 1", len(list.Ratings))
	}
	if a := list.Ratings[0].Item.Artwork; a == nil || a.Poster != "posterhash" {
		t.Errorf("rated item artwork = %+v, want poster %q", a, "posterhash")
	}
}

// The totals split by kind, and the titles finished most often, with their
// posters (profile-2-plan §4c).
func TestProfileSplitsTotalsByKindAndListsMostPlayed(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	id := h.addFile(t, "Twice.mkv", []byte("x"))
	if err := h.st.PutArtwork(ctx, id, "posterhash", "poster", "u", 342, 513, 1); err != nil {
		t.Fatal(err)
	}
	// Finished, started again, finished again: two plays.
	for _, finished := range []bool{true, false, true} {
		if err := h.st.SaveProgress(ctx, id, "local", 0, finished); err != nil {
			t.Fatal(err)
		}
	}

	body := h.profile(t, "")
	if len(body.Stats.ByKind) != 1 || body.Stats.ByKind[0].Kind != "movie" ||
		body.Stats.ByKind[0].Viewings != 2 {
		t.Errorf("by_kind = %+v, want one movie seen twice", body.Stats.ByKind)
	}
	if len(body.MostPlayed) != 1 || body.MostPlayed[0].Plays != 2 {
		t.Fatalf("most_played = %+v, want the film with 2 plays", body.MostPlayed)
	}
	if a := body.MostPlayed[0].Item.Artwork; a == nil || a.Poster != "posterhash" {
		t.Errorf("most played artwork = %+v, want its poster", a)
	}
}

// ?kind= narrows both histories, a kind it does not know is a 400 rather than
// everything, and /api/profile/kinds answers with empty lists, not nulls, and
// with its rows' posters (profile-2-plan §4d, §4e).
func TestProfileKindFilterAndKinds(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	id := h.addFile(t, "Film.mkv", []byte("x"))
	if err := h.st.SaveProgress(ctx, id, "local", 0, true); err != nil {
		t.Fatal(err)
	}

	if b := h.profile(t, "?kind=video"); len(b.History) != 1 {
		t.Errorf("video history = %d rows, want the film", len(b.History))
	}
	if b := h.profile(t, "?kind=music"); len(b.History) != 0 {
		t.Errorf("music history = %d rows, want none", len(b.History))
	}
	for _, path := range []string{"/api/profile?kind=games", "/api/profile/viewings?kind=film"} {
		if resp := h.do(t, "GET", path, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", path, resp.StatusCode)
		}
	}
	resp := h.do(t, "GET", "/api/profile/viewings?kind=video", nil)
	var log struct {
		Total int `json:"total"`
	}
	decode(t, resp, &log)
	if log.Total != 1 {
		t.Errorf("video log total = %d, want 1", log.Total)
	}

	resp = h.do(t, "GET", "/api/profile/kinds", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("kinds = %d", resp.StatusCode)
	}
	var k map[string]json.RawMessage
	decode(t, resp, &k)
	for _, want := range []string{"books", "comics", "games"} {
		if string(k[want]) == "null" || k[want] == nil {
			t.Errorf("%s = %s; want a value, never null", want, k[want])
		}
	}
	if string(k["games"]) != "[]" {
		t.Errorf("games = %s, want []", k["games"])
	}
}
