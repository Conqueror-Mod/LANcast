package api

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func registerFilm(t *testing.T, h *harness, path, kind string) int64 {
	t.Helper()
	id, err := h.st.UpsertItem(context.Background(), store.ScanFile{
		LibraryID: h.lib.ID, Path: path, Kind: kind, Title: filepath.Base(path),
		SortTitle: filepath.Base(path), Container: "mkv", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type extrasResp struct {
	Extras []extraView `json:"extras"`
}

func extrasOf(t *testing.T, h *harness, id int64) []extraView {
	t.Helper()
	var got extrasResp
	decode(t, h.do(t, "GET", "/api/items/"+itoa(id)+"/extras", nil), &got)
	return got.Extras
}

func TestAFilmInItsOwnFolderListsItsExtras(t *testing.T) {
	h := newHarness(t)
	film := filepath.Join(h.dir, "Alien (1979)", "Alien (1979).mkv")
	writeFile(t, film, "film")
	writeFile(t, filepath.Join(h.dir, "Alien (1979)", "Trailers", "Teaser.mp4"), "teaser bytes")
	writeFile(t, filepath.Join(h.dir, "Alien (1979)", "Behind The Scenes", "Making_of.mkv"), "bts")
	writeFile(t, filepath.Join(h.dir, "Alien (1979)", "Alien (1979)-featurette.mkv"), "feat")
	writeFile(t, filepath.Join(h.dir, "Alien (1979)", "sample.mkv"), "junk")
	writeFile(t, filepath.Join(h.dir, "Alien (1979)", "Trailers", "notes.txt"), "not a video")
	id := registerFilm(t, h, film, "movie")

	got := extrasOf(t, h, id)
	var summary []string
	for _, x := range got {
		summary = append(summary, x.Kind+":"+x.Title)
	}
	want := []string{"trailer:Teaser", "featurette:Alien (1979)", "behind_the_scenes:Making of"}
	if len(summary) != len(want) {
		t.Fatalf("extras = %v, want %v", summary, want)
	}
	for i := range want {
		if summary[i] != want[i] {
			t.Fatalf("extras = %v, want %v", summary, want)
		}
	}

	// Played straight from the file, ranges and all.
	req, _ := http.NewRequest("GET", h.srv.URL+"/api/items/"+itoa(id)+"/extras/"+got[0].Key, nil)
	req.Header.Set("Range", "bytes=0-5")
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(body) != "teaser" || resp.Header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("stream = %d %q %q", resp.StatusCode, body, resp.Header.Get("Content-Type"))
	}
}

// Several films in the library root: only the marked file naming this film is
// its extra, and a "Trailers" folder at the root is a category, not extras.
func TestAFilmSharingAFolderClaimsOnlyItsOwnMarkedExtras(t *testing.T) {
	h := newHarness(t)
	alien := filepath.Join(h.dir, "Alien.mkv")
	writeFile(t, alien, "film")
	writeFile(t, filepath.Join(h.dir, "Aliens.mkv"), "film")
	writeFile(t, filepath.Join(h.dir, "Alien-trailer.mp4"), "mine")
	writeFile(t, filepath.Join(h.dir, "Aliens-trailer.mp4"), "theirs")
	writeFile(t, filepath.Join(h.dir, "Trailers", "Something Else.mp4"), "a category")
	id := registerFilm(t, h, alien, "movie")

	got := extrasOf(t, h, id)
	if len(got) != 1 || got[0].Title != "Alien" || got[0].Kind != "trailer" {
		t.Fatalf("extras = %+v", got)
	}
}

// A key is looked up against a fresh listing, never decoded into a path, so a
// key naming any other file plays nothing.
func TestAForgedKeyPlaysNothing(t *testing.T) {
	h := newHarness(t)
	film := filepath.Join(h.dir, "Film (2000)", "Film (2000).mkv")
	writeFile(t, film, "film")
	writeFile(t, filepath.Join(h.dir, "Film (2000)", "Trailers", "T.mp4"), "trailer")
	writeFile(t, filepath.Join(h.dir, "secret.mkv"), "not an extra")
	id := registerFilm(t, h, film, "movie")

	for _, rel := range []string{"../secret.mkv", "Film (2000).mkv", "Trailers/../../secret.mkv"} {
		key := base64.RawURLEncoding.EncodeToString([]byte(rel))
		resp := h.do(t, "GET", "/api/items/"+itoa(id)+"/extras/"+key, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("key for %q answered %d", rel, resp.StatusCode)
		}
	}
}

func TestAShowListsTheExtrasInItsFolder(t *testing.T) {
	h := newHarness(t)
	show := filepath.Join(h.dir, "Firefly")
	writeFile(t, filepath.Join(show, "Season 1", "Firefly S01E01.mkv"), "ep")
	writeFile(t, filepath.Join(show, "Featurettes", "Here's how it was.mkv"), "feat")
	id := registerFilm(t, h, show, "show")

	got := extrasOf(t, h, id)
	if len(got) != 1 || got[0].Kind != "featurette" || got[0].Title != "Here's how it was" {
		t.Fatalf("extras = %+v", got)
	}
}

func TestOnlyFilmsAndShowsHaveExtras(t *testing.T) {
	h := newHarness(t)
	dir := filepath.Join(h.dir, "Show", "Season 1")
	episode := filepath.Join(dir, "Show S01E01.mkv")
	writeFile(t, episode, "ep")
	// An extras folder beside an episode is the show's, not the episode's.
	writeFile(t, filepath.Join(dir, "Trailers", "T.mp4"), "trailer")
	id := registerFilm(t, h, episode, "episode")
	got := extrasOf(t, h, id)
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
	resp := h.do(t, "GET", "/api/items/99999/extras", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown item = %d", resp.StatusCode)
	}
}
