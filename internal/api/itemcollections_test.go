package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"lancast/internal/store"
)

/*
 * "View collection" on a title page lists the collections the title is in
 * that are real ones: two or more present films. A provider's collection of
 * one is a duplicate tile of the film, and offering to "view" it would open a
 * page holding only the film you came from.
 */
func TestATitleNamesTheCollectionsItIsIn(t *testing.T) {
	h := langHarness(t)
	ctx := context.Background()
	a := h.addFile(t, "a.mkv", []byte("a"))
	b := h.addFile(t, "b.mkv", []byte("b"))
	lib := h.lib.ID

	real, _, err := h.st.EnsureCollection(ctx, lib, "tmdb", "1", "Saw Collection", "Saw Collection")
	if err != nil {
		t.Fatal(err)
	}
	lonely, _, err := h.st.EnsureCollection(ctx, lib, "tmdb", "2", "Lonely Collection", "Lonely Collection")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct{ item, col int64 }{{a, real}, {b, real}, {a, lonely}} {
		if err := h.st.AddToCollection(ctx, m.item, m.col, 0); err != nil {
			t.Fatal(err)
		}
	}

	var got struct {
		Items []store.Item `json:"items"`
	}
	decode(t, h.authed(t, "GET", fmt.Sprintf("/api/items/%d/collections", a), nil), &got)
	if len(got.Items) != 1 || got.Items[0].ID != real {
		t.Errorf("collections of a = %+v, want only the real one (%d)", got.Items, real)
	}

	if code := status(t, h.authed(t, "GET", "/api/items/999999/collections", nil)); code != http.StatusNotFound {
		t.Errorf("an unknown item = %d, want 404", code)
	}
}
