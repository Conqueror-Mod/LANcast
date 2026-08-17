package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lancast/internal/meta"
)

// fakeTMDB serves canned responses so the suite never touches the network.
func fakeTMDB(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func newClient(t *testing.T, srv *httptest.Server, opts ...Option) *Client {
	t.Helper()
	all := append([]Option{
		WithBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithLimiter(meta.NewLimiter(1000, 1000)),
	}, opts...)
	return New("test-key", all...)
}

const movieSearchJSON = `{"results":[
 {"id":335984,"title":"Blade Runner 2049","release_date":"2017-10-04","overview":"K discovers a secret.","popularity":42.5,"poster_path":"/br.jpg"},
 {"id":78,"title":"Blade Runner","release_date":"1982-06-25","overview":"Deckard hunts replicants.","popularity":61.0,"poster_path":"/br82.jpg"}]}`

func TestSearchMovies(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/search/movie") {
			t.Errorf("path = %q, want /search/movie", r.URL.Path)
		}
		if r.URL.Query().Get("query") != "Blade Runner 2049" {
			t.Errorf("query = %q", r.URL.Query().Get("query"))
		}
		if r.URL.Query().Get("api_key") != "test-key" {
			t.Error("api_key was not sent")
		}
		w.Write([]byte(movieSearchJSON))
	})

	cands, err := newClient(t, srv).Search(context.Background(),
		meta.Query{Kind: meta.KindMovie, Title: "Blade Runner 2049", Year: 2017})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("got %d candidates, want 2", len(cands))
	}
	if cands[0].ExternalID != "335984" || cands[0].Year != 2017 {
		t.Errorf("first = %+v, want id 335984 year 2017", cands[0])
	}
	if cands[0].Provider != ID {
		t.Errorf("Provider = %q, want %q", cands[0].Provider, ID)
	}
}

// The end-to-end disambiguation: search returns both films, ranking picks the
// right one, and the wrong one stays below the auto-accept threshold.
func TestSearchThenRankPicksCorrectFilm(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(movieSearchJSON))
	})

	q := meta.Query{Kind: meta.KindMovie, Title: "Blade Runner 2049", Year: 2017}
	cands, err := newClient(t, srv).Search(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	meta.Rank(q, cands)

	if cands[0].ExternalID != "335984" {
		t.Fatalf("best match = %q (%s), want Blade Runner 2049", cands[0].Title, cands[0].ExternalID)
	}
	if meta.StateFor(cands[0].Score) != meta.StateMatched {
		t.Errorf("best score %.3f gives state %q, want matched", cands[0].Score, meta.StateFor(cands[0].Score))
	}
	if cands[1].Score >= meta.ThresholdAuto {
		t.Errorf("wrong film scored %.3f — it would auto-apply", cands[1].Score)
	}
}

// TMDB's year parameter is a hard filter, so sending it means a filename with
// a slightly wrong year returns nothing at all instead of a lower-scored
// match. Scoring handles year proximity; the provider must not pre-filter.
func TestSearchDoesNotSendYearAsFilter(t *testing.T) {
	var gotYear string
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		gotYear = r.URL.Query().Get("year") + r.URL.Query().Get("primary_release_year")
		w.Write([]byte(movieSearchJSON))
	})

	_, err := newClient(t, srv).Search(context.Background(),
		meta.Query{Kind: meta.KindMovie, Title: "Blade Runner 2049", Year: 2017})
	if err != nil {
		t.Fatal(err)
	}
	if gotYear != "" {
		t.Errorf("year filter sent as %q — a wrong filename year would return no results", gotYear)
	}
}

