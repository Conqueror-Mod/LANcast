package api

import (
	"context"
	"fmt"
	"testing"

	"lancast/internal/store"
)

/*
 * The shelf's tiles draw their posters from this response, so the items have
 * to come decorated like every other listing. They did not, once: the shelf
 * shipped with titles on blank tiles (reported 2026-10-17).
 */
func TestWhatToRateComesWithPosters(t *testing.T) {
	h := langHarness(t)
	ctx := context.Background()
	id := h.addFile(t, "film.mkv", []byte("x"))
	if err := h.st.PutArtwork(ctx, id, "poster-hash", "poster", "", 1, 1, 1); err != nil {
		t.Fatal(err)
	}
	if code := status(t, h.authed(t, "PUT", fmt.Sprintf("/api/items/%d/progress", id),
		map[string]any{"position_ms": 0, "watched": true})); code >= 300 {
		t.Fatalf("mark watched = %d", code)
	}
	var got struct {
		Items []store.Item `json:"items"`
	}
	decode(t, h.authed(t, "GET", "/api/profile/ratings/pending", nil), &got)
	if len(got.Items) != 1 || got.Items[0].ID != id {
		t.Fatalf("to rate = %+v", got.Items)
	}
	if a := got.Items[0].Artwork; a == nil || a.Poster != "poster-hash" {
		t.Errorf("artwork = %+v, want the poster", a)
	}
}
