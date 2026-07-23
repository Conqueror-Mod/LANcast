// Package tmdb implements meta.Provider against The Movie Database.
//
// It is the only network provider that ships first. One provider done properly
// proves the interface; three done partially prove nothing.
package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"lancast/internal/meta"
)

const (
	// ID is the provider identifier stored in media_item.provider.
	ID = "tmdb"

	defaultBaseURL = "https://api.themoviedb.org/3"
	imageBaseURL   = "https://image.tmdb.org/t/p/original"

	// Search results change rarely; a day is generous and keeps rescans free.
	cacheTTL = 24 * time.Hour
)

// Cache is the provider response cache. The store satisfies it; tests supply
// their own. Caching raw payloads means a rescan, a re-match, and a refresh of
// the same title cost one API call rather than three.
type Cache interface {
	CachedResponse(ctx context.Context, provider, key string, maxAge time.Duration) ([]byte, bool, error)
	CacheResponse(ctx context.Context, provider, key string, payload []byte) error
}

// Client is a TMDB-backed meta.Provider.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
	limiter *meta.Limiter
	cache   Cache

	// MaxRetries bounds 429 and 5xx retries.
	MaxRetries int
}

// Option customizes a Client.
type Option func(*Client)

func WithBaseURL(u string) Option          { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }
func WithCache(cache Cache) Option         { return func(c *Client) { c.cache = cache } }
func WithLimiter(l *meta.Limiter) Option   { return func(c *Client) { c.limiter = l } }

// New builds a TMDB client. An empty apiKey yields a client that reports
// itself unconfigured rather than failing at call time — LANcast must stay
// fully usable with no key at all.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:     apiKey,
		baseURL:    defaultBaseURL,
		http:       &http.Client{Timeout: 15 * time.Second},
		limiter:    meta.NewLimiter(5, 5),
		MaxRetries: 3,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) ID() string { return ID }

func (c *Client) Caps() meta.Caps {
	return meta.Caps{Movie: true, Show: true, Episode: true, Artwork: true}
}

// Configured reports whether an API key is present.
func (c *Client) Configured() bool { return c.apiKey != "" }

// ErrNotConfigured is returned when no API key is set. Callers treat it as
// "no metadata available", never as a failure to surface to the user.
var ErrNotConfigured = fmt.Errorf("tmdb: no API key configured")

// Search finds candidates for a query.
func (c *Client) Search(ctx context.Context, q meta.Query) ([]meta.Candidate, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}

	title := q.Title
	path := "/search/movie"
	kind := meta.KindMovie
	if q.Kind == meta.KindShow || q.Kind == meta.KindEpisode || q.Kind == meta.KindSeason {
		path = "/search/tv"
		kind = meta.KindShow
		if q.Series != "" {
			title = q.Series
		}
	}
	if strings.TrimSpace(title) == "" {
		return nil, nil
	}

	// The year is deliberately NOT sent to TMDB.
	//
	// Its `year` parameter is a hard filter, so a filename off by one — and
	// they frequently are, since release years and filename years disagree —
	// returns zero results rather than a slightly weaker match. That defeats
	// the entire point of confidence scoring: the provider would be rejecting
	// imperfect data before the code built to handle imperfect data ever runs.
	//
	// Score() already weights year proximity, and it can tell "close" from
	// "wrong" in a way a filter cannot.
	params := url.Values{"query": {title}}

	var raw searchResponse
	if err := c.get(ctx, path, params, &raw); err != nil {
		return nil, err
	}

	out := make([]meta.Candidate, 0, len(raw.Results))
	for _, r := range raw.Results {
		out = append(out, meta.Candidate{
			Provider:   ID,
			ExternalID: strconv.Itoa(r.ID),
			Kind:       kind,
			Title:      r.displayTitle(),
			Year:       yearOf(r.releaseDate()),
			Overview:   r.Overview,
			Popularity: r.Popularity,
			PosterURL:  imageURL(r.PosterPath),
		})
	}
	return out, nil
}

