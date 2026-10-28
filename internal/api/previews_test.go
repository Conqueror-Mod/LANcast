package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"

	"lancast/internal/preview"
	"lancast/internal/store"
)

// placePreviews writes a manifest and n sheets for an item, keyed to the file
// the item has now, as the worker would.
func placePreviews(t *testing.T, h *harness, id int64, sheets int) {
	t.Helper()
	it, err := h.st.GetItem(context.Background(), id, "")
	if err != nil {
		t.Fatal(err)
	}
	if h.srvAPI.previewsDir == "" {
		h.srvAPI.previewsDir = t.TempDir()
	}
	dir := preview.Dir(h.srvAPI.previewsDir, id)
	os.MkdirAll(dir, 0o755)
	for n := 0; n < sheets; n++ {
		os.WriteFile(preview.SheetPath(h.srvAPI.previewsDir, id, n), []byte("\xff\xd8 sheet "+itoa(int64(n))), 0o644)
	}
	raw, _ := json.Marshal(preview.Manifest{
		Key: store.PreviewKey(*it, preview.Rev), IntervalMS: 10000, Columns: 10, Rows: 10,
		Width: 240, Height: 136, Sheets: sheets,
	})
	os.WriteFile(dir+"/manifest.json", raw, 0o644)
}

func TestPreviewsAreServedForTheFileTheyWereMadeFrom(t *testing.T) {
	h := newHarness(t)
	h.srvAPI.previewsDir = t.TempDir()
	id := h.addFile(t, "film.mkv", []byte("a film"))

	get := func(path string) (int, string, string) {
		resp := h.do(t, "GET", "/api/items/"+itoa(id)+path, nil)
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header.Get("Content-Type"), string(raw)
	}

	// None yet: the ordinary answer.
	if code, _, _ := get("/previews"); code != http.StatusNotFound {
		t.Fatalf("before any: %d", code)
	}

	placePreviews(t, h, id, 2)
	code, _, body := get("/previews")
	var m struct {
		IntervalMS int `json:"interval_ms"`
		Sheets     int `json:"sheets"`
		Width      int `json:"width"`
	}
	json.Unmarshal([]byte(body), &m)
	if code != http.StatusOK || m.Sheets != 2 || m.IntervalMS != 10000 || m.Width != 240 {
		t.Fatalf("shape: %d %s", code, body)
	}
	if code, ctype, body := get("/previews/1"); code != http.StatusOK || ctype != "image/jpeg" || body != "\xff\xd8 sheet 1" {
		t.Fatalf("sheet 1: %d %s %q", code, ctype, body)
	}
	for _, bad := range []string{"2", "-1", "x", "..%2Fmanifest.json"} {
		if code, _, _ := get("/previews/" + bad); code != http.StatusNotFound {
			t.Errorf("sheet %q: %d", bad, code)
		}
	}

	// The file replaced since: its old previews are not this file's.
	it, err := h.st.GetItem(context.Background(), id, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.UpsertItem(context.Background(), store.ScanFile{
		LibraryID: h.lib.ID, Path: it.Path, Kind: "movie", Title: "film", SortTitle: "film",
		Container: "mkv", SizeBytes: 999_999, MTime: 12345,
	}); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := get("/previews"); code != http.StatusNotFound {
		t.Fatalf("after the file changed: %d", code)
	}
	if code, _, _ := get("/previews/0"); code != http.StatusNotFound {
		t.Fatalf("a sheet of the old file: %d", code)
	}
}

// A title kept from an account is kept from its previews too: the lookup is
// the same one every item route makes.
func TestPreviewsFollowTheItemsVisibility(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	h.srvAPI.previewsDir = t.TempDir()
	id := h.addFile(t, "film.mkv", []byte("a film"))
	placePreviews(t, h, id, 1)
	m := h.member(t, "kid", "a long enough password")
	resp := h.authed(t, "PATCH", "/api/users/"+m.id, map[string]any{"max_content_rating": "PG"})
	resp.Body.Close()
	for _, p := range []string{"/previews", "/previews/0"} {
		resp := h.asUser(t, m, "GET", "/api/items/"+itoa(id)+p, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s as a limited account: %d", p, resp.StatusCode)
		}
		resp = h.authed(t, "GET", "/api/items/"+itoa(id)+p, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s as the owner: %d", p, resp.StatusCode)
		}
	}
}

// A film opened with no previews yet is put at the front of the queue, and
// the answer says so, while the setting is on; not otherwise.
func TestAnOpenedFilmWithoutPreviewsIsQueued(t *testing.T) {
	h := newHarness(t)
	h.srvAPI.previewsDir = t.TempDir()
	var wanted []int64
	h.srvAPI.previewsWant = func(id int64) { wanted = append(wanted, id) }
	id := h.addFile(t, "film.mkv", []byte("a film"))

	code := func() (int, string) {
		resp := h.do(t, "GET", "/api/items/"+itoa(id)+"/previews", nil)
		defer resp.Body.Close()
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&body)
		return resp.StatusCode, body.Error.Code
	}

	// Off: the plain answer, nothing queued.
	if c, why := code(); c != http.StatusNotFound || why != "not_found" || len(wanted) != 0 {
		t.Fatalf("off: %d %q, wanted %v", c, why, wanted)
	}
	resp := h.do(t, "PUT", "/api/settings", map[string]any{"scrub_previews": true})
	resp.Body.Close()
	if c, why := code(); c != http.StatusNotFound || why != "queued" || len(wanted) != 1 || wanted[0] != id {
		t.Fatalf("on: %d %q, wanted %v", c, why, wanted)
	}
	// Made: served, not queued again.
	placePreviews(t, h, id, 1)
	if c, _ := code(); c != http.StatusOK || len(wanted) != 1 {
		t.Fatalf("once made: %d, wanted %v", c, wanted)
	}
}
