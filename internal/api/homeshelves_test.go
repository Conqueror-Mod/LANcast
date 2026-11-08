package api

import (
	"fmt"
	"net/http"
	"testing"

	"lancast/internal/store"
)

/*
 * Pinned shelves over HTTP (ADR 0119 §2): pin, rename, move, unpin, and the
 * three refusals — a query the grid would not set, a library you cannot
 * open, and a thirteenth shelf.
 */

type shelvesBody struct {
	Shelves []store.HomeShelf `json:"shelves"`
	Max     int               `json:"max"`
}

func (h *harness) shelves(t *testing.T) shelvesBody {
	t.Helper()
	var b shelvesBody
	decode(t, h.do(t, http.MethodGet, "/api/home/shelves", nil), &b)
	return b
}

func (h *harness) pin(t *testing.T, title, query string) *http.Response {
	t.Helper()
	return h.do(t, http.MethodPost, "/api/home/shelves",
		map[string]any{"library_id": h.lib.ID, "title": title, "query": query})
}

func TestPinRenameMoveAndUnpinAShelf(t *testing.T) {
	h := newHarness(t)
	resp := h.pin(t, "  Eighties action  ", "genre=Action&decade=1980&watched=false")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("pin: status %d", resp.StatusCode)
	}
	var first store.HomeShelf
	decode(t, resp, &first)
	if first.Title != "Eighties action" {
		t.Errorf("title = %q; it should be trimmed", first.Title)
	}
	// Kept in a canonical order, so the same view pinned twice reads the same.
	if first.Query != "decade=1980&genre=Action&watched=false" {
		t.Errorf("query = %q", first.Query)
	}
	resp = h.pin(t, "Second", "sort=added")
	resp.Body.Close()

	b := h.shelves(t)
	if len(b.Shelves) != 2 || b.Max != store.MaxHomeShelves {
		t.Fatalf("shelves = %+v", b)
	}

	path := fmt.Sprintf("/api/home/shelves/%d", first.ID)
	resp = h.do(t, http.MethodPatch, path, map[string]any{"title": "Rewatch", "position": 1})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("patch: status %d", resp.StatusCode)
	}
	b = h.shelves(t)
	if b.Shelves[0].Title != "Second" || b.Shelves[1].Title != "Rewatch" {
		t.Errorf("after rename and move: %+v", b.Shelves)
	}

	resp = h.do(t, http.MethodDelete, path, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status %d", resp.StatusCode)
	}
	if b = h.shelves(t); len(b.Shelves) != 1 || b.Shelves[0].Title != "Second" {
		t.Errorf("after unpin: %+v", b.Shelves)
	}
	resp = h.do(t, http.MethodDelete, path, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unpinning twice: status %d, want 404", resp.StatusCode)
	}
}

// A shelf is a listing the grid could have shown, never a way to ask for more.
func TestAPinnedQueryIsOnlyWhatTheGridSets(t *testing.T) {
	h := newHarness(t)
	for _, q := range []string{"library_id=9", "parent_id=4", "limit=5000", "kind=photo", "%zz"} {
		resp := h.pin(t, "Sneaky", q)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("query %q: status %d, want 400", q, resp.StatusCode)
		}
	}
	for _, title := range []string{"", "   "} {
		resp := h.pin(t, title, "")
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("title %q: status %d, want 400", title, resp.StatusCode)
		}
	}
}

func TestPinningAnUnknownLibraryIsNotFound(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, http.MethodPost, "/api/home/shelves",
		map[string]any{"library_id": 999, "title": "Nowhere", "query": ""})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", resp.StatusCode)
	}
}

func TestATwelfthShelfIsTheLast(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < store.MaxHomeShelves; i++ {
		resp := h.pin(t, fmt.Sprint("Shelf ", i), "")
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("pin %d: status %d", i, resp.StatusCode)
		}
	}
	resp := h.pin(t, "One too many", "")
	var body map[string]map[string]string
	decode(t, resp, &body)
	if resp.StatusCode != http.StatusConflict || body["error"]["code"] != "shelf_limit" {
		t.Errorf("thirteenth: status %d, body %v", resp.StatusCode, body)
	}
}
