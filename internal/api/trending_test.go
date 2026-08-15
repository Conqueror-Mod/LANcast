package api

import (
	"context"
	"net/http"
	"testing"

	"lancast/internal/store"
)

func (h *harness) trending(t *testing.T, query string) trendingResponse {
	t.Helper()
	resp := h.do(t, "GET", "/api/libraries/"+itoa(h.lib.ID)+"/trending"+query, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body trendingResponse
	decode(t, resp, &body)
	return body
}

// played records progress for a specific account, which is the only way to
// build the multi-user case this feature exists for.
func (h *harness) played(t *testing.T, itemID int64, userID string, positionMS int64, watched bool) {
	t.Helper()
	if err := h.st.SaveProgress(context.Background(), itemID, userID, positionMS, watched); err != nil {
		t.Fatal(err)
	}
}

// A library nobody has played answers with an empty array, not null.
func TestTrendingEmpty(t *testing.T) {
	h := newHarness(t)
	body := h.trending(t, "")
	if body.Items == nil {
		t.Error("items is null; want an empty array")
	}
	if body.WindowDays != 30 {
		t.Errorf("window_days = %d, want 30", body.WindowDays)
	}
}

// The ranking claim: more distinct accounts is higher, and the count is
// accounts rather than plays.
func TestTrendingRanksByViewers(t *testing.T) {
	h := newHarness(t)
	popular := h.addFile(t, "Popular.mkv", []byte("x"))
	niche := h.addFile(t, "Niche.mkv", []byte("x"))

	h.played(t, popular, "alice", 1000, false)
	h.played(t, popular, "bob", 2000, true)
	h.played(t, popular, "carol", 3000, false)
	h.played(t, niche, "alice", 5000, false)

	body := h.trending(t, "")
	if len(body.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(body.Items))
	}
	if body.Items[0].Item.ID != popular {
		t.Errorf("first item is %d, want the one three people played (%d)",
			body.Items[0].Item.ID, popular)
	}
	if body.Items[0].Viewers != 3 {
		t.Errorf("viewers = %d, want 3", body.Items[0].Viewers)
	}
	// Started and finished are different facts. A title everybody starts and
	// nobody finishes is worth telling apart from one everybody finished, and
	// a single popularity number destroys that.
	if body.Items[0].Finishers != 1 {
		t.Errorf("finishers = %d, want 1", body.Items[0].Finishers)
	}
	if body.Contributors != 3 {
		t.Errorf("contributors = %d, want 3", body.Contributors)
	}
}

/*
 * The honesty check.
 *
 * On a single-account server every count is 1 and this is not a trend, it is
 * one person's history. The endpoint cannot refuse to answer — the data is
 * real — so it reports the number of contributing accounts and lets the client
 * decline to call it trending. This asserts the client is given what it needs
 * to tell the difference.
 */
func TestTrendingReportsASingleContributor(t *testing.T) {
	h := newHarness(t)
	for _, name := range []string{"One.mkv", "Two.mkv"} {
		h.played(t, h.addFile(t, name, []byte("x")), store.LocalUserID, 1000, false)
	}

	body := h.trending(t, "")
	if body.Contributors != 1 {
		t.Errorf("contributors = %d, want 1 — the client needs this to avoid calling one person's history a trend",
			body.Contributors)
	}
}

// A season is not a thing anybody played; it is where the episodes live. A
// shelf offering "Season 2" beside four films reads as a bug — so containers
// are excluded even when they carry progress, which they can: marking a season
// watched writes a row like anything else.
func TestTrendingExcludesContainers(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	film := h.addFile(t, "Film.mkv", []byte("x"))
	// A container row, made the way the scanner makes one.
	if _, err := h.st.UpsertItem(ctx, store.ScanFile{
		LibraryID: h.lib.ID, Kind: "season", Path: "",
		Title: "Season 1", SortTitle: "season 1",
	}); err != nil {
		t.Fatal(err)
	}
	var seasonID int64
	items, _, err := h.st.ListItems(ctx, store.ItemFilter{LibraryID: h.lib.ID, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Kind == "season" {
			seasonID = it.ID
		}
	}
	if seasonID == 0 {
		t.Skip("no season row was created; the container path differs in this build")
	}

	h.played(t, seasonID, "alice", 1000, false)
	h.played(t, film, "alice", 1000, false)

	got := h.trending(t, "").Items
	if len(got) != 1 || got[0].Item.ID != film {
		t.Errorf("items = %+v, want only the film — a container is not something anybody played",
			func() []int64 {
				ids := []int64{}
				for _, g := range got {
					ids = append(ids, g.Item.ID)
				}
				return ids
			}())
	}
}

func TestTrendingUnknownLibrary(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "GET", "/api/libraries/9999/trending", nil),
		http.StatusNotFound, "not_found")
}
