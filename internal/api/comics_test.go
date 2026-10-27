package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/comic/comictest"
	"lancast/internal/store"
)

// addComic makes a comics library holding one real archive of n pages.
func addComic(t *testing.T, h *harness, n int) int64 {
	t.Helper()
	dir := t.TempDir()
	lib, err := h.st.CreateLibrary(context.Background(), "Comics", "comics", dir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Saga 001.cbz")
	comictest.Write(t, path, n)
	return upsertComic(t, h, lib.ID, path)
}

func upsertComic(t *testing.T, h *harness, libID int64, path string) int64 {
	t.Helper()
	if _, err := h.st.UpsertItem(context.Background(), store.ScanFile{
		LibraryID: libID, Path: path, Kind: "comic",
		Title: "Saga 001", SortTitle: "Saga 001", Container: "cbz", SizeBytes: 1, MTime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	known, err := h.st.KnownFiles(context.Background(), libID)
	if err != nil || known[path].ID == 0 {
		t.Fatalf("comic not upserted: %v", err)
	}
	return known[path].ID
}

type pagesAnswer struct {
	Pages int               `json:"pages"`
	Place *store.ComicPlace `json:"place"`
}

func readPages(t *testing.T, h *harness, id int64) pagesAnswer {
	t.Helper()
	resp := h.do(t, "GET", fmt.Sprintf("/api/items/%d/pages", id), nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pages: %d", resp.StatusCode)
	}
	var a pagesAnswer
	json.NewDecoder(resp.Body).Decode(&a)
	return a
}

func TestAComicsPagesComeOutOfTheArchiveInOrder(t *testing.T) {
	h := newHarness(t)
	id := addComic(t, h, 10)

	if a := readPages(t, h, id); a.Pages != 10 || a.Place != nil {
		t.Fatalf("pages = %+v", a)
	}
	for _, n := range []int{0, 1, 9} {
		resp := h.do(t, "GET", fmt.Sprintf("/api/items/%d/pages/%d", id, n), nil)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" {
			t.Fatalf("page %d: %d %s", n, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		img, _, err := image.Decode(bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		// Page n is the archive's p(n+1).png, whose red channel is n+1:
		// natural order, not "p1, p10, p2".
		if r, _, _, _ := img.At(0, 0).RGBA(); int(r>>8) != n+1 {
			t.Fatalf("page %d is p%d.png", n, r>>8)
		}
	}
	for _, bad := range []string{"10", "-1", "x", "..%2F..%2Fsecret"} {
		resp := h.do(t, "GET", fmt.Sprintf("/api/items/%d/pages/%s", id, bad), nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("page %q: %d", bad, resp.StatusCode)
		}
	}
}

func TestOnlyAComicHasPages(t *testing.T) {
	h := newHarness(t)
	film := h.addFile(t, "film.mkv", []byte("x"))
	resp := h.do(t, "GET", fmt.Sprintf("/api/items/%d/pages", film), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a film's pages: %d", resp.StatusCode)
	}
}

// The row is trusted, the file system is not: a comic whose path leads out
// of its library is not opened (CLAUDE.md, ADR 0094).
func TestAComicOutsideItsLibraryIsNotOpened(t *testing.T) {
	h := newHarness(t)
	lib, err := h.st.CreateLibrary(context.Background(), "Comics", "comics", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "elsewhere.cbz")
	comictest.Write(t, outside, 2)
	id := upsertComic(t, h, lib.ID, outside)
	resp := h.do(t, "GET", fmt.Sprintf("/api/items/%d/pages/0", id), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestABrokenArchiveSaysSo(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	lib, _ := h.st.CreateLibrary(context.Background(), "Comics", "comics", dir)
	path := filepath.Join(dir, "broken.cbz")
	os.WriteFile(path, []byte("not a zip"), 0o644)
	id := upsertComic(t, h, lib.ID, path)
	resp := h.do(t, "GET", fmt.Sprintf("/api/items/%d/pages", id), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestYourPlaceIsKeptAndTheLastPageFinishesIt(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	id := addComic(t, h, 3)
	put := func(body any) int {
		resp := h.authed(t, "PUT", fmt.Sprintf("/api/items/%d/place", id), body)
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := put(map[string]any{"page": 1}); code != http.StatusNoContent {
		t.Fatalf("page 1: %d", code)
	}
	resp := h.authed(t, "GET", fmt.Sprintf("/api/items/%d/pages", id), nil)
	var a pagesAnswer
	json.NewDecoder(resp.Body).Decode(&a)
	resp.Body.Close()
	if a.Place == nil || a.Place.Page != 1 || a.Place.Pages != 3 {
		t.Fatalf("place = %+v", a.Place)
	}
	for _, bad := range []any{map[string]any{"page": 3}, map[string]any{"page": -1}, map[string]any{}} {
		if code := put(bad); code != http.StatusBadRequest {
			t.Errorf("%v: %d", bad, code)
		}
	}

	it, err := h.st.GetItem(context.Background(), id, h.ownID(t))
	if err != nil {
		t.Fatal(err)
	}
	if it.Progress != nil && it.Progress.Watched {
		t.Fatal("read before the last page")
	}
	if code := put(map[string]any{"page": 2}); code != http.StatusNoContent {
		t.Fatalf("last page: %d", code)
	}
	it, _ = h.st.GetItem(context.Background(), id, h.ownID(t))
	if it.Progress == nil || !it.Progress.Watched {
		t.Fatal("the last page did not mark it read")
	}

	// Somebody else's place is their own.
	m := h.member(t, "sam", "a long enough password")
	resp = h.asUser(t, m, "GET", fmt.Sprintf("/api/items/%d/pages", id), nil)
	var theirs pagesAnswer
	json.NewDecoder(resp.Body).Decode(&theirs)
	resp.Body.Close()
	if theirs.Place != nil {
		t.Fatalf("sam sees dana's place: %+v", theirs.Place)
	}
}

func (h *harness) ownID(t *testing.T) string {
	t.Helper()
	u, err := h.st.UserByName(context.Background(), testUser)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// Nothing rates comics yet, so an account with a content limit cannot open
// one: an unrated title is kept from a limited account (ADR 0071), and a page
// route is no way round that.
func TestALimitedAccountCannotOpenAComic(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a long enough password")
	id := addComic(t, h, 2)
	m := h.member(t, "kid", "a long enough password")
	// Before the limit the same account opens it, so the 404 below is the
	// limit's doing and not the route's.
	resp := h.asUser(t, m, "GET", fmt.Sprintf("/api/items/%d/pages", id), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("before the limit: %d", resp.StatusCode)
	}
	resp = h.authed(t, "PATCH", "/api/users/"+m.id, map[string]any{"max_content_rating": "PG"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("set limit: %d", resp.StatusCode)
	}
	for _, p := range []string{"/pages", "/pages/0"} {
		resp := h.asUser(t, m, "GET", fmt.Sprintf("/api/items/%d%s", id, p), nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d", p, resp.StatusCode)
		}
	}

	// Rated by hand, within the limit, it is theirs to read again.
	resp = h.authed(t, "PATCH", fmt.Sprintf("/api/items/%d", id), map[string]any{"content_rating": "G"})
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("rate by hand: %d", resp.StatusCode)
	}
	resp = h.asUser(t, m, "GET", fmt.Sprintf("/api/items/%d/pages", id), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("after rating it G: %d", resp.StatusCode)
	}
}
