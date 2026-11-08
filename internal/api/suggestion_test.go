package api

import (
	"context"
	"net/http"
	"testing"

	"lancast/internal/store"
)

/*
 * GET /api/home/suggestion (ADR 0119 §1): the genre recent finishes share,
 * with films in it nobody has begun.
 */

type suggestionBody struct {
	Taste *struct {
		Genre  string `json:"genre"`
		Titles int    `json:"titles"`
	} `json:"taste"`
	Items []struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	} `json:"items"`
}

func (h *harness) genreFilm(t *testing.T, name string, genres ...string) int64 {
	t.Helper()
	id := h.addFile(t, name, []byte("x"))
	if err := h.st.ReplaceGenres(context.Background(), id, genres); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSuggestionComesFromWhatWasFinished(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	me := store.LocalUserID
	for _, n := range []string{"a.mkv", "b.mkv", "c.mkv"} {
		if err := h.st.SaveProgress(ctx, h.genreFilm(t, n, "Thriller"), me, 0, true); err != nil {
			t.Fatal(err)
		}
	}
	fresh := h.genreFilm(t, "fresh.mkv", "Thriller")
	begun := h.genreFilm(t, "begun.mkv", "Thriller")
	if err := h.st.SaveProgress(ctx, begun, me, 60_000, false); err != nil {
		t.Fatal(err)
	}
	h.genreFilm(t, "other.mkv", "Comedy")

	var body suggestionBody
	decode(t, h.do(t, http.MethodGet, "/api/home/suggestion?seed=7", nil), &body)

	if body.Taste == nil || body.Taste.Genre != "Thriller" || body.Taste.Titles != 3 {
		t.Fatalf("taste = %+v, want Thriller across 3 titles", body.Taste)
	}
	// Only the Thriller nobody has begun: not the three finished, not the one
	// part-watched (it is on Continue), and not the Comedy.
	if len(body.Items) != 1 || body.Items[0].ID != fresh {
		t.Errorf("items = %+v, want only the unstarted Thriller", body.Items)
	}
}

// Below three titles there is no pattern to claim, and nothing to show.
func TestSuggestionIsEmptyWithoutAPattern(t *testing.T) {
	h := newHarness(t)
	if err := h.st.SaveProgress(context.Background(), h.genreFilm(t, "a.mkv", "Western"),
		store.LocalUserID, 0, true); err != nil {
		t.Fatal(err)
	}
	h.genreFilm(t, "b.mkv", "Western")

	var body suggestionBody
	decode(t, h.do(t, http.MethodGet, "/api/home/suggestion", nil), &body)
	if body.Taste != nil || len(body.Items) != 0 {
		t.Errorf("body = %+v, want no taste and no items", body)
	}
}
