package api

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

/*
 * An episode's collision says which episode it is (R13-4).
 *
 * Every episode of a show carries the show's external_id, so two episodes that
 * each collide arrive as two collisions with the same (provider, external_id).
 * The spec documents season and episode on Collision for exactly this; the
 * handler built its own struct without them, so a client could not tell the
 * two apart, and the report's Compare showed one episode's card another
 * episode's files.
 */
func TestCollisionsCarryTheEpisodeTheyAreAbout(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	for _, ep := range []int{1, 5} {
		for _, copy := range []string{"a", "b"} {
			season, episode := 1, ep
			path := filepath.Join(h.dir, "s01e0"+itoa(int64(ep))+copy+".mkv")
			id, err := h.st.UpsertItem(ctx, store.ScanFile{
				LibraryID: h.lib.ID, Path: path, Kind: "episode",
				Title: "Episode", SortTitle: "black books", Container: "mkv",
				Season: &season, Episode: &episode, SizeBytes: 500, MTime: 1,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := h.st.SetMatch(ctx, id, "tmdb", "903", "matched", 1); err != nil {
				t.Fatal(err)
			}
		}
	}

	resp := h.do(t, "GET", "/api/collisions", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body struct {
		Collisions []struct {
			ExternalID string `json:"external_id"`
			Season     *int64 `json:"season"`
			Episode    *int64 `json:"episode"`
			Members    []struct {
				ID int64 `json:"id"`
			} `json:"members"`
		} `json:"collisions"`
	}
	decode(t, resp, &body)

	if len(body.Collisions) != 2 {
		t.Fatalf("got %d collisions, want one per episode: %+v", len(body.Collisions), body.Collisions)
	}
	seen := map[int64]bool{}
	for _, c := range body.Collisions {
		if c.Season == nil || c.Episode == nil {
			t.Fatalf("collision for %s arrived without its season and episode", c.ExternalID)
		}
		if *c.Season != 1 {
			t.Errorf("season = %d, want 1", *c.Season)
		}
		seen[*c.Episode] = true
	}
	if !seen[1] || !seen[5] {
		t.Errorf("episodes reported = %v, want 1 and 5", seen)
	}
}
