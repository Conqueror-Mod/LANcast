package api

import (
	"context"
	"testing"
)

// The match_music switch decides what the enrichment queue holds (ADR 0115):
// off takes albums and artists out at once, on puts them back and starts a
// pass rather than waiting for the next scan.
func TestMatchMusicSwitchMovesTheQueue(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	lib, err := h.st.CreateLibrary(ctx, "Music", "music", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	artist, err := h.st.EnsureDerivedContainer(ctx, lib.ID, "artist", "k::artist=A", "A", "A", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.EnsureDerivedContainer(ctx, lib.ID, "album", "k::album=B", "B", "B", &artist); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	decode(t, h.do(t, "GET", "/api/settings", nil), &got)
	if got["match_music"] != true {
		t.Fatalf("match_music default = %v, want true", got["match_music"])
	}

	decode(t, h.do(t, "PUT", "/api/settings", map[string]any{"match_music": false}), &got)
	if n, _ := h.st.PendingCount(ctx); n != 0 {
		t.Errorf("off: %d pending, want 0", n)
	}
	before := h.enriched
	decode(t, h.do(t, "PUT", "/api/settings", map[string]any{"match_music": true}), &got)
	if n, _ := h.st.PendingCount(ctx); n != 2 {
		t.Errorf("on: %d pending, want the album and its artist", n)
	}
	if h.enriched == before {
		t.Error("switching it on did not start a pass")
	}
	if !h.settings.Get().MatchMusic {
		t.Error("the setting was not stored")
	}
}
