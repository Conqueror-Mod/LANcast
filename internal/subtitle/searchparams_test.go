package subtitle

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// The ids reach OpenSubtitles as its own parameter names, "tt" stripped as it
// is for imdb_id, and nothing else is sent in their place.
func TestSearchSendsParentIDsForAnEpisode(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c := NewOpenSubtitles("key")
	c.SetBaseURL(srv.URL)
	if _, err := c.Search(context.Background(), SearchQuery{
		ParentIMDBID: "tt0149460", Season: 1, Episode: 2, Languages: []string{"en"},
	}); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"parent_imdb_id": "0149460", "season_number": "1", "episode_number": "2", "languages": "en",
	} {
		if got.Get(k) != want {
			t.Errorf("%s = %q, want %q (sent %v)", k, got.Get(k), want, got)
		}
	}
	if got.Has("query") || got.Has("imdb_id") {
		t.Errorf("sent %v; an episode by its show's id needs no title and no episode id", got)
	}

	if _, err := c.Search(context.Background(), SearchQuery{ParentTMDBID: "615", Season: 3, Episode: 4}); err != nil {
		t.Fatal(err)
	}
	if got.Get("parent_tmdb_id") != "615" {
		t.Errorf("parent_tmdb_id = %q, want 615", got.Get("parent_tmdb_id"))
	}
}
