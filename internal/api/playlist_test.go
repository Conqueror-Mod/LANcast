package api

import (
	"context"
	"testing"
)

// The playlist listing is a contract change, so it gets a test at the contract
// rather than only at the store.
//
// The property that matters is the one no other listing has: a playlist may
// return the same item id twice, in order. Everything downstream — the client's
// list keys, the play queue — is wrong in a quiet, shortening way if this
// collapses duplicates.
func TestPlaylistEntriesComeBackInOrderWithRepeats(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	a := h.addFile(t, "a.mkv", []byte("a"))
	b := h.addFile(t, "b.mkv", []byte("b"))

	pid, err := h.st.EnsurePlaylist(ctx, h.lib.ID, h.dir+"/set.m3u", "The Set", "the set")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetPlaylistEntries(ctx, pid, []int64{b, a, b}); err != nil {
		t.Fatal(err)
	}

	resp := h.do(t, "GET", "/api/items?playlist_id="+itoa(pid), nil)
	defer resp.Body.Close()
	var body struct {
		Items []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		} `json:"items"`
	}
	decode(t, resp, &body)

	if len(body.Items) != 3 {
		t.Fatalf("got %d entries, want 3 (a repeat must not be collapsed): %+v",
			len(body.Items), body.Items)
	}
	if body.Items[0].ID != b || body.Items[1].ID != a || body.Items[2].ID != b {
		t.Errorf("ids = %d %d %d, want %d %d %d in playing order",
			body.Items[0].ID, body.Items[1].ID, body.Items[2].ID, b, a, b)
	}
}

func TestPlaylistIDMustBeANumber(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "GET", "/api/items?playlist_id=abc", nil), 400, "bad_request")
}

// An empty playlist is a real thing — the importer creates one when no line
// resolves — and must answer with an empty list rather than an error.
func TestEmptyPlaylistIsNotAnError(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	pid, err := h.st.EnsurePlaylist(ctx, h.lib.ID, h.dir+"/empty.m3u", "Empty", "empty")
	if err != nil {
		t.Fatal(err)
	}
	resp := h.do(t, "GET", "/api/items?playlist_id="+itoa(pid), nil)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
