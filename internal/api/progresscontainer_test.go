package api

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
)

/*
 * A progress write on something with no file is refused.
 *
 * Nothing reads a play state written against a show: Continue Watching and
 * Next up judge it by its episodes. Accepting the write is how the home page's
 * "Mark as watched" on a show reported success and changed nothing.
 */
func TestProgressOnAShowIsRefused(t *testing.T) {
	h := newHarness(t)
	show, _, err := h.st.EnsureShow(context.Background(), h.lib.ID,
		filepath.Join(h.dir, "A Show"), "A Show", "a show")
	if err != nil {
		t.Fatal(err)
	}
	wantError(t, h.do(t, "PUT", "/api/items/"+itoa(show)+"/progress",
		map[string]any{"position_ms": 0, "watched": true}), http.StatusBadRequest, "not_playable")

	// A film still takes one.
	film := h.addFile(t, "film.mkv", []byte("f"))
	resp := h.do(t, "PUT", "/api/items/"+itoa(film)+"/progress",
		map[string]any{"position_ms": 1000, "watched": false})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("a film's progress = %d, want 204", resp.StatusCode)
	}
}

// A listing by kind offers only what can be played, as the grid does. New
// Music asks for kind=artist, and a missing artist stayed there as a dead tile.
func TestAListingByKindLeavesOutMissingFiles(t *testing.T) {
	h := newHarness(t)
	here := h.addFile(t, "here.mkv", []byte("h"))
	gone := h.addFile(t, "gone.mkv", []byte("g"))
	if err := h.st.MarkMissing(context.Background(), []int64{gone}); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	decode(t, h.do(t, "GET", "/api/items?kind=movie", nil), &body)
	if len(body.Items) != 1 || body.Items[0].ID != here {
		t.Errorf("kind=movie listed %v, want only the film still on disk (%d)", body.Items, here)
	}
}