// The real-world case: the file says 2021, the film is 2022. Without a hard
// filter the candidate still comes back, and scoring places it just below the
// auto threshold rather than losing it entirely.
func TestOffByOneYearStillMatches(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"results":[{"id":616037,"title":"Thor: Love and Thunder","release_date":"2022-07-06","popularity":55}]}`))
	})

	q := meta.Query{Kind: meta.KindMovie, Title: "Thor Love and Thunder", Year: 2021}
	cands, err := newClient(t, srv).Search(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want the film despite the wrong year", len(cands))
	}
	meta.Rank(q, cands)
	if state := meta.StateFor(cands[0].Score); state == meta.StateUnmatched {
		t.Errorf("score %.3f gives %q — a one-year slip should not lose the match",
			cands[0].Score, state)
	}
}

func TestSearchTVUsesSeriesName(t *testing.T) {
	var gotQuery string
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/search/tv") {
			t.Errorf("path = %q, want /search/tv", r.URL.Path)
		}
		gotQuery = r.URL.Query().Get("query")
		w.Write([]byte(`{"results":[{"id":83867,"name":"Andor","first_air_date":"2022-09-21","popularity":30}]}`))
	})

	cands, err := newClient(t, srv).Search(context.Background(),
		meta.Query{Kind: meta.KindEpisode, Title: "Announcement", Series: "Andor", Season: 1, Episode: 7})
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "Andor" {
		t.Errorf("query = %q, want the series name, not the episode title", gotQuery)
	}
	if len(cands) != 1 || cands[0].Kind != meta.KindShow {
		t.Errorf("candidates = %+v, want one show", cands)
	}
}

func TestFetchMovie(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/movie/335984" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("append_to_response") != "credits" {
			t.Error("credits were not requested in the same call")
		}
		w.Write([]byte(`{
		 "title":"Blade Runner 2049","overview":"K discovers a secret.",
		 "release_date":"2017-10-04","runtime":164,"vote_average":7.5,
		 "imdb_id":"tt1856101",
		 "genres":[{"name":"Science Fiction"},{"name":"Drama"}],
		 "poster_path":"/p.jpg","backdrop_path":"/b.jpg",
		 "credits":{"cast":[{"name":"Ryan Gosling","character":"K","order":0}],
		            "crew":[{"name":"Denis Villeneuve","job":"Director"},
		                    {"name":"Some Gaffer","job":"Gaffer"}]}}`))
	})

	rec, err := newClient(t, srv).Fetch(context.Background(),
		meta.Ref{Kind: meta.KindMovie, ExternalID: "335984"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if *rec.Fields.Title != "Blade Runner 2049" {
		t.Errorf("Title = %q", *rec.Fields.Title)
	}
	if *rec.Fields.Year != 2017 {
		t.Errorf("Year = %d, want 2017", *rec.Fields.Year)
	}
	if *rec.Fields.DurationMS != 164*60_000 {
		t.Errorf("DurationMS = %d, want runtime converted from minutes", *rec.Fields.DurationMS)
	}
	if *rec.Fields.Rating != 7.5 {
		t.Errorf("Rating = %v", *rec.Fields.Rating)
	}
	if rec.IMDbID != "tt1856101" {
		t.Errorf("IMDbID = %q, want tt1856101 (the OMDb join key)", rec.IMDbID)
	}
	if rec.Fields.ReleasedAt == nil {
		t.Error("ReleasedAt was not parsed")
	}
	if len(rec.Genres) != 2 {
		t.Errorf("Genres = %v, want 2", rec.Genres)
	}
	if len(rec.Artwork) != 2 {
		t.Errorf("Artwork = %+v, want poster and fanart", rec.Artwork)
	}
	if !strings.HasPrefix(rec.Artwork[0].URL, "https://image.tmdb.org") {
		t.Errorf("artwork URL = %q, want an absolute image URL", rec.Artwork[0].URL)
	}

	// Cast plus director and writer only — a full crew list is hundreds of
	// people nobody browses by.
	var roles []string
	for _, c := range rec.Credits {
		roles = append(roles, c.Role)
	}
	if len(rec.Credits) != 2 {
		t.Errorf("credits = %v (%d), want actor + director and no gaffer", roles, len(rec.Credits))
	}
}

// A movie in a franchise carries its belongs_to_collection through as a
// CollectionRef, so the enricher can group it (ADR 0017). A standalone film
// leaves Collection nil.
func TestFetchMovieCollection(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
		 "title":"The Fellowship of the Ring","release_date":"2001-12-19",
		 "belongs_to_collection":{"id":119,"name":"The Lord of the Rings Collection",
		   "poster_path":"/cp.jpg","backdrop_path":"/cb.jpg"}}`))
	})

	rec, err := newClient(t, srv).Fetch(context.Background(),
		meta.Ref{Kind: meta.KindMovie, ExternalID: "120"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if rec.Collection == nil {
		t.Fatal("Collection is nil, want the LOTR collection")
	}
	if rec.Collection.ExternalID != "119" {
		t.Errorf("Collection.ExternalID = %q, want 119", rec.Collection.ExternalID)
	}
	if rec.Collection.Name != "The Lord of the Rings Collection" {
		t.Errorf("Collection.Name = %q", rec.Collection.Name)
	}
	if len(rec.Collection.Artwork) != 2 {
		t.Errorf("Collection.Artwork = %+v, want poster and backdrop", rec.Collection.Artwork)
	}
}

func TestFetchMovieNoCollection(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"title":"Arrival","release_date":"2016-11-11"}`))
	})
	rec, err := newClient(t, srv).Fetch(context.Background(),
		meta.Ref{Kind: meta.KindMovie, ExternalID: "329865"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if rec.Collection != nil {
		t.Errorf("Collection = %+v, want nil for a standalone film", rec.Collection)
	}
}

func TestFetchEpisode(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tv/83867/season/1/episode/7" {
			t.Errorf("path = %q, want the exact episode path", r.URL.Path)
		}
		w.Write([]byte(`{"name":"Announcement","overview":"Cassian is arrested.","air_date":"2022-10-26","vote_average":8.1,"still_path":"/s.jpg"}`))
	})

	rec, err := newClient(t, srv).Fetch(context.Background(),
		meta.Ref{Kind: meta.KindEpisode, ExternalID: "83867", Season: 1, Episode: 7})
	if err != nil {
		t.Fatal(err)
	}
	if *rec.Fields.Title != "Announcement" {
		t.Errorf("Title = %q", *rec.Fields.Title)
	}
	if *rec.Fields.Season != 1 || *rec.Fields.Episode != 7 {
		t.Errorf("numbering = %d/%d, want 1/7", *rec.Fields.Season, *rec.Fields.Episode)
	}
}

func TestFetchSeasonUsesTheShowID(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tv/31497/season/2" {
			t.Errorf("path = %q, want the exact season path", r.URL.Path)
		}
		w.Write([]byte(`{"name":"Season 2","overview":"The second season.","air_date":"2010-09-16","vote_average":7.4,"poster_path":"/s2.jpg"}`))
	})

	rec, err := newClient(t, srv).Fetch(context.Background(),
		meta.Ref{Kind: meta.KindSeason, ExternalID: "31497", Season: 2})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Kind != meta.KindSeason {
		t.Errorf("Kind = %q, want season", rec.Kind)
	}
	if *rec.Fields.Title != "Season 2" {
		t.Errorf("Title = %q, want the season's own name", *rec.Fields.Title)
	}
	if *rec.Fields.Season != 2 {
		t.Errorf("Season = %d, want 2", *rec.Fields.Season)
	}
	// The season's poster, not the show's, and no fanart of its own.
	if len(rec.Artwork) != 1 || rec.Artwork[0].Kind != meta.ArtPoster {
		t.Fatalf("Artwork = %+v, want exactly one poster", rec.Artwork)
	}
	// A season is a position inside a show. Claiming the show's name as this
	// row's series is what let a season masquerade as its parent in a grid.
	if rec.Fields.Series != nil {
		t.Errorf("Series = %q, want nothing", *rec.Fields.Series)
	}
}

// A season's name is a position, not a title. Searching for one returns real
// shows that merely contain "Season 2" in their names, and those score as exact
// title matches — which is how one Thai drama became the poster for season 2 of
// nine unrelated series. The query must never leave the process.
func TestSeasonIsNeverSearchedByName(t *testing.T) {
	var called int32
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)
		w.Write([]byte(`{"results":[]}`))
	})

	for _, q := range []meta.Query{
		{Kind: meta.KindSeason, Title: "Season 2", Season: 2},
		{Kind: meta.KindSeason, Title: "The League S02", Series: "The League", Season: 2},
	} {
		cands, err := newClient(t, srv).Search(context.Background(), q)
		if err != nil {
			t.Fatalf("Search(%+v): %v", q, err)
		}
		if len(cands) != 0 {
			t.Errorf("Search(%+v) returned %d candidates, want none", q, len(cands))
		}
	}
	if n := atomic.LoadInt32(&called); n != 0 {
		t.Errorf("provider was called %d times for a season search, want 0", n)
	}
}

// LANcast must be fully usable with no key. An unconfigured provider reports
// that fact rather than producing an error to show the user.
func TestUnconfiguredClient(t *testing.T) {
	c := New("")
	if c.Configured() {
		t.Error("Configured = true with an empty key")
	}
	if _, err := c.Search(context.Background(), meta.Query{Title: "x"}); err != ErrNotConfigured {
		t.Errorf("Search error = %v, want ErrNotConfigured", err)
	}
	if _, err := c.Fetch(context.Background(), meta.Ref{Kind: meta.KindMovie}); err != ErrNotConfigured {
		t.Errorf("Fetch error = %v, want ErrNotConfigured", err)
	}
}

func TestEmptyTitleDoesNotCallAPI(t *testing.T) {
	var called int32
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)
		w.Write([]byte(`{"results":[]}`))
	})

	cands, err := newClient(t, srv).Search(context.Background(), meta.Query{Kind: meta.KindMovie, Title: "   "})
	if err != nil || len(cands) != 0 {
		t.Errorf("got %v, %v; want no candidates and no error", cands, err)
	}
	if atomic.LoadInt32(&called) != 0 {
		t.Error("an empty title still hit the API")
	}
}

// memCache is a minimal Cache for testing.
type memCache struct {
	mu    sync.Mutex
	data  map[string][]byte
	reads int
}

func newMemCache() *memCache { return &memCache{data: map[string][]byte{}} }

func (m *memCache) CachedResponse(_ context.Context, _, key string, _ time.Duration) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if ok {
		m.reads++
	}
	return v, ok, nil
}

func (m *memCache) CacheResponse(_ context.Context, _, key string, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = payload
	return nil
}

// A rescan, a re-match, and a refresh of the same title must cost one API call,
// not three.
func TestResponsesAreCached(t *testing.T) {
	var calls int32
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Write([]byte(movieSearchJSON))
	})

	cache := newMemCache()
	c := newClient(t, srv, WithCache(cache))
	q := meta.Query{Kind: meta.KindMovie, Title: "Blade Runner 2049", Year: 2017}

	for i := 0; i < 3; i++ {
		if _, err := c.Search(context.Background(), q); err != nil {
			t.Fatal(err)
		}
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("%d API calls for 3 identical searches, want 1", n)
	}
	if cache.reads != 2 {
		t.Errorf("cache reads = %d, want 2", cache.reads)
	}
}

// A 429 must back off and retry rather than cascading into failure.
func TestRetriesOnRateLimit(t *testing.T) {
	var calls int32
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(movieSearchJSON))
	})

	c := newClient(t, srv)
	cands, err := c.Search(context.Background(), meta.Query{Kind: meta.KindMovie, Title: "Blade Runner 2049"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(cands) != 2 {
		t.Errorf("got %d candidates after retry, want 2", len(cands))
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Errorf("calls = %d, want 3 (two 429s then success)", calls)
	}
}

// Retrying a rejected key just burns quota and delays a clear error.
func TestUnauthorizedIsNotRetried(t *testing.T) {
	var calls int32
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusUnauthorized)
	})

	_, err := newClient(t, srv).Search(context.Background(), meta.Query{Kind: meta.KindMovie, Title: "x"})
	if err == nil {
		t.Fatal("want an error for a rejected key")
	}
	if !strings.Contains(err.Error(), "API key") {
		t.Errorf("error = %v, want it to name the API key", err)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("calls = %d, want 1 — a bad key must not be retried", n)
	}
}

func TestGivesUpAfterMaxRetries(t *testing.T) {
	var calls int32
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	})

	c := newClient(t, srv)
	c.MaxRetries = 2
	if _, err := c.Search(context.Background(), meta.Query{Kind: meta.KindMovie, Title: "x"}); err == nil {
		t.Fatal("want an error after exhausting retries")
	}
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Errorf("calls = %d, want 3 (initial + 2 retries)", n)
	}
}

func TestContextCancellationStopsRetries(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := newClient(t, srv).Search(ctx, meta.Query{Kind: meta.KindMovie, Title: "x"}); err == nil {
		t.Fatal("want an error when the context expires")
	}
}
