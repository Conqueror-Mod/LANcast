package api

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

// testBook makes a book of two parts, ten and twenty minutes, the way the
// scan would (ADR 0116), and returns the book and its parts.
func testBook(t *testing.T, h *harness) (int64, []int64) {
	t.Helper()
	ctx := context.Background()
	dir := filepath.Join(h.dir, "Warped")
	book, err := h.st.EnsureDerivedContainer(ctx, h.lib.ID, "book", dir, "Warped", "warped", nil)
	if err != nil {
		t.Fatal(err)
	}
	var parts []int64
	for i, minutes := range []int64{10, 20} {
		name := []string{"Warped 1of2.mp3", "Warped 2of2.mp3"}[i]
		id, err := h.st.UpsertItem(ctx, store.ScanFile{
			LibraryID: h.lib.ID, Path: filepath.Join(dir, name), Kind: "book_part",
			Title: name, SortTitle: name, Container: "mp3", SizeBytes: 10, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.st.SetBookPart(ctx, id, book, i+1, "Part "+itoa(int64(i+1)), "part"); err != nil {
			t.Fatal(err)
		}
		if err := h.st.SaveProbe(ctx, id, store.ProbeResult{DurationMS: minutes * 60_000, Container: "mp3", AudioCodec: "mp3"}); err != nil {
			t.Fatal(err)
		}
		parts = append(parts, id)
	}
	return book, parts
}

// A book is one timeline over its parts, chaptered by them, with the
// caller's place and speed (ADR 0116 step 2).
func TestBookIsOneTimelineOverItsParts(t *testing.T) {
	h := newHarness(t)
	book, parts := testBook(t, h)

	var got bookInfo
	decode(t, h.do(t, "GET", "/api/items/"+itoa(book)+"/book", nil), &got)
	if !got.Ready || got.DurationMS != 30*60_000 || len(got.Parts) != 2 {
		t.Fatalf("book = %+v", got)
	}
	if got.Parts[1].ID != parts[1] || got.Parts[1].StartMS != 10*60_000 {
		t.Fatalf("second part placed at %d, want 10 minutes in", got.Parts[1].StartMS)
	}
	if len(got.Chapters) != 2 || got.Chapters[1].Title != "Part 2" || got.Chapters[1].StartMS != 10*60_000 {
		t.Fatalf("chapters = %+v", got.Chapters)
	}
	if got.Speed != nil {
		t.Fatalf("a book never played has no speed of its own: %v", *got.Speed)
	}

	// A part is not a book.
	if resp := h.do(t, "GET", "/api/items/"+itoa(parts[0])+"/book", nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a part's /book = %d, want 400", resp.StatusCode)
	}
}

// The place is written against the book, on the book's own clock, and the
// book is finished within its last minute — not at a film's percentage.
func TestBookProgressIsTheBooks(t *testing.T) {
	h := newHarness(t)
	book, parts := testBook(t, h)

	// Fifteen minutes in is half way through the second part.
	if resp := h.do(t, "PUT", "/api/items/"+itoa(book)+"/progress",
		map[string]any{"position_ms": 15 * 60_000}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("book progress = %d", resp.StatusCode)
	}
	var got bookInfo
	decode(t, h.do(t, "GET", "/api/items/"+itoa(book)+"/book", nil), &got)
	if got.PositionMS != 15*60_000 || got.Watched {
		t.Fatalf("place = %d watched=%v", got.PositionMS, got.Watched)
	}
	// The positive control for the speed test's absence check: a book with a
	// place is on the Continue shelf, as the book and not as a part.
	cont, err := h.st.ContinueWatching(context.Background(), store.LocalUserID, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cont) != 1 || cont[0].ID != book {
		t.Fatalf("continue shelf = %+v, want the book alone", cont)
	}
	// 90% of a book is not finishing it.
	h.do(t, "PUT", "/api/items/"+itoa(book)+"/progress", map[string]any{"position_ms": 27 * 60_000})
	decode(t, h.do(t, "GET", "/api/items/"+itoa(book)+"/book", nil), &got)
	if got.Watched {
		t.Fatal("three minutes from the end is not finished")
	}
	h.do(t, "PUT", "/api/items/"+itoa(book)+"/progress", map[string]any{"position_ms": 30*60_000 - 30_000})
	decode(t, h.do(t, "GET", "/api/items/"+itoa(book)+"/book", nil), &got)
	if !got.Watched {
		t.Fatal("thirty seconds from the end is finished")
	}

	// Nothing was written against a part.
	it, err := h.st.GetItem(context.Background(), parts[1], store.LocalUserID)
	if err != nil {
		t.Fatal(err)
	}
	if it.Progress != nil && it.Progress.PositionMS != 0 {
		t.Fatalf("a part carries a place of its own: %+v", it.Progress)
	}
}

func TestBookSpeedIsKeptPerBook(t *testing.T) {
	h := newHarness(t)
	book, parts := testBook(t, h)

	if resp := h.do(t, "PUT", "/api/items/"+itoa(book)+"/speed", map[string]any{"speed": 1.5}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("set speed = %d", resp.StatusCode)
	}
	var got bookInfo
	decode(t, h.do(t, "GET", "/api/items/"+itoa(book)+"/book", nil), &got)
	if got.Speed == nil || *got.Speed != 1.5 || got.PositionMS != 0 {
		t.Fatalf("speed = %v, position = %d", got.Speed, got.PositionMS)
	}
	// Choosing a speed is not a place: the book stays off the Continue shelf.
	cont, err := h.st.ContinueWatching(context.Background(), store.LocalUserID, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range cont {
		if it.ID == book {
			t.Fatal("a book with only a speed is on the Continue shelf")
		}
	}
	for _, bad := range []float64{0.5, 2.5} {
		if resp := h.do(t, "PUT", "/api/items/"+itoa(book)+"/speed", map[string]any{"speed": bad}); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("speed %v = %d, want 400", bad, resp.StatusCode)
		}
	}
	if resp := h.do(t, "PUT", "/api/items/"+itoa(parts[0])+"/speed", map[string]any{"speed": 1.25}); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a part's speed = %d, want 400", resp.StatusCode)
	}
}
