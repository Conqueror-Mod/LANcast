package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"lancast/internal/store"
)

/*
 * The end of a film: the next film in its collection first, marked with
 * continues_from, then similar films from outside the franchise, unmarked.
 * The ranking itself is the store's and tested there; this is the wire.
 */
func TestTheEndOfAFilmOffersTheNextAndSomethingSimilar(t *testing.T) {
	h := langHarness(t)
	ctx := context.Background()
	one := h.addFile(t, "one.mkv", []byte("1"))
	two := h.addFile(t, "two.mkv", []byte("2"))
	other := h.addFile(t, "other.mkv", []byte("3"))
	for _, id := range []int64{one, two, other} {
		if err := h.st.ReplaceGenres(ctx, id, []string{"Horror"}); err != nil {
			t.Fatal(err)
		}
	}
	coll, _, err := h.st.EnsureCollection(ctx, h.lib.ID, "tmdb", "1", "Saga", "Saga")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{one, two} {
		if err := h.st.AddToCollection(ctx, id, coll, 0); err != nil {
			t.Fatal(err)
		}
	}

	resp := h.authed(t, "GET", fmt.Sprintf("/api/items/%d/afterwards", one), nil)
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	var got struct {
		Items []store.Item `json:"items"`
	}
	decode(t, resp, &got)
	if len(got.Items) != 2 {
		t.Fatalf("afterwards = %+v, want the next film and one similar", got.Items)
	}
	if got.Items[0].ID != two || got.Items[0].ContinuesFrom == nil || got.Items[0].ContinuesFrom.AfterID != one {
		t.Errorf("first = %+v, want film two continuing from one", got.Items[0])
	}
	if got.Items[1].ID != other || got.Items[1].ContinuesFrom != nil {
		t.Errorf("second = %+v, want the film outside the collection, unmarked", got.Items[1])
	}

	if code := status(t, h.authed(t, "GET", "/api/items/999999/afterwards", nil)); code != http.StatusNotFound {
		t.Errorf("unknown item: %d, want 404", code)
	}
}
