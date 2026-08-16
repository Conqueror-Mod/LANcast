package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/meta"
	"lancast/internal/store"
)

/*
 * End to end, on the shape that caused this: the year lives on the folder, the
 * row was searched without one and landed in review holding a wrong identity.
 *
 * Re-parse must correct the stored guess and requeue the row. Refresh could not
 * have done this — it asks the provider the same question again, and the
 * question was the broken part.
 */
func TestReparseRecoversAFolderYearAndRequeues(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if err := os.MkdirAll(filepath.Join(h.dir, "Dredd (2012)"), 0o755); err != nil {
		t.Fatal(err)
	}
	id := h.addFile(t, filepath.Join("Dredd (2012)", "Dredd.mkv"), make([]byte, 16))

	wrong := "Judge Minty"
	wrongYear := 2013
	h.st.UpdateItemMetadata(ctx, id, store.ItemMetadata{Title: &wrong, Year: &wrongYear})
	h.st.SetMatch(ctx, id, "stub", "9", meta.StateReview, 0.61)

	before := h.enriched
	var res struct {
		Examined int `json:"examined"`
		Changed  int `json:"changed"`
	}
	resp := h.do(t, "POST", "/api/libraries/"+itoa(h.lib.ID)+"/reparse", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("reparse = %d, want 200", resp.StatusCode)
	}
	decode(t, resp, &res)

	if res.Examined != 1 || res.Changed != 1 {
		t.Errorf("result = %+v, want 1 examined and 1 changed", res)
	}

	it, err := h.st.GetItem(ctx, id, "local")
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "Dredd" {
		t.Errorf("title = %q, want Dredd", it.Title)
	}
	if it.Year == nil || *it.Year != 2012 {
		t.Errorf("year = %v, want 2012 recovered from the folder", it.Year)
	}
	if h.enriched == before {
		t.Error("enrichment was not nudged — the corrected guess is never searched")
	}
}

// Safe to run twice: the second pass finds the row already agreeing with its
// filename, changes nothing, and does not requeue the library.
func TestReparseIsIdempotent(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if err := os.MkdirAll(filepath.Join(h.dir, "Dredd (2012)"), 0o755); err != nil {
		t.Fatal(err)
	}
	id := h.addFile(t, filepath.Join("Dredd (2012)", "Dredd.mkv"), make([]byte, 16))
	h.st.UpdateItemMetadata(ctx, id, store.ItemMetadata{})
	h.st.SetMatch(ctx, id, "stub", "9", meta.StateReview, 0.61)

	h.do(t, "POST", "/api/libraries/"+itoa(h.lib.ID)+"/reparse", nil)
	after := h.enriched

	var res struct {
		Changed int `json:"changed"`
	}
	decode(t, h.do(t, "POST", "/api/libraries/"+itoa(h.lib.ID)+"/reparse", nil), &res)
	if res.Changed != 0 {
		t.Errorf("changed = %d on a second run, want 0", res.Changed)
	}
	if h.enriched != after {
		t.Error("a no-op re-parse still nudged enrichment")
	}
}

// A matched row's title came from a provider, which is better evidence than any
// filename. Re-parse must not touch it.
func TestReparseLeavesMatchedRowsAlone(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if err := os.MkdirAll(filepath.Join(h.dir, "Dredd (2012)"), 0o755); err != nil {
		t.Fatal(err)
	}
	id := h.addFile(t, filepath.Join("Dredd (2012)", "Dredd.mkv"), make([]byte, 16))

	provider := "Dredd 3D"
	h.st.UpdateItemMetadata(ctx, id, store.ItemMetadata{Title: &provider})
	h.st.SetMatch(ctx, id, "stub", "9", meta.StateMatched, 0.93)

	var res struct {
		Examined int `json:"examined"`
	}
	decode(t, h.do(t, "POST", "/api/libraries/"+itoa(h.lib.ID)+"/reparse", nil), &res)
	if res.Examined != 0 {
		t.Errorf("examined = %d, want 0 — a matched row was offered to a re-parse", res.Examined)
	}

	it, _ := h.st.GetItem(ctx, id, "local")
	if it.Title != "Dredd 3D" {
		t.Errorf("title = %q — a provider title was overwritten by a filename guess", it.Title)
	}
}
