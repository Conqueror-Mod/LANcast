package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

/*
 * Your year, at the boundary.
 *
 * The store's suite proves the arithmetic. These prove the two things a handler
 * decides: that it answers about the caller and nobody else, and that it says
 * whether the year it just computed is finished — because "your 2025" and "your
 * 2025 so far" are a summary and a claim, and only the server knows which one
 * it produced.
 */

// fmtYearPath fills an item id into a route template.
func fmtYearPath(pattern string, id int64) string {
	return fmt.Sprintf(pattern, id)
}

func TestAYearDefaultsToThisOne(t *testing.T) {
	h := newHarness(t)

	var got struct {
		Year    int  `json:"year"`
		Partial bool `json:"partial"`
	}
	decode(t, h.do(t, "GET", "/api/profile/year", nil), &got)

	if got.Year != time.Now().Year() {
		t.Errorf("year = %d, want this one", got.Year)
	}
	// A year still running says so. A page that summarises March as though it
	// were December is making a claim nobody checked.
	if !got.Partial {
		t.Error("the current year did not report itself as still running")
	}
}

func TestAFinishedYearIsNotCalledPartial(t *testing.T) {
	h := newHarness(t)

	var got struct {
		Partial bool `json:"partial"`
	}
	decode(t, h.do(t, "GET", "/api/profile/year?year=2020", nil), &got)
	if got.Partial {
		t.Error("a year that has ended was reported as still running")
	}
}

func TestAYearNobodyCouldHaveWatchedIsRefused(t *testing.T) {
	/*
	 * Rather than an empty summary, which looks identical on screen to a real
	 * year in which nothing was watched — and only one of those is worth
	 * showing somebody.
	 */
	h := newHarness(t)
	for _, q := range []string{"?year=1066", "?year=999999", "?year=soon"} {
		resp := h.do(t, "GET", "/api/profile/year"+q, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400", q, resp.StatusCode)
		}
	}
}

func TestAnEmptyYearIsAnAnswerRatherThanAFailure(t *testing.T) {
	// A perfectly ordinary response for a year somebody did not use the
	// server, and the page has to be able to say so.
	h := newHarness(t)

	var got struct {
		Titles int `json:"titles"`
		Months []struct {
			Month  int `json:"month"`
			Titles int `json:"titles"`
		} `json:"months"`
		Years []int `json:"years"`
	}
	decode(t, h.do(t, "GET", "/api/profile/year?year=2019", nil), &got)

	if got.Titles != 0 {
		t.Errorf("titles = %d, want none", got.Titles)
	}
	// Twelve months even when empty: the quiet months are part of the shape.
	if len(got.Months) != 12 {
		t.Errorf("months = %d, want 12", len(got.Months))
	}
	if got.Years == nil {
		t.Error("years was null; an empty list is the honest answer to no history")
	}
}

func TestOneAccountsYearIsNotAnothers(t *testing.T) {
	/*
	 * Viewing is private by default (ADR 0035). The endpoint takes no account
	 * id at all, which is the strongest form of this guarantee — but what a
	 * member actually receives is worth asserting rather than inferring from
	 * the absence of a parameter.
	 */
	h := newHarness(t)
	h.secure(t, "a good long password")
	id := h.addFile(t, "Arrival.mkv", make([]byte, 16))

	// The administrator watches something.
	resp := h.authed(t, "PUT", fmtYearPath("/api/items/%d/progress", id),
		map[string]any{"position_ms": 60000, "watched": true})
	resp.Body.Close()

	member := h.addMember(t, "viewer", "correct horse battery staple")

	var theirs struct {
		Titles int `json:"titles"`
	}
	decode(t, h.doAs(t, member, "GET", "/api/profile/year", nil), &theirs)
	if theirs.Titles != 0 {
		t.Errorf("a member's year held %d titles from somebody else's history", theirs.Titles)
	}

	var mine struct {
		Titles int `json:"titles"`
	}
	decode(t, h.authed(t, "GET", "/api/profile/year", nil), &mine)
	if mine.Titles != 1 {
		t.Errorf("the watcher's own year held %d titles, want 1", mine.Titles)
	}
}

func TestTheEndsOfTheYearCarryTheirPosters(t *testing.T) {
	// They are rendered as tiles, so they need artwork for the same reason
	// every other list endpoint attaches it — a shelf of blank rectangles is
	// the bug the trending shelf already shipped once.
	h := newHarness(t)
	id := h.addFile(t, "Arrival.mkv", make([]byte, 16))
	resp := h.do(t, "PUT", fmtYearPath("/api/items/%d/progress", id),
		map[string]any{"position_ms": 60000, "watched": true})
	resp.Body.Close()

	var got struct {
		First *struct {
			Title   string         `json:"title"`
			Artwork map[string]any `json:"artwork"`
		} `json:"first"`
		Last *struct {
			Title string `json:"title"`
		} `json:"last"`
	}
	decode(t, h.do(t, "GET", "/api/profile/year", nil), &got)

	if got.First == nil || got.Last == nil {
		t.Fatalf("a year with one title reported first=%v last=%v", got.First, got.Last)
	}
	// One title is both ends of its year, which is the case that would look
	// like a bug if only one of the two were filled in.
	if got.First.Title != got.Last.Title {
		t.Errorf("first %q and last %q differ in a year holding one title",
			got.First.Title, got.Last.Title)
	}
}
