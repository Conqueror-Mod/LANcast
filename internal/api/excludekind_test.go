package api

import (
	"context"
	"testing"
)

// exclude_kind, which the browse grid uses to keep collections out of it.
//
// A collection groups films rather than being one, and a franchise tile beside
// its own members made a curated shelf read as an unsorted one. The grid asks
// for everything except collections; the collections page asks for exactly
// those. Both from the same endpoint, which is why the parameter exists rather
// than a second listing.
func TestExcludeKindKeepsCollectionsOutOfTheGrid(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	film := h.addFile(t, "film.mkv", []byte("f"))
	other := h.addFile(t, "other.mkv", []byte("o"))

	coll, err := h.st.EnsureDerivedContainer(ctx, h.lib.ID, "collection",
		h.dir+"::collection=Trilogy", "Trilogy", "trilogy", nil)
	if err != nil {
		t.Skipf("collections are not a derived container in this build: %v", err)
	}
	// A collection appears in the grid only once it groups two present members
	// (topLevelPredicate): a collection of one is a duplicate tile of the film
	// it contains.
	for i, id := range []int64{film, other} {
		if err := h.st.AddToCollection(ctx, id, coll, i); err != nil {
			t.Fatal(err)
		}
	}

	kinds := func(query string) map[string]int {
		resp := h.do(t, "GET", "/api/items?library_id="+itoa(h.lib.ID)+query, nil)
		var body struct {
			Items []struct {
				Kind string `json:"kind"`
			} `json:"items"`
		}
		decode(t, resp, &body)
		out := map[string]int{}
		for _, it := range body.Items {
			out[it.Kind]++
		}
		return out
	}

	if got := kinds(""); got["collection"] == 0 {
		t.Fatalf("the collection is not in the plain listing to begin with: %v", got)
	}
	got := kinds("&exclude_kind=collection")
	if got["collection"] != 0 {
		t.Errorf("exclude_kind left %d collections in the grid", got["collection"])
	}
	if got["movie"] == 0 {
		t.Error("exclude_kind removed the films as well")
	}

	// And the collections page asks the other way round.
	only := kinds("&kind=collection")
	if only["collection"] == 0 || only["movie"] != 0 {
		t.Errorf("kind=collection returned %v, want only collections", only)
	}
}