// Fetch retrieves a full record by reference.
func (c *Client) Fetch(ctx context.Context, ref meta.Ref) (*meta.Record, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	switch ref.Kind {
	case meta.KindMovie:
		return c.fetchMovie(ctx, ref.ExternalID)
	case meta.KindShow, meta.KindSeason:
		return c.fetchShow(ctx, ref.ExternalID)
	case meta.KindEpisode:
		return c.fetchEpisode(ctx, ref)
	}
	return nil, fmt.Errorf("tmdb: unsupported kind %q", ref.Kind)
}

func (c *Client) fetchMovie(ctx context.Context, id string) (*meta.Record, error) {
	var m movieDetail
	if err := c.get(ctx, "/movie/"+id, url.Values{"append_to_response": {"credits"}}, &m); err != nil {
		return nil, err
	}

	rec := &meta.Record{Source: ID, ExternalID: id, Kind: meta.KindMovie}
	rec.Fields.Title = meta.S(m.Title)
	if m.Overview != "" {
		rec.Fields.Overview = meta.S(m.Overview)
	}
	if y := yearOf(m.ReleaseDate); y > 0 {
		rec.Fields.Year = meta.I(y)
		if ts, ok := parseDate(m.ReleaseDate); ok {
			rec.Fields.ReleasedAt = meta.I64(ts)
		}
	}
	if m.VoteAverage > 0 {
		rec.Fields.Rating = meta.F(m.VoteAverage)
	}
	if m.Runtime > 0 {
		rec.Fields.DurationMS = meta.I64(int64(m.Runtime) * 60_000)
	}
	rec.Genres = genreNames(m.Genres)
	rec.Credits = convertCredits(m.Credits)
	rec.Artwork = artRefs(m.PosterPath, m.BackdropPath)
	return rec, nil
}

func (c *Client) fetchShow(ctx context.Context, id string) (*meta.Record, error) {
	var s showDetail
	if err := c.get(ctx, "/tv/"+id, url.Values{"append_to_response": {"credits"}}, &s); err != nil {
		return nil, err
	}

	rec := &meta.Record{Source: ID, ExternalID: id, Kind: meta.KindShow}
	rec.Fields.Title = meta.S(s.Name)
	rec.Fields.Series = meta.S(s.Name)
	if s.Overview != "" {
		rec.Fields.Overview = meta.S(s.Overview)
	}
	if y := yearOf(s.FirstAirDate); y > 0 {
		rec.Fields.Year = meta.I(y)
		if ts, ok := parseDate(s.FirstAirDate); ok {
			rec.Fields.ReleasedAt = meta.I64(ts)
		}
	}
	if s.VoteAverage > 0 {
		rec.Fields.Rating = meta.F(s.VoteAverage)
	}
	rec.Genres = genreNames(s.Genres)
	rec.Credits = convertCredits(s.Credits)
	rec.Artwork = artRefs(s.PosterPath, s.BackdropPath)
	return rec, nil
}

func (c *Client) fetchEpisode(ctx context.Context, ref meta.Ref) (*meta.Record, error) {
	path := fmt.Sprintf("/tv/%s/season/%d/episode/%d", ref.ExternalID, ref.Season, ref.Episode)
	var e episodeDetail
	if err := c.get(ctx, path, nil, &e); err != nil {
		return nil, err
	}

	rec := &meta.Record{Source: ID, ExternalID: ref.ExternalID, Kind: meta.KindEpisode}
	rec.Fields.Title = meta.S(e.Name)
	rec.Fields.Season = meta.I(ref.Season)
	rec.Fields.Episode = meta.I(ref.Episode)
	if e.Overview != "" {
		rec.Fields.Overview = meta.S(e.Overview)
	}
	if y := yearOf(e.AirDate); y > 0 {
		rec.Fields.Year = meta.I(y)
		if ts, ok := parseDate(e.AirDate); ok {
			rec.Fields.ReleasedAt = meta.I64(ts)
		}
	}
	if e.VoteAverage > 0 {
		rec.Fields.Rating = meta.F(e.VoteAverage)
	}
	if e.StillPath != "" {
		rec.Artwork = []meta.ArtRef{{Kind: meta.ArtThumb, URL: imageURL(e.StillPath)}}
	}
	return rec, nil
}

