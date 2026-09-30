package api

import (
	"context"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

/*
 * Continue Watching's next_episode carries the episode's own artwork.
 *
 * The client's Next up shelf draws the episode a show would play next -- its
 * still, not the show's poster. Artwork was attached to the rows of this
 * response and next_episode is not a row, so the still was never sent and the
 * shelf could only have repeated the show's cover.
 */
func TestContinueSendsTheNextEpisodesStill(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	show, _, err := h.st.EnsureShow(ctx, h.lib.ID, filepath.Join(h.dir, "Show"), "A Show", "show")
	if err != nil {
		t.Fatal(err)
	}
	season, _, err := h.st.EnsureSeason(ctx, h.lib.ID, show, 1,
		filepath.Join(h.dir, "Show", "Season 1"), "Season 1", "season 1")
	if err != nil {
		t.Fatal(err)
	}
	series := "A Show"
	var eps []int64
	for i := 1; i <= 2; i++ {
		s, e := 1, i
		name := filepath.Join(h.dir, "Show", "Season 1", "e"+itoa(int64(i))+".mkv")
		id, err := h.st.UpsertItem(ctx, store.ScanFile{
			LibraryID: h.lib.ID, Path: name, Kind: "episode",
			Title: "Episode " + itoa(int64(i)), SortTitle: "a show",
			Series: &series, Season: &s, Episode: &e, Container: "mkv", SizeBytes: 1, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.st.SetParent(ctx, id, &season); err != nil {
			t.Fatal(err)
		}
		eps = append(eps, id)
	}
	if err := h.st.PutArtwork(ctx, eps[1], "still-hash", "thumb", "http://x/still.jpg", 1280, 720, 1); err != nil {
		t.Fatal(err)
	}

	resp := h.do(t, "PUT", "/api/items/"+itoa(eps[0])+"/progress",
		map[string]any{"position_ms": 1000, "watched": true})
	resp.Body.Close()

	var body struct {
		Items []struct {
			ID          int64 `json:"id"`
			NextEpisode *struct {
				ID      int64 `json:"id"`
				Artwork *struct {
					Thumb string `json:"thumb"`
				} `json:"artwork"`
			} `json:"next_episode"`
		} `json:"items"`
	}
	decode(t, h.do(t, "GET", "/api/continue", nil), &body)

	if len(body.Items) != 1 || body.Items[0].ID != show {
		t.Fatalf("continue = %+v, want the show", body.Items)
	}
	next := body.Items[0].NextEpisode
	if next == nil || next.ID != eps[1] {
		t.Fatalf("next_episode = %+v, want episode 2", next)
	}
	if next.Artwork == nil || next.Artwork.Thumb != "still-hash" {
		t.Errorf("next_episode.artwork = %+v, want the episode's still", next.Artwork)
	}
}
