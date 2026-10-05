package api

import (
	"context"
	"path/filepath"
	"testing"

	"lancast/internal/marker"
	"lancast/internal/store"
)

/*
 * An episode is finished at its credits, not at a percentage (ADR 0054,
 * decision 4). These pin the rule from the wire: what a player posts, and what
 * the item then says about itself.
 */

const (
	creditsTestDuration = 2_700_000 // 45 minutes
	creditsTestMarker   = 2_500_000 // 92.6%: inside the trusted window
)

// creditsItem adds a playable item of kind, 45 minutes long, carrying one
// credits marker from source at startMS (or none when source is empty).
func creditsItem(t *testing.T, h *harness, kind, name, source string, startMS int64) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := h.st.UpsertItem(ctx, store.ScanFile{
		LibraryID: h.lib.ID, Path: filepath.Join(h.dir, name), Kind: kind,
		Title: name, SortTitle: name, Container: "mkv", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.SaveProbe(ctx, id, store.ProbeResult{
		DurationMS: creditsTestDuration, VideoCodec: "h264", AudioCodec: "aac",
	}); err != nil {
		t.Fatal(err)
	}
	if source != "" {
		if err := h.st.SaveMarkers(ctx, id, []string{store.MarkerCredits}, []store.Marker{
			{Kind: store.MarkerCredits, StartMS: startMS, Source: source, Confidence: 1},
		}); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

// postProgress writes a position and returns what the item then says.
func postProgress(t *testing.T, h *harness, id, pos int64, watched bool) bool {
	t.Helper()
	resp := h.do(t, "PUT", "/api/items/"+itoa(id)+"/progress",
		map[string]any{"position_ms": pos, "watched": watched})
	if resp.StatusCode != 204 {
		t.Fatalf("progress status = %d, want 204", resp.StatusCode)
	}
	resp.Body.Close()
	var it store.Item
	decode(t, h.do(t, "GET", "/api/items/"+itoa(id), nil), &it)
	if it.Progress == nil {
		t.Fatal("no progress after a write")
	}
	return it.Progress.Watched
}

// The case the change exists for: four minutes of the last act left is not
// finished, even past 90% and even with the player's own 92% flag set.
func TestEpisodeIsNotWatchedBeforeItsCredits(t *testing.T) {
	for _, source := range []string{marker.Source, marker.SourceUngated, marker.SourceEnding} {
		t.Run(source, func(t *testing.T) {
			h := newHarness(t)
			id := creditsItem(t, h, "episode", "ep.mkv", source, creditsTestMarker)

			// 92% — past the percentage rule, and the flag the player sends.
			pos := int64(creditsTestDuration * 92 / 100)
			if postProgress(t, h, id, pos, true) {
				t.Errorf("watched at %d with credits at %d; an episode is finished at its credits", pos, creditsTestMarker)
			}
		})
	}
}

// At the credits — allowing the slack for a player that stops on the first
// card — it is finished, whether or not the client said so.
func TestEpisodeIsWatchedAtItsCredits(t *testing.T) {
	h := newHarness(t)
	id := creditsItem(t, h, "episode", "ep.mkv", marker.SourceEnding, creditsTestMarker)

	// Literal, not creditsSlackMS: a slack quietly set to zero must fail here.
	if postProgress(t, h, id, creditsTestMarker-15_001, false) {
		t.Errorf("watched more than 15 s before the credits")
	}
	if !postProgress(t, h, id, creditsTestMarker-10_000, false) {
		t.Errorf("not watched 10 s before the credits; a player stopping on the first card has finished")
	}
	if !postProgress(t, h, id, creditsTestMarker-15_000, false) {
		t.Errorf("not watched exactly 15 s before the credits")
	}
}

// "Mark as watched" posts position 0 with watched. It is somebody saying so,
// not a playhead, and the credits rule must not overrule it.
func TestMarkAsWatchedStandsOnAnEpisodeWithCredits(t *testing.T) {
	h := newHarness(t)
	id := creditsItem(t, h, "episode", "ep.mkv", marker.SourceEnding, creditsTestMarker)
	if !postProgress(t, h, id, 0, true) {
		t.Error("Mark as watched was overruled by the credits rule")
	}
}

// A film keeps the percentage. Its markers are late too often to be a finish
// line, so even a gated one does not move it.
func TestFilmKeepsThePercentageDespiteACreditsMarker(t *testing.T) {
	h := newHarness(t)
	id := creditsItem(t, h, "movie", "film.mkv", marker.Source, creditsTestMarker)
	pos := int64(creditsTestDuration * 91 / 100)
	if !postProgress(t, h, id, pos, false) {
		t.Errorf("film not watched at 91%%; films keep the percentage rule")
	}
}

// Without a marker the player would act on, an episode falls back to the
// percentage: no marker, one outside the window, or a source nobody trusts.
func TestEpisodeFallsBackWithoutATrustedMarker(t *testing.T) {
	cases := []struct {
		name, source string
		startMS      int64
	}{
		{"no marker", "", 0},
		{"marker too early", marker.SourceEnding, creditsTestDuration * 80 / 100},
		{"marker too late", marker.SourceEnding, creditsTestDuration * 995 / 1000},
		{"untrusted source", "chapter", creditsTestMarker},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			id := creditsItem(t, h, "episode", "ep.mkv", c.source, c.startMS)
			pos := int64(creditsTestDuration * 91 / 100)
			if !postProgress(t, h, id, pos, false) {
				t.Errorf("not watched at 91%% with %s; the percentage rule should apply", c.name)
			}
		})
	}
}
