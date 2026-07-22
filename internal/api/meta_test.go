package api

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"testing"

	"lancast/internal/meta"
	"lancast/internal/store"
)

// stubProvider lets match and candidate endpoints be tested without a network.
type stubProvider struct {
	id    string
	cands []meta.Candidate
}

func (s *stubProvider) ID() string      { return s.id }
func (s *stubProvider) Caps() meta.Caps { return meta.Caps{Movie: true, Show: true, Episode: true} }
func (s *stubProvider) Search(context.Context, meta.Query) ([]meta.Candidate, error) {
	return s.cands, nil
}
func (s *stubProvider) Fetch(context.Context, meta.Ref) (*meta.Record, error) {
	return &meta.Record{Source: s.id, ExternalID: "1"}, nil
}

func TestPatchItemLocksEditedFields(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "a.mkv", make([]byte, 16))

	resp := h.do(t, "PATCH", "/api/items/"+itoa(id), map[string]any{
		"title": "My Corrected Title",
		"year":  1999,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var it store.Item
	decode(t, resp, &it)
	if it.Title != "My Corrected Title" {
		t.Errorf("Title = %q", it.Title)
	}
	if it.Year == nil || *it.Year != 1999 {
		t.Errorf("Year = %v", it.Year)
	}

	// Editing a field must lock it, and the client must be told so it can show
	// an indicator — a lock the user cannot see is indistinguishable from a bug.
	locked := map[string]bool{}
	for _, f := range it.LockedFields {
		locked[f] = true
	}
	if !locked["title"] || !locked["year"] {
		t.Errorf("locked_fields = %v, want title and year", it.LockedFields)
	}
	if locked["overview"] {
		t.Error("an untouched field was locked")
	}
}

func TestPatchItemDerivesSortTitle(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "a.mkv", make([]byte, 16))

	h.do(t, "PATCH", "/api/items/"+itoa(id), map[string]any{"title": "The Matrix"}).Body.Close()

	items, _, err := h.st.ListItems(context.Background(), store.ItemFilter{LibraryID: h.lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.ID == id && it.SortTitle != "matrix" {
			t.Errorf("SortTitle = %q, want the article stripped", it.SortTitle)
		}
	}
}

func TestPatchItemValidation(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "a.mkv", make([]byte, 16))
	path := "/api/items/" + itoa(id)

	tests := []struct {
		name string
		body any
	}{
		{"unknown field", map[string]any{"nonsense": "x"}},
		{"path is not editable", map[string]any{"path": "/etc/passwd"}},
		{"empty title", map[string]any{"title": "  "}},
		{"wrong type", map[string]any{"year": "not a number"}},
		{"no fields", map[string]any{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantError(t, h.do(t, "PATCH", path, tt.body), 400, "bad_request")
		})
	}

	wantError(t, h.do(t, "PATCH", "/api/items/9999", map[string]any{"title": "x"}), 404, "not_found")
}

func TestUnlockField(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "a.mkv", make([]byte, 16))
	h.do(t, "PATCH", "/api/items/"+itoa(id), map[string]any{"title": "Locked"}).Body.Close()

	resp := h.do(t, "DELETE", "/api/items/"+itoa(id)+"/locks/title", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	resp.Body.Close()

	locks, _ := h.st.LockedFields(context.Background(), id)
	if len(locks) != 0 {
		t.Errorf("locks = %v, want none after unlock", locks)
	}

	wantError(t, h.do(t, "DELETE", "/api/items/"+itoa(id)+"/locks/bogus", nil), 400, "bad_request")
	wantError(t, h.do(t, "DELETE", "/api/items/9999/locks/title", nil), 404, "not_found")
}

func TestCandidatesRequiresProvider(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "a.mkv", make([]byte, 16))

	// With nothing registered, the search yields nothing rather than erroring.
	resp := h.do(t, "GET", "/api/items/"+itoa(id)+"/candidates", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var cands []meta.Candidate
	decode(t, resp, &cands)
	if len(cands) != 0 {
		t.Errorf("candidates = %+v, want none", cands)
	}
}

