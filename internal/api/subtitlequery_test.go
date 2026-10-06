package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

/*
 * What a subtitle search asks for (subtitleQuery).
 *
 * By id when the metadata match gave one, so the provider returns subtitles
 * for this film or this episode of this show, not for everything sharing a few
 * words of its title. By title only when there is no id, and by what was typed
 * whenever somebody typed something.
 */

func strp(s string) *string { return &s }
func intp(n int) *int       { return &n }

func TestSubtitleQueryAsksForAFilmByItsIMDbID(t *testing.T) {
	film := &store.Item{Kind: "movie", Title: "Avengers", Year: intp(2012),
		IMDbID: strp("tt0848228"), Provider: strp("tmdb"), ExternalID: strp("24428")}
	q, title := subtitleQuery(film, nil, "")
	if q.IMDBID != "tt0848228" || q.Query != "" || q.TMDBID != "" {
		t.Errorf("query = %+v, want the IMDb id alone, no title text", q)
	}
	if title != "Avengers" {
		t.Errorf("rank title = %q, want the film's", title)
	}
}

func TestSubtitleQueryFallsBackToTheTMDBID(t *testing.T) {
	film := &store.Item{Kind: "movie", Title: "Avengers", Provider: strp("tmdb"), ExternalID: strp("24428")}
	q, _ := subtitleQuery(film, nil, "")
	if q.TMDBID != "24428" || q.Query != "" {
		t.Errorf("query = %+v, want the TMDB id", q)
	}
}

// An id from another provider is not a TMDB id.
func TestSubtitleQueryIgnoresAnotherProvidersID(t *testing.T) {
	film := &store.Item{Kind: "movie", Title: "Home Video", Year: intp(1999), Provider: strp("nfo"), ExternalID: strp("abc")}
	q, _ := subtitleQuery(film, nil, "")
	if q.TMDBID != "" || q.Query != "Home Video" || q.Year != 1999 {
		t.Errorf("query = %+v, want the title and year", q)
	}
}

func TestSubtitleQueryAsksForAnEpisodeByItsShowsID(t *testing.T) {
	ep := &store.Item{Kind: "episode", Title: "Pilot", Series: strp("Futurama"), Season: intp(1), Episode: intp(1)}
	show := &store.Item{Kind: "show", Title: "Futurama", IMDbID: strp("tt0149460")}
	q, title := subtitleQuery(ep, show, "")
	if q.ParentIMDBID != "tt0149460" || q.Season != 1 || q.Episode != 1 || q.Query != "" || q.IMDBID != "" {
		t.Errorf("query = %+v, want the show's IMDb id with season and episode", q)
	}
	if title != "Futurama" {
		t.Errorf("rank title = %q, want the series", title)
	}

	show = &store.Item{Kind: "show", Title: "Futurama", Provider: strp("tmdb"), ExternalID: strp("615")}
	q, _ = subtitleQuery(ep, show, "")
	if q.ParentTMDBID != "615" || q.Season != 1 {
		t.Errorf("query = %+v, want the show's TMDB id", q)
	}
}

// An episode whose show is not known falls back to the series name.
func TestSubtitleQueryEpisodeWithoutAShowUsesTheSeriesName(t *testing.T) {
	ep := &store.Item{Kind: "episode", Title: "Pilot", Series: strp("Futurama"), Season: intp(1), Episode: intp(1)}
	q, _ := subtitleQuery(ep, nil, "")
	if q.Query != "Futurama" || q.Season != 1 || q.Episode != 1 {
		t.Errorf("query = %+v, want the series name with season and episode", q)
	}
}

// A manual search is how a wrong match is worked around, so it is never
// overridden by the id that may be the wrong one.
func TestSubtitleQueryRespectsWhatWasTyped(t *testing.T) {
	film := &store.Item{Kind: "movie", Title: "Avengers", IMDbID: strp("tt0848228")}
	q, title := subtitleQuery(film, nil, "The Avengers 1998")
	if q.Query != "The Avengers 1998" || q.IMDBID != "" {
		t.Errorf("query = %+v, want only the typed text", q)
	}
	if title != "The Avengers 1998" {
		t.Errorf("rank title = %q, want what was typed", title)
	}
}

// End to end: searching from an episode finds its show through the season and
// sends the show's IMDb id, with season and episode, and no title text.
func TestSubtitleSearchFromAnEpisodeSendsItsShowsID(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	var got url.Values
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer fake.Close()
	h.srvAPI.subtitleBaseURL = fake.URL
	h.putSettings(t, map[string]any{"opensubtitles_key": "test-key"})

	show, err := h.st.UpsertItem(ctx, store.ScanFile{LibraryID: h.lib.ID, Path: filepath.Join(h.dir, "Futurama"),
		Kind: "show", Title: "Futurama", SortTitle: "futurama", MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.UpdateItemMetadata(ctx, show, store.ItemMetadata{IMDbID: strp("tt0149460")}); err != nil {
		t.Fatal(err)
	}
	season, err := h.st.UpsertItem(ctx, store.ScanFile{LibraryID: h.lib.ID, Path: filepath.Join(h.dir, "Futurama", "Season 1"),
		Kind: "season", Title: "Season 1", SortTitle: "season 1", MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	epPath := filepath.Join(h.dir, "Futurama", "Season 1", "s01e02.mkv")
	if err := os.MkdirAll(filepath.Dir(epPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(epPath, make([]byte, 256*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	series := "Futurama"
	ep, err := h.st.UpsertItem(ctx, store.ScanFile{LibraryID: h.lib.ID, Path: epPath, Kind: "episode",
		Title: "Fry and the Slurm Factory", SortTitle: "fry", Series: &series, Season: intp(1), Episode: intp(2),
		Container: "matroska", SizeBytes: 256 * 1024, MTime: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetParent(ctx, season, &show); err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetParent(ctx, ep, &season); err != nil {
		t.Fatal(err)
	}

	resp := h.do(t, "GET", "/api/items/"+itoa(ep)+"/subtitles/search", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search status = %d", resp.StatusCode)
	}
	if got.Get("parent_imdb_id") != "0149460" || got.Get("season_number") != "1" || got.Get("episode_number") != "2" {
		t.Errorf("sent %v, want the show's IMDb id with season 1 episode 2", got)
	}
	if got.Has("query") {
		t.Errorf("sent a title query %q alongside the id", got.Get("query"))
	}
}
