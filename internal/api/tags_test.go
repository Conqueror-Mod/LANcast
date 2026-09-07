package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"lancast/internal/store"
)

/*
 * Tags and favourites over HTTP (ADR 0062).
 *
 * The store is written so no caller can read another account's tags. These
 * tests are about the layer above it: that nothing here hands the store a
 * different account than the caller's, and that the API does not disclose
 * through a status code what the schema refuses to disclose through a row.
 */

func taggedItem(t *testing.T, h *harness) int64 {
	t.Helper()
	return h.addFile(t, "tagged.mkv", []byte("x"))
}

func tagNames(t *testing.T, resp *http.Response) []string {
	t.Helper()
	defer resp.Body.Close()
	var body struct {
		Tags []store.Tag `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(body.Tags))
	for _, tg := range body.Tags {
		out = append(out, tg.Name)
	}
	return out
}

// The ordinary path: tag an item, see it back, filter by it.
func TestTaggingAnItemAndReadingItBack(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	id := taggedItem(t, h)

	resp := h.authed(t, "POST", "/api/items/"+itoa(id)+"/tags",
		map[string]any{"name": "  Christmas  "})
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	resp.Body.Close()

	if got := tagNames(t, h.authed(t, "GET", "/api/items/"+itoa(id)+"/tags", nil)); len(got) != 1 || got[0] != "Christmas" {
		t.Fatalf("item tags = %v, want the trimmed spelling", got)
	}
	if got := tagNames(t, h.authed(t, "GET", "/api/tags", nil)); len(got) != 1 {
		t.Errorf("tag list = %v", got)
	}
}

// A nameless tag is refused: one nobody can type is one nobody can remove.
func TestANamelessTagIsRefused(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	id := taggedItem(t, h)

	resp := h.authed(t, "POST", "/api/items/"+itoa(id)+"/tags", map[string]any{"name": "   "})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

/*
 * Removing somebody else's tag answers 404, exactly as a tag that does not
 * exist does.
 *
 * A 403 would be a worse leak than the row itself: it answers "this id belongs
 * to another account" for anybody who cares to ask, which is the disclosure the
 * per-account vocabulary exists to prevent.
 */
func TestRemovingAnUnknownTagIsIndistinguishableFromAnothersTag(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	id := taggedItem(t, h)

	// A tag belonging to a different account, created directly so there is no
	// route through which this session could have made it.
	other, err := h.st.CreateUser(t.Context(), "", "someone-else", "hash", store.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := h.st.AddTag(t.Context(), other.ID, id, "sell these")
	if err != nil {
		t.Fatal(err)
	}

	theirResp := h.authed(t, "DELETE", "/api/items/"+itoa(id)+"/tags/"+itoa(theirs.ID), nil)
	defer theirResp.Body.Close()
	missingResp := h.authed(t, "DELETE", "/api/items/"+itoa(id)+"/tags/999999", nil)
	defer missingResp.Body.Close()

	if theirResp.StatusCode != http.StatusNotFound {
		t.Errorf("another account's tag: status = %d, want 404", theirResp.StatusCode)
	}
	if theirResp.StatusCode != missingResp.StatusCode {
		t.Errorf("another account's tag answered %d and a missing one %d — the "+
			"difference tells anybody who asks that the id is real",
			theirResp.StatusCode, missingResp.StatusCode)
	}
}

// And it is not visible on the item, or in the list, or in the grid.
func TestAnotherAccountsTagIsInvisibleThroughTheAPI(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	id := taggedItem(t, h)

	other, err := h.st.CreateUser(t.Context(), "", "someone-else", "hash", store.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := h.st.AddTag(t.Context(), other.ID, id, "sell these")
	if err != nil {
		t.Fatal(err)
	}

	if got := tagNames(t, h.authed(t, "GET", "/api/items/"+itoa(id)+"/tags", nil)); len(got) != 0 {
		t.Errorf("the item showed another account's tags: %v", got)
	}
	if got := tagNames(t, h.authed(t, "GET", "/api/tags", nil)); len(got) != 0 {
		t.Errorf("the tag list leaked %v — the existence is the private part", got)
	}

	resp := h.authed(t, "GET", "/api/items?library_id="+itoa(h.lib.ID)+"&tag="+itoa(theirs.ID), nil)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(raw), "tagged.mkv") {
		t.Error("filtering by another account's tag id returned their selection")
	}
}

// A favourite is the caller's own, and round-trips.
func TestAFavouriteRoundTrips(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	id := taggedItem(t, h)

	resp := h.authed(t, "PUT", "/api/items/"+itoa(id)+"/favourite",
		map[string]any{"favourite": true})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Favourite bool `json:"favourite"`
	}
	r2 := h.authed(t, "GET", "/api/items/"+itoa(id)+"/tags", nil)
	defer r2.Body.Close()
	if err := json.NewDecoder(r2.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Favourite {
		t.Error("the favourite did not stick")
	}
}