func TestCandidatesRanked(t *testing.T) {
	h := newHarness(t)
	h.reg.AddProvider(&stubProvider{id: "stub", cands: []meta.Candidate{
		{ExternalID: "78", Kind: meta.KindMovie, Title: "Blade Runner", Year: 1982, Popularity: 61},
		{ExternalID: "335984", Kind: meta.KindMovie, Title: "Blade Runner 2049", Year: 2017, Popularity: 42},
	}})

	id := h.addFile(t, "a.mkv", make([]byte, 16))
	h.do(t, "PATCH", "/api/items/"+itoa(id), map[string]any{
		"title": "Blade Runner 2049", "year": 2017,
	}).Body.Close()

	resp := h.do(t, "GET", "/api/items/"+itoa(id)+"/candidates", nil)
	var cands []meta.Candidate
	decode(t, resp, &cands)

	if len(cands) != 2 {
		t.Fatalf("candidates = %d, want 2", len(cands))
	}
	if cands[0].ExternalID != "335984" {
		t.Errorf("best = %q (%s), want Blade Runner 2049", cands[0].Title, cands[0].ExternalID)
	}
	if cands[0].Score <= cands[1].Score {
		t.Error("candidates are not ranked best first")
	}
}

func TestApplyMatchLocksIdentity(t *testing.T) {
	h := newHarness(t)
	h.reg.AddProvider(&stubProvider{id: "stub"})
	id := h.addFile(t, "a.mkv", make([]byte, 16))

	resp := h.do(t, "POST", "/api/items/"+itoa(id)+"/match",
		map[string]any{"provider": "stub", "external_id": "335984"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var it store.Item
	decode(t, resp, &it)

	if it.MatchState != meta.StateLocked {
		t.Errorf("MatchState = %q, want locked", it.MatchState)
	}
	if it.ExternalID == nil || *it.ExternalID != "335984" {
		t.Errorf("ExternalID = %v", it.ExternalID)
	}
	if h.enriched == 0 {
		t.Error("confirming a match did not trigger enrichment")
	}
}

func TestApplyMatchValidation(t *testing.T) {
	h := newHarness(t)
	h.reg.AddProvider(&stubProvider{id: "stub"})
	id := h.addFile(t, "a.mkv", make([]byte, 16))
	path := "/api/items/" + itoa(id) + "/match"

	wantError(t, h.do(t, "POST", path, map[string]any{"external_id": "1"}), 400, "bad_request")
	wantError(t, h.do(t, "POST", path, map[string]any{"provider": "stub"}), 400, "bad_request")
	wantError(t, h.do(t, "POST", path, map[string]any{"provider": "ghost", "external_id": "1"}), 400, "bad_request")
	wantError(t, h.do(t, "POST", "/api/items/9999/match",
		map[string]any{"provider": "stub", "external_id": "1"}), 404, "not_found")
}

func TestReviewQueueEndpoint(t *testing.T) {
	h := newHarness(t)
	good := h.addFile(t, "good.mkv", make([]byte, 16))
	iffy := h.addFile(t, "iffy.mkv", make([]byte, 16))

	ctx := context.Background()
	h.st.SetMatch(ctx, good, "stub", "1", meta.StateMatched, 0.95)
	h.st.SetMatch(ctx, iffy, "stub", "2", meta.StateReview, 0.62)
	// Only enriched items qualify — an unscanned library must not report every
	// title as needing review.
	h.st.UpdateItemMetadata(ctx, good, store.ItemMetadata{})
	h.st.UpdateItemMetadata(ctx, iffy, store.ItemMetadata{})

	var body struct {
		Total int          `json:"total"`
		Items []store.Item `json:"items"`
	}
	decode(t, h.do(t, "GET", "/api/review", nil), &body)

	if body.Total != 1 || body.Items[0].ID != iffy {
		t.Errorf("review queue = %+v, want only the uncertain item", body.Items)
	}
}

func TestRefreshEndpoints(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "a.mkv", make([]byte, 16))
	ctx := context.Background()

	title := "Enriched"
	h.st.UpdateItemMetadata(ctx, id, store.ItemMetadata{Title: &title})
	if pending, _ := h.st.PendingEnrichment(ctx, 10); len(pending) != 0 {
		t.Fatal("test setup: item should not be pending")
	}

	resp := h.do(t, "POST", "/api/items/"+itoa(id)+"/refresh", nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("item refresh status = %d, want 202", resp.StatusCode)
	}
	resp.Body.Close()

	if pending, _ := h.st.PendingEnrichment(ctx, 10); len(pending) != 1 {
		t.Error("refresh did not requeue the item")
	}

	resp = h.do(t, "POST", "/api/libraries/1/refresh", nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("library refresh status = %d, want 202", resp.StatusCode)
	}
	resp.Body.Close()

	wantError(t, h.do(t, "POST", "/api/items/9999/refresh", nil), 404, "not_found")
	wantError(t, h.do(t, "POST", "/api/libraries/9999/refresh", nil), 404, "not_found")
}

func TestArtworkServing(t *testing.T) {
	h := newHarness(t)

	img := image.NewRGBA(image.Rect(0, 0, 600, 900))
	for y := 0; y < 900; y++ {
		for x := 0; x < 600; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: 100, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	hash, _, _, _, err := h.art.Put(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	resp := h.do(t, "GET", "/api/artwork/"+hash+"?size=poster", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q", ct)
	}
	// Content addressing makes indefinite caching safe.
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", cc)
	}
	if etag := resp.Header.Get("ETag"); etag != `"`+hash+`"` {
		t.Errorf("ETag = %q", etag)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Error("empty image body")
	}
}

// The grid renders from the list endpoint. If artwork only arrives on the
// detail response, every poster is fetched and stored and never displayed.
func TestListItemsIncludesArtwork(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "a.mkv", make([]byte, 16))
	h.st.PutArtwork(context.Background(), id, "posterhash", "poster", "u", 342, 513, 1)

	var body struct {
		Items []store.Item `json:"items"`
	}
	decode(t, h.do(t, "GET", "/api/items?library_id=1", nil), &body)

	if len(body.Items) != 1 {
		t.Fatalf("items = %d", len(body.Items))
	}
	if body.Items[0].Artwork == nil || body.Items[0].Artwork.Poster != "posterhash" {
		t.Errorf("list artwork = %+v, want the poster hash", body.Items[0].Artwork)
	}
}

func TestArtworkConditionalRequest(t *testing.T) {
	h := newHarness(t)
	img := image.NewRGBA(image.Rect(0, 0, 100, 150))
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, nil)
	hash, _, _, _, _ := h.art.Put(buf.Bytes())

	req, _ := http.NewRequest("GET", h.srv.URL+"/api/artwork/"+hash, nil)
	req.Header.Set("If-None-Match", `"`+hash+`"`)
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("status = %d, want 304 for a matching ETag", resp.StatusCode)
	}
}

