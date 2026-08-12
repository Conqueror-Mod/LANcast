package api

import (
	"context"
	"net/http"
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

// ---------------------------------------------------------------- editing

// entryIDs reads a playlist back through the API, which is what a client sees
// and therefore what the edits have to be judged against.
func entryIDs(t *testing.T, h *harness, pid int64) []int64 {
	t.Helper()
	resp := h.do(t, "GET", "/api/items?playlist_id="+itoa(pid), nil)
	var body struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	decode(t, resp, &body)
	out := make([]int64, 0, len(body.Items))
	for _, it := range body.Items {
		out = append(out, it.ID)
	}
	return out
}

func sameIDs(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestCreatePlaylistThenAddAndReorder(t *testing.T) {
	h := newHarness(t)
	a := h.addFile(t, "a.mkv", []byte("a"))
	b := h.addFile(t, "b.mkv", []byte("b"))

	resp := h.do(t, "POST", "/api/playlists",
		map[string]any{"title": "Road Trip", "library_id": h.lib.ID})
	if resp.StatusCode != 200 {
		t.Fatalf("create status = %d, want 200", resp.StatusCode)
	}
	// The created item comes back flat, the shape GET /api/items/{id} uses.
	var created struct {
		ID    int64  `json:"id"`
		Kind  string `json:"kind"`
		Title string `json:"title"`
	}
	decode(t, resp, &created)
	pid := created.ID
	if created.Kind != "playlist" || created.Title != "Road Trip" {
		t.Fatalf("created = %+v, want a playlist called Road Trip", created)
	}
	if got := entryIDs(t, h, pid); len(got) != 0 {
		t.Errorf("a new playlist has %d entries, want 0", len(got))
	}

	// Append, twice, including a repeat: adding the same track again is a
	// legitimate edit, not a no-op.
	if resp := h.do(t, "POST", "/api/playlists/"+itoa(pid)+"/entries",
		map[string]any{"item_ids": []int64{a, b}}); resp.StatusCode != 204 {
		t.Fatalf("add status = %d, want 204", resp.StatusCode)
	}
	if resp := h.do(t, "POST", "/api/playlists/"+itoa(pid)+"/entries",
		map[string]any{"item_ids": []int64{a}}); resp.StatusCode != 204 {
		t.Fatalf("second add status = %d, want 204", resp.StatusCode)
	}
	if got := entryIDs(t, h, pid); !sameIDs(got, []int64{a, b, a}) {
		t.Fatalf("after appends = %v, want %v — appends go on the end, repeats kept", got, []int64{a, b, a})
	}

	// Reorder is a whole-sequence write.
	if resp := h.do(t, "PUT", "/api/playlists/"+itoa(pid)+"/entries",
		map[string]any{"item_ids": []int64{b, a, a}}); resp.StatusCode != 204 {
		t.Fatalf("set status = %d, want 204", resp.StatusCode)
	}
	if got := entryIDs(t, h, pid); !sameIDs(got, []int64{b, a, a}) {
		t.Fatalf("after reorder = %v, want %v", got, []int64{b, a, a})
	}
}

// The property that makes removal position-addressed: removing the first of two
// identical tracks must leave the second one, and must not be answerable by id.
func TestRemoveByPositionKeepsTheOtherCopy(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	a := h.addFile(t, "a.mkv", []byte("a"))
	b := h.addFile(t, "b.mkv", []byte("b"))

	pid, err := h.st.EnsurePlaylist(ctx, h.lib.ID, h.dir+"/set.m3u", "The Set", "the set")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetPlaylistEntries(ctx, pid, []int64{a, b, a}); err != nil {
		t.Fatal(err)
	}

	if resp := h.do(t, "DELETE", "/api/playlists/"+itoa(pid)+"/entries/0", nil); resp.StatusCode != 204 {
		t.Fatalf("remove status = %d, want 204", resp.StatusCode)
	}
	if got := entryIDs(t, h, pid); !sameIDs(got, []int64{b, a}) {
		t.Fatalf("after removing position 0 = %v, want %v", got, []int64{b, a})
	}

	// Positions are dense afterwards, so the client's next index is still an
	// index. Removing the new last entry proves the resequence happened.
	if resp := h.do(t, "DELETE", "/api/playlists/"+itoa(pid)+"/entries/1", nil); resp.StatusCode != 204 {
		t.Fatalf("second remove status = %d, want 204", resp.StatusCode)
	}
	if got := entryIDs(t, h, pid); !sameIDs(got, []int64{b}) {
		t.Fatalf("after second remove = %v, want %v", got, []int64{b})
	}
	// And a stale client clicking again is told, not obeyed.
	wantError(t, h.do(t, "DELETE", "/api/playlists/"+itoa(pid)+"/entries/1", nil), 404, "not_found")
}

// The rule this whole feature is subordinate to (ADR 0030, CLAUDE.md): an edit
// marks membership user-owned, so a rescan cannot undo it. The scanner's half
// is tested in internal/scan; this is the half that has to set the lock.
func TestEveryMembershipWriteLocksMembers(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		call func(t *testing.T, h *harness, pid, item int64) *http.Response
	}{
		{"append", func(t *testing.T, h *harness, pid, item int64) *http.Response {
			return h.do(t, "POST", "/api/playlists/"+itoa(pid)+"/entries",
				map[string]any{"item_ids": []int64{item}})
		}},
		{"replace", func(t *testing.T, h *harness, pid, item int64) *http.Response {
			return h.do(t, "PUT", "/api/playlists/"+itoa(pid)+"/entries",
				map[string]any{"item_ids": []int64{item}})
		}},
		{"remove", func(t *testing.T, h *harness, pid, item int64) *http.Response {
			return h.do(t, "DELETE", "/api/playlists/"+itoa(pid)+"/entries/0", nil)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			item := h.addFile(t, "a.mkv", []byte("a"))
			pid, err := h.st.EnsurePlaylist(ctx, h.lib.ID, h.dir+"/set.m3u", "Set", "set")
			if err != nil {
				t.Fatal(err)
			}
			if err := h.st.SetPlaylistEntries(ctx, pid, []int64{item}); err != nil {
				t.Fatal(err)
			}
			locked, _ := h.st.LockedFields(ctx, pid)
			if len(locked) != 0 {
				t.Fatalf("an imported playlist starts with %v locked, want nothing", locked)
			}

			resp := tc.call(t, h, pid, item)
			resp.Body.Close()
			if resp.StatusCode != 204 {
				t.Fatalf("status = %d, want 204", resp.StatusCode)
			}

			locked, err = h.st.LockedFields(ctx, pid)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, f := range locked {
				if f == "members" {
					found = true
				}
			}
			if !found {
				t.Errorf("locked fields = %v, want members among them — "+
					"without it the next scan re-imports the .m3u over this edit", locked)
			}
		})
	}
}

// Deleting a playlist removes the list, never the music.
func TestDeletePlaylistLeavesItsTracksAlone(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	a := h.addFile(t, "a.mkv", []byte("a"))
	pid, err := h.st.EnsurePlaylist(ctx, h.lib.ID, h.dir+"/set.m3u", "Set", "set")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetPlaylistEntries(ctx, pid, []int64{a}); err != nil {
		t.Fatal(err)
	}

	if resp := h.do(t, "DELETE", "/api/playlists/"+itoa(pid), nil); resp.StatusCode != 204 {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	resp := h.do(t, "GET", "/api/items/"+itoa(pid), nil)
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("playlist still there: status = %d, want 404", resp.StatusCode)
	}
	resp = h.do(t, "GET", "/api/items/"+itoa(a), nil)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("the track went with it: status = %d, want 200 — "+
			"being in a playlist is not where a track lives", resp.StatusCode)
	}
}

func TestPlaylistEditRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	a := h.addFile(t, "a.mkv", []byte("a"))
	pid, err := h.st.EnsurePlaylist(ctx, h.lib.ID, h.dir+"/set.m3u", "Set", "set")
	if err != nil {
		t.Fatal(err)
	}

	// An id that is not a playlist: writing entries against an album would
	// succeed silently and be read by nothing.
	wantError(t, h.do(t, "POST", "/api/playlists/"+itoa(a)+"/entries",
		map[string]any{"item_ids": []int64{a}}), 400, "bad_request")
	// An item id that no longer exists — the stale-client case.
	wantError(t, h.do(t, "PUT", "/api/playlists/"+itoa(pid)+"/entries",
		map[string]any{"item_ids": []int64{a, 999999}}), 400, "bad_request")
	wantError(t, h.do(t, "POST", "/api/playlists",
		map[string]any{"title": "  ", "library_id": h.lib.ID}), 400, "bad_request")
	wantError(t, h.do(t, "POST", "/api/playlists",
		map[string]any{"title": "Nowhere", "library_id": 999999}), 404, "not_found")
	wantError(t, h.do(t, "DELETE", "/api/playlists/"+itoa(pid)+"/entries/-1", nil), 400, "bad_request")
	wantError(t, h.do(t, "DELETE", "/api/playlists/999999", nil), 404, "not_found")
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