// Trailer returns the best promotional video for a title.
//
// Prefers an official trailer over a teaser or clip, and the most recently
// published when several qualify — studios re-upload, and the newest is
// usually the one that still plays.
func (c *Client) Trailer(ctx context.Context, ref meta.Ref) (*meta.Trailer, error) {
	if !c.Configured() || ref.ExternalID == "" {
		return nil, nil
	}

	path := "/movie/" + ref.ExternalID + "/videos"
	if ref.Kind == meta.KindShow || ref.Kind == meta.KindSeason || ref.Kind == meta.KindEpisode {
		path = "/tv/" + ref.ExternalID + "/videos"
	}

	var doc struct {
		Results []struct {
			Name        string `json:"name"`
			Key         string `json:"key"`
			Site        string `json:"site"`
			Type        string `json:"type"`
			Official    bool   `json:"official"`
			PublishedAt string `json:"published_at"`
		} `json:"results"`
	}
	if err := c.get(ctx, path, nil, &doc); err != nil {
		return nil, err
	}

	best := -1
	bestRank := -1
	for i, v := range doc.Results {
		if !strings.EqualFold(v.Site, "YouTube") || v.Key == "" {
			continue
		}
		rank := 0
		switch strings.ToLower(v.Type) {
		case "trailer":
			rank = 3
		case "teaser":
			rank = 2
		case "clip", "featurette":
			rank = 1
		default:
			continue
		}
		if v.Official {
			rank += 4
		}
		if rank > bestRank ||
			(rank == bestRank && best >= 0 && v.PublishedAt > doc.Results[best].PublishedAt) {
			best, bestRank = i, rank
		}
	}
	if best < 0 {
		return nil, nil
	}

	v := doc.Results[best]
	return &meta.Trailer{Site: "YouTube", Key: v.Key, Name: v.Name}, nil
}

// get performs a cached, rate-limited, retrying GET and decodes into out.
func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	if params == nil {
		params = url.Values{}
	}
	cacheKey := path + "?" + params.Encode()

	if c.cache != nil {
		if payload, ok, err := c.cache.CachedResponse(ctx, ID, cacheKey, cacheTTL); err == nil && ok {
			return json.Unmarshal(payload, out)
		}
	}

	params.Set("api_key", c.apiKey)
	endpoint := c.baseURL + path + "?" + params.Encode()

	body, err := c.doWithRetry(ctx, endpoint)
	if err != nil {
		return err
	}
	if c.cache != nil {
		// A cache write failure must not fail the request.
		_ = c.cache.CacheResponse(ctx, ID, cacheKey, body)
	}
	return json.Unmarshal(body, out)
}

func (c *Client) doWithRetry(ctx context.Context, endpoint string) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= c.MaxRetries+1; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
		} else {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()

			switch {
			case resp.StatusCode == http.StatusOK:
				if readErr != nil {
					return nil, readErr
				}
				return body, nil
			case resp.StatusCode == http.StatusUnauthorized:
				// Retrying a bad key just burns quota.
				return nil, fmt.Errorf("tmdb: unauthorized — check the API key")
			case resp.StatusCode == http.StatusNotFound:
				return nil, fmt.Errorf("tmdb: not found")
			case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
				lastErr = fmt.Errorf("tmdb: status %d", resp.StatusCode)
			default:
				return nil, fmt.Errorf("tmdb: status %d", resp.StatusCode)
			}
		}

		if attempt <= c.MaxRetries {
			delay := meta.Backoff(attempt, 500*time.Millisecond, 8*time.Second)
			t := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				t.Stop()
				return nil, ctx.Err()
			case <-t.C:
			}
		}
	}
	return nil, lastErr
}