// The hash arrives from the URL path, so a traversal attempt must be a miss,
// never a file read.
func TestArtworkRejectsBadHash(t *testing.T) {
	h := newHarness(t)
	for _, bad := range []string{"short", "..%2f..%2fetc%2fpasswd", "ZZZZ"} {
		wantError(t, h.do(t, "GET", "/api/artwork/"+bad, nil), 404, "not_found")
	}
}

func TestSettingsKeyIsWriteOnly(t *testing.T) {
	h := newHarness(t)

	var before map[string]any
	decode(t, h.do(t, "GET", "/api/settings", nil), &before)
	if before["tmdb"].(map[string]any)["configured"] != false {
		t.Errorf("configured = %v, want false initially", before["tmdb"])
	}

	resp := h.do(t, "PUT", "/api/settings", map[string]any{"tmdb_key": "super-secret-value"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)

	// A secret readable back out of the API leaks through screenshots, logs,
	// and shared sessions.
	if bytes.Contains(raw, []byte("super-secret-value")) {
		t.Fatalf("the API echoed the key back: %s", raw)
	}
	if !bytes.Contains(raw, []byte(`"configured":true`)) {
		t.Errorf("response did not report the key as configured: %s", raw)
	}

	var after map[string]any
	decode(t, h.do(t, "GET", "/api/settings", nil), &after)
	if after["tmdb"].(map[string]any)["configured"] != true {
		t.Error("configured = false after setting a key")
	}
	if h.settings.Get().TMDBKey != "super-secret-value" {
		t.Error("the key was not persisted")
	}
}

// Toggling one setting must not require resending the API key.
func TestSettingsPartialUpdatePreservesKey(t *testing.T) {
	h := newHarness(t)
	h.do(t, "PUT", "/api/settings", map[string]any{"tmdb_key": "keep-me"}).Body.Close()
	h.do(t, "PUT", "/api/settings", map[string]any{"write_nfo": true}).Body.Close()

	got := h.settings.Get()
	if got.TMDBKey != "keep-me" {
		t.Errorf("TMDBKey = %q, want it preserved across a partial update", got.TMDBKey)
	}
	if !got.WriteNFO {
		t.Error("WriteNFO was not applied")
	}
}

func TestSettingsValidation(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "PUT", "/api/settings", map[string]any{"rate_per_sec": 0}), 400, "bad_request")
	wantError(t, h.do(t, "PUT", "/api/settings", map[string]any{"rate_per_sec": 999}), 400, "bad_request")
}

func TestEnrichStatusEndpoint(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "GET", "/api/enrich", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
