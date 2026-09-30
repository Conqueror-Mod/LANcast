package api

import (
	"encoding/csv"
	"io"
	"net/http"
	"strings"
	"testing"

	"lancast/internal/store"
)

/*
 * The watch history over HTTP (ADR 0074): finishing a film through the
 * ordinary progress route logs it, and both export formats carry it.
 */
func TestAFinishedFilmIsInTheHistoryAndBothExports(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "a.mkv", make([]byte, 16))
	for _, watched := range []bool{false, true, true} {
		resp := h.do(t, "PUT", "/api/items/"+itoa(id)+"/progress",
			map[string]any{"position_ms": 5000, "watched": watched})
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("progress = %d", resp.StatusCode)
		}
	}

	var page struct {
		Viewings []store.Viewing `json:"viewings"`
		Total    int             `json:"total"`
	}
	decode(t, h.do(t, "GET", "/api/profile/viewings", nil), &page)
	if page.Total != 1 || len(page.Viewings) != 1 || page.Viewings[0].ItemID == nil || *page.Viewings[0].ItemID != id {
		t.Fatalf("history = %+v, want the one film once", page)
	}

	resp := h.do(t, "GET", "/api/profile/viewings/export?format=csv", nil)
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("Content-Disposition = %q, want an attachment", cd)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	records, err := csv.NewReader(resp.Body).ReadAll()
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0][0] != "watched_at" || records[1][1] != "movie" {
		t.Errorf("csv = %v, want a header and one movie row", records)
	}

	var trakt struct {
		Movies []struct {
			WatchedAt string `json:"watched_at"`
			Title     string `json:"title"`
		} `json:"movies"`
		Shows   []any `json:"shows"`
		Skipped int   `json:"skipped"`
	}
	decode(t, h.do(t, "GET", "/api/profile/viewings/export?format=trakt", nil), &trakt)
	if len(trakt.Movies) != 1 || !strings.HasSuffix(trakt.Movies[0].WatchedAt, "Z") {
		t.Errorf("trakt export = %+v, want one movie with a UTC time", trakt)
	}

	bad := h.do(t, "GET", "/api/profile/viewings/export?format=xml", nil)
	io.Copy(io.Discard, bad.Body)
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("an unknown format answered %d, want 400", bad.StatusCode)
	}
}

// Episodes group under their show and season; one with no numbers cannot be
// placed and is counted rather than dropped silently.
func TestTheTraktShapeGroupsEpisodesUnderTheirShow(t *testing.T) {
	i := func(n int) *int { return &n }
	s := func(v string) *string { return &v }
	got := traktHistory([]store.Viewing{
		{Kind: "episode", Title: "One", Series: s("Futurama"), Season: i(1), Episode: i(1), ShowIMDbID: s("tt1"), FinishedAt: 1},
		{Kind: "episode", Title: "Two", Series: s("Futurama"), Season: i(1), Episode: i(2), ShowIMDbID: s("tt1"), FinishedAt: 2},
		{Kind: "episode", Title: "Other", Series: s("Futurama"), Season: i(2), Episode: i(1), ShowIMDbID: s("tt1"), FinishedAt: 3},
		{Kind: "episode", Title: "Loose", Series: s("Futurama"), FinishedAt: 4},
		{Kind: "movie", Title: "Heat", Year: i(1995), IMDbID: s("tt0113277"), FinishedAt: 5},
	})
	if len(got.Shows) != 1 || len(got.Shows[0].Seasons) != 2 || len(got.Shows[0].Seasons[0].Episodes) != 2 {
		t.Errorf("shows = %+v, want one show with two seasons, the first holding two episodes", got.Shows)
	}
	if got.Skipped != 1 {
		t.Errorf("skipped = %d, want the episode with no numbers", got.Skipped)
	}
	if len(got.Movies) != 1 || got.Movies[0].IDs.IMDb != "tt0113277" {
		t.Errorf("movies = %+v", got.Movies)
	}
}
