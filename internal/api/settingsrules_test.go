package api

import (
	"context"
	"testing"
	"time"

	"lancast/internal/store"
)

/*
 * The five server-side rules added in v0.6.13, tested at the contract.
 *
 * Every one of them is a setting whose failure is silent: a shelf that quietly
 * keeps the wrong things, a film that quietly counts as watched, a delete that
 * quietly still works after being switched off. None of those announce
 * themselves, which is the argument for testing them here rather than trusting
 * the handler to keep doing what it does today.
 */

// put updates settings, failing the test if the server refuses.
func (h *harness) putSettings(t *testing.T, body any) {
	t.Helper()
	resp := h.do(t, "PUT", "/api/settings", body)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("PUT /api/settings = %d, want 200", resp.StatusCode)
	}
}

func continueIDs(t *testing.T, h *harness) []int64 {
	t.Helper()
	resp := h.do(t, "GET", "/api/continue", nil)
	var body struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	decode(t, resp, &body)
	out := []int64{}
	for _, it := range body.Items {
		out = append(out, it.ID)
	}
	return out
}

// The watched threshold, applied server-side so every client agrees.
func TestWatchedThresholdMarksAnItemFinished(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	id := h.addFile(t, "film.mkv", []byte("f"))
	// Ten minutes long, written the way a probe writes it. Without a duration
	// the rule cannot apply at all — see the unknown-duration case in
	// config.TestWatchedThreshold.
	if err := h.st.SaveProbe(ctx, id, store.ProbeResult{
		DurationMS: 600_000, VideoCodec: "h264", AudioCodec: "aac",
	}); err != nil {
		t.Fatal(err)
	}

	// Stopping at 96% is finishing it: credits are not the film.
	resp := h.do(t, "PUT", "/api/items/"+itoa(id)+"/progress",
		map[string]any{"position_ms": 576_000, "watched": false})
	resp.Body.Close()

	if got := continueIDs(t, h); len(got) != 0 {
		t.Errorf("continue watching = %v, want empty — 96%% of a film is watched", got)
	}

	// And a threshold the operator raised means the same position is not.
	h.putSettings(t, map[string]any{"watched_threshold": 99})
	resp = h.do(t, "PUT", "/api/items/"+itoa(id)+"/progress",
		map[string]any{"position_ms": 576_000, "watched": false})
	resp.Body.Close()
	if got := continueIDs(t, h); len(got) != 1 {
		t.Errorf("continue watching = %v, want the film back at a 99%% threshold", got)
	}
}

// The Continue Watching window and cap.
//
// The window is asserted against the store, because proving it needs an entry
// older than the cutoff and backdating one would mean a setter that exists only
// for this test. Passing a cutoff directly is the same code path the handler
// uses, with the clock as an argument instead of a fact — which is why
// ContinueWatching takes it rather than reading the settings itself.
func TestContinueWatchingWindowAndLimit(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	recent := h.addFile(t, "recent.mkv", []byte("r"))
	stale := h.addFile(t, "stale.mkv", []byte("s"))

	for _, id := range []int64{recent, stale} {
		resp := h.do(t, "PUT", "/api/items/"+itoa(id)+"/progress",
			map[string]any{"position_ms": 1000, "watched": false})
		resp.Body.Close()
	}
	_ = stale

	// A cutoff in the future excludes everything; zero excludes nothing. The
	// difference between "never expire" and "expire now" is the whole setting.
	future := time.Now().Add(time.Hour).Unix()
	got, err := h.st.ContinueWatching(ctx, "local", 40, future)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("with a cutoff ahead of every entry, continue = %d items, want 0", len(got))
	}
	got, err = h.st.ContinueWatching(ctx, "local", 40, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("with no cutoff, continue = %d items, want 2", len(got))
	}

	// The cap is the server's, and a client cannot raise it.
	h.putSettings(t, map[string]any{"continue_limit": 1})
	if got := continueIDs(t, h); len(got) != 1 {
		t.Errorf("continue with limit 1 = %v, want one", got)
	}
	resp := h.do(t, "GET", "/api/continue?limit=50", nil)
	var body struct {
		Items []struct{} `json:"items"`
	}
	decode(t, resp, &body)
	if len(body.Items) != 1 {
		t.Errorf("a client asking for 50 got %d, want the server's 1", len(body.Items))
	}
}

// The deletion switch. mode=ignore is untouched by it: that writes no file.
func TestAllowMediaDeletionGatesDiskDeletes(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "keep.mkv", []byte("k"))

	h.putSettings(t, map[string]any{"allow_media_deletion": false})
	wantError(t, h.do(t, "DELETE", "/api/items/"+itoa(id)+"?mode=delete", nil), 403, "forbidden")

	resp := h.do(t, "GET", "/api/items/"+itoa(id), nil)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("the item was removed anyway: status = %d", resp.StatusCode)
	}

	// Removing it from the library still works — no file is touched.
	resp = h.do(t, "DELETE", "/api/items/"+itoa(id)+"?mode=ignore", nil)
	resp.Body.Close()
	if resp.StatusCode != 204 && resp.StatusCode != 200 {
		t.Errorf("mode=ignore = %d, want success; it deletes nothing from disk", resp.StatusCode)
	}
}

func TestSettingsRejectOutOfRangeRules(t *testing.T) {
	h := newHarness(t)
	for _, body := range []map[string]any{
		{"watched_threshold": 200},
		{"watched_threshold": 10},
		{"continue_weeks": -1},
		{"continue_limit": 0},
		{"continue_limit": 500},
		{"scan_interval_hours": -4},
		{"scan_interval_hours": 10000},
	} {
		wantError(t, h.do(t, "PUT", "/api/settings", body), 400, "bad_request")
	}
}

// The settings a client needs in order to render the controls at all.
func TestSettingsReportTheNewRules(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "GET", "/api/settings", nil)
	var body map[string]any
	decode(t, resp, &body)
	for _, key := range []string{
		"watched_threshold", "continue_weeks", "continue_limit",
		"allow_media_deletion", "scan_interval_hours",
	} {
		if _, ok := body[key]; !ok {
			t.Errorf("GET /api/settings omits %q, so no client can show it", key)
		}
	}
}
