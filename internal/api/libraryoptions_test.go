package api

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"lancast/internal/comic/comictest"
	"lancast/internal/meta"
	"lancast/internal/store"
)

/*
 * A library's own options (ADR 0123): a film or show library's metadata
 * language, a comics library's reading direction, and a series that reads the
 * other way.
 */

// addSeriesComic puts a two-page comic in a series folder of a new comics
// library, the way ADR 0101 names a series.
func addSeriesComic(t *testing.T, h *harness, series string) (libID, itemID int64) {
	t.Helper()
	dir := t.TempDir()
	lib, err := h.st.CreateLibrary(context.Background(), "Manga", "comics", dir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, series, series+" 001.cbz")
	comictest.Write(t, path, 2)
	s := series
	if _, err := h.st.UpsertItem(context.Background(), store.ScanFile{
		LibraryID: lib.ID, Path: path, Kind: "comic", Series: &s,
		Title: series + " 001", SortTitle: series + " 001", Container: "cbz", SizeBytes: 1, MTime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	known, _ := h.st.KnownFiles(context.Background(), lib.ID)
	return lib.ID, known[path].ID
}

func readDirection(t *testing.T, h *harness, id int64) map[string]any {
	t.Helper()
	var body map[string]any
	decode(t, h.do(t, "GET", fmt.Sprintf("/api/items/%d/pages", id), nil), &body)
	return body
}

func TestALibraryCarriesItsOwnOptions(t *testing.T) {
	h := newHarness(t)
	films, err := h.st.CreateLibrary(context.Background(), "Anime", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	comics, _ := addSeriesComic(t, h, "Akira")

	var lib map[string]any
	decode(t, h.do(t, "PATCH", fmt.Sprintf("/api/libraries/%d", films.ID),
		map[string]any{"metadata_language": "ja-JP"}), &lib)
	if lib["metadata_language"] != "ja-JP" {
		t.Errorf("metadata_language = %#v, want ja-JP", lib["metadata_language"])
	}
	decode(t, h.do(t, "PATCH", fmt.Sprintf("/api/libraries/%d", comics),
		map[string]any{"reading_direction": "rtl"}), &lib)
	if lib["reading_direction"] != "rtl" {
		t.Errorf("reading_direction = %#v, want rtl", lib["reading_direction"])
	}

	// Cleared with "", and left to right is stored as the default.
	decode(t, h.do(t, "PATCH", fmt.Sprintf("/api/libraries/%d", films.ID),
		map[string]any{"metadata_language": ""}), &lib)
	if lib["metadata_language"] != nil {
		t.Errorf("after clearing, metadata_language = %#v, want null", lib["metadata_language"])
	}
	decode(t, h.do(t, "PATCH", fmt.Sprintf("/api/libraries/%d", comics),
		map[string]any{"reading_direction": "ltr"}), &lib)
	if lib["reading_direction"] != nil {
		t.Errorf("ltr was stored as %#v, want null", lib["reading_direction"])
	}
}

func TestAnOptionALibraryCannotUseIsRefused(t *testing.T) {
	h := newHarness(t)
	music, _ := h.st.CreateLibrary(context.Background(), "Music", "music", t.TempDir())
	films, _ := h.st.CreateLibrary(context.Background(), "Films", "movie", t.TempDir())
	comics, _ := addSeriesComic(t, h, "Akira")

	for _, c := range []struct {
		lib  int64
		body map[string]any
	}{
		{music.ID, map[string]any{"metadata_language": "de-DE"}},
		{comics, map[string]any{"metadata_language": "ja-JP"}},
		{films.ID, map[string]any{"metadata_language": "klingon"}},
		{films.ID, map[string]any{"reading_direction": "rtl"}},
		{comics, map[string]any{"reading_direction": "upwards"}},
		// A bad option stops the whole request, rename included.
		{films.ID, map[string]any{"name": "Renamed", "reading_direction": "rtl"}},
	} {
		wantError(t, h.do(t, "PATCH", fmt.Sprintf("/api/libraries/%d", c.lib), c.body), 400, "bad_request")
	}
	if l, _ := h.st.GetLibrary(context.Background(), films.ID); l.Name != "Films" {
		t.Errorf("name = %q: a refused request renamed the library", l.Name)
	}
}

func TestAComicReadsItsSeriesWayThenItsLibrarysThenLeftToRight(t *testing.T) {
	h := newHarness(t)
	lib, id := addSeriesComic(t, h, "Akira")

	if d := readDirection(t, h, id); d["direction"] != "ltr" || d["direction_from"] != "default" || d["series"] != "Akira" {
		t.Errorf("before anything is set: %v", d)
	}

	h.do(t, "PATCH", fmt.Sprintf("/api/libraries/%d", lib), map[string]any{"reading_direction": "rtl"})
	if d := readDirection(t, h, id); d["direction"] != "rtl" || d["direction_from"] != "library" {
		t.Errorf("library rtl: %v", d)
	}

	var d map[string]any
	decode(t, h.do(t, "PUT", fmt.Sprintf("/api/items/%d/series-direction", id),
		map[string]any{"direction": "ltr"}), &d)
	if d["direction"] != "ltr" || d["direction_from"] != "series" {
		t.Errorf("PUT answered %v, want ltr from the series", d)
	}
	if d := readDirection(t, h, id); d["direction"] != "ltr" || d["direction_from"] != "series" {
		t.Errorf("series ltr in an rtl library: %v", d)
	}

	// Removing the override hands the series back to its library.
	h.do(t, "PUT", fmt.Sprintf("/api/items/%d/series-direction", id), map[string]any{"direction": ""})
	if d := readDirection(t, h, id); d["direction"] != "rtl" || d["direction_from"] != "library" {
		t.Errorf("after removing the override: %v", d)
	}
}

func TestASeriesOverrideNeedsASeriesAndAnAdmin(t *testing.T) {
	h := newHarness(t)
	loose := addComic(t, h, 2) // at the library's root: no series
	wantError(t, h.do(t, "PUT", fmt.Sprintf("/api/items/%d/series-direction", loose),
		map[string]any{"direction": "rtl"}), 400, "bad_request")

	_, id := addSeriesComic(t, h, "Akira")
	wantError(t, h.do(t, "PUT", fmt.Sprintf("/api/items/%d/series-direction", id),
		map[string]any{"direction": "sideways"}), 400, "bad_request")

	m := h.member(t, "reader", "a long enough password")
	resp := h.asUser(t, m, "PUT", fmt.Sprintf("/api/items/%d/series-direction", id), map[string]any{"direction": "rtl"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a member set a series' direction: %d, want 403", resp.StatusCode)
	}
}

// Fix match searches in the library's own language (ADR 0123).
func TestFixMatchSearchesInTheLibrarysLanguage(t *testing.T) {
	h := newHarness(t)
	lib, err := h.st.CreateLibrary(context.Background(), "Anime", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetLibraryLanguage(context.Background(), lib.ID, "ja-JP"); err != nil {
		t.Fatal(err)
	}
	if got := meta.LanguageFrom(h.srvAPI.inLibraryLanguage(context.Background(), lib.ID)); got != "ja-JP" {
		t.Errorf("context language = %q, want ja-JP", got)
	}
	other, _ := h.st.CreateLibrary(context.Background(), "Films", "movie", t.TempDir())
	if got := meta.LanguageFrom(h.srvAPI.inLibraryLanguage(context.Background(), other.ID)); got != "" {
		t.Errorf("a library with no language carried %q; want none, so the server's applies", got)
	}
}
