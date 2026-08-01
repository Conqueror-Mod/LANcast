package omdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New("test-key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
}

func TestRatingsParsesAndNormalizes(t *testing.T) {
	var gotQuery url.Values
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"Response": "True",
			"imdbVotes": "1,234,567",
			"Ratings": [
				{"Source": "Internet Movie Database", "Value": "7.9/10"},
				{"Source": "Rotten Tomatoes", "Value": "94%"},
				{"Source": "Metacritic", "Value": "81/100"}
			]
		}`))
	})

	got, err := c.Ratings(context.Background(), "tt2543164")
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery.Get("i") != "tt2543164" || gotQuery.Get("apikey") != "test-key" {
		t.Errorf("query = %v, want i=tt2543164 apikey=test-key", gotQuery)
	}

	by := map[string]struct {
		score   float64
		display string
		votes   int
	}{}
	for _, r := range got {
		by[r.Source] = struct {
			score   float64
			display string
			votes   int
		}{r.Score, r.Display, r.Votes}
	}

	if v := by[SourceIMDb]; v.score != 7.9 || v.display != "7.9" || v.votes != 1234567 {
		t.Errorf("imdb = %+v, want score 7.9 display 7.9 votes 1234567", v)
	}
	if v := by[SourceRottenTomatoes]; v.score != 9.4 || v.display != "94%" {
		t.Errorf("rotten_tomatoes = %+v, want score 9.4 display 94%%", v)
	}
	if v := by[SourceMetacritic]; v.score != 8.1 || v.display != "81" {
		t.Errorf("metacritic = %+v, want score 8.1 display 81", v)
	}
}

func TestRatingsNotFoundIsEmptyNotError(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Response":"False","Error":"Incorrect IMDb ID."}`))
	})
	got, err := c.Ratings(context.Background(), "tt0000000")
	if err != nil {
		t.Fatalf("miss should not error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d ratings, want none", len(got))
	}
}

func TestUnconfiguredIsDormant(t *testing.T) {
	called := false
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { called = true })
	c.apiKey = "" // simulate no key configured
	got, err := c.Ratings(context.Background(), "tt2543164")
	if err != nil || got != nil {
		t.Errorf("unconfigured Ratings = (%v, %v), want (nil, nil)", got, err)
	}
	if called {
		t.Error("unconfigured client made a network call")
	}
}

func TestNormalizeIMDbID(t *testing.T) {
	cases := map[string]string{
		"tt2543164": "tt2543164",
		"2543164":   "tt2543164",
		"  tt99  ":  "tt99",
		"":          "",
	}
	for in, want := range cases {
		if got := normalizeIMDbID(in); got != want {
			t.Errorf("normalizeIMDbID(%q) = %q, want %q", in, got, want)
		}
	}
}