// ----------------------------------------------------------------- decoding

type searchResponse struct {
	Results []searchResult `json:"results"`
}

type searchResult struct {
	ID           int     `json:"id"`
	Title        string  `json:"title"`          // movies
	Name         string  `json:"name"`           // tv
	ReleaseDate  string  `json:"release_date"`   // movies
	FirstAirDate string  `json:"first_air_date"` // tv
	Overview     string  `json:"overview"`
	Popularity   float64 `json:"popularity"`
	PosterPath   string  `json:"poster_path"`
}

func (r searchResult) displayTitle() string {
	if r.Title != "" {
		return r.Title
	}
	return r.Name
}

func (r searchResult) releaseDate() string {
	if r.ReleaseDate != "" {
		return r.ReleaseDate
	}
	return r.FirstAirDate
}

type tmdbGenre struct {
	Name string `json:"name"`
}

type creditsBlock struct {
	Cast []struct {
		Name      string `json:"name"`
		Character string `json:"character"`
		Order     int    `json:"order"`
	} `json:"cast"`
	Crew []struct {
		Name string `json:"name"`
		Job  string `json:"job"`
	} `json:"crew"`
}

type movieDetail struct {
	Title        string       `json:"title"`
	Overview     string       `json:"overview"`
	ReleaseDate  string       `json:"release_date"`
	Runtime      int          `json:"runtime"`
	VoteAverage  float64      `json:"vote_average"`
	Genres       []tmdbGenre  `json:"genres"`
	PosterPath   string       `json:"poster_path"`
	BackdropPath string       `json:"backdrop_path"`
	Credits      creditsBlock `json:"credits"`
}

type showDetail struct {
	Name         string       `json:"name"`
	Overview     string       `json:"overview"`
	FirstAirDate string       `json:"first_air_date"`
	VoteAverage  float64      `json:"vote_average"`
	Genres       []tmdbGenre  `json:"genres"`
	PosterPath   string       `json:"poster_path"`
	BackdropPath string       `json:"backdrop_path"`
	Credits      creditsBlock `json:"credits"`
}

type episodeDetail struct {
	Name        string  `json:"name"`
	Overview    string  `json:"overview"`
	AirDate     string  `json:"air_date"`
	VoteAverage float64 `json:"vote_average"`
	StillPath   string  `json:"still_path"`
}

func genreNames(gs []tmdbGenre) []string {
	if len(gs) == 0 {
		return nil
	}
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		if g.Name != "" {
			out = append(out, g.Name)
		}
	}
	return out
}

// convertCredits keeps the top billed cast plus directors and writers. A full
// crew list is hundreds of people nobody browses by.
func convertCredits(c creditsBlock) []meta.Credit {
	var out []meta.Credit
	for i, m := range c.Cast {
		if i >= 20 {
			break
		}
		out = append(out, meta.Credit{
			Name: m.Name, Role: "actor", Character: m.Character, Order: m.Order,
		})
	}
	for _, m := range c.Crew {
		switch m.Job {
		case "Director":
			out = append(out, meta.Credit{Name: m.Name, Role: "director"})
		case "Writer", "Screenplay":
			out = append(out, meta.Credit{Name: m.Name, Role: "writer"})
		}
	}
	return out
}

func artRefs(poster, backdrop string) []meta.ArtRef {
	var out []meta.ArtRef
	if poster != "" {
		out = append(out, meta.ArtRef{Kind: meta.ArtPoster, URL: imageURL(poster)})
	}
	if backdrop != "" {
		out = append(out, meta.ArtRef{Kind: meta.ArtFanart, URL: imageURL(backdrop)})
	}
	return out
}

func imageURL(path string) string {
	if path == "" {
		return ""
	}
	return imageBaseURL + path
}

func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}
	return y
}

func parseDate(date string) (int64, bool) {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0, false
	}
	return t.Unix(), true
}
