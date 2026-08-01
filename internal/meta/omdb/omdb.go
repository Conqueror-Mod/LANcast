// Package omdb implements meta.RatingSource against the OMDb API.
//
// OMDb is not a metadata Provider: it does not search for identity, it answers
// "what do IMDb, Rotten Tomatoes, and Metacritic say about this imdb id?" That
// is exactly meta.RatingSource, and forcing it into the Provider shape would
// mean faking a confidence-scored candidate for a service with no opinion on
// what a title is (ADR 0007, ADR 0019).
package omdb

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
	// ID is the rating-source identifier. The per-service scores it returns carry
	// their own source ids ("imdb", "rotten_tomatoes", "metacritic").
	ID = "omdb"

	defaultBaseURL = "https://www.omdbapi.com"

	// Ratings drift slowly and the free tier is 1,000 requests/day, so the cache
	// window is generous — a rescan of a rated library should cost nothing.
	cacheTTL = 7 * 24 * time.Hour
)

// Source ids for the individual scores OMDb aggregates. These are the values
// stored in item_rating.source and are a stable part of the API surface.
const (
	SourceIMDb           = "imdb"
	SourceRottenTomatoes = "rotten_tomatoes"
	SourceMetacritic     = "metacritic"
)

// Cache is the response cache. The store satisfies it; tests supply their own.
type Cache interface {
	CachedResponse(ctx context.Context, provider, key string, maxAge time.Duration) ([]byte, bool, error)
	CacheResponse(ctx context.Context, provider, key string, payload []byte) error
}

// Client is an OMDb-backed meta.RatingSource.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
	limiter *meta.Limiter
	cache   Cache

	MaxRetries int
}

// Option customizes a Client.
type Option func(*Client)

func WithBaseURL(u string) Option          { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }
func WithCache(cache Cache) Option         { return func(c *Client) { c.cache = cache } }
func WithLimiter(l *meta.Limiter) Option   { return func(c *Client) { c.limiter = l } }

// New builds an OMDb client. An empty apiKey yields a client that reports itself
// unconfigured; the enricher registers a rating source only when it is
// configured, so no key means the feature is simply dormant (no phone-home).
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:     apiKey,
		baseURL:    defaultBaseURL,
		http:       &http.Client{Timeout: 15 * time.Second},
		limiter:    meta.NewLimiter(5, 5),
		MaxRetries: 2,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) ID() string { return ID }

// Configured reports whether an API key is present.
func (c *Client) Configured() bool { return c.apiKey != "" }

// response is the subset of OMDb's payload we read. The Ratings array is the
// authoritative per-service list; imdbVotes gives IMDb its vote count.
type response struct {
	Response  string `json:"Response"` // "True" | "False"
	Error     string `json:"Error"`
	IMDbVotes string `json:"imdbVotes"`
	Ratings   []struct {
		Source string `json:"Source"`
		Value  string `json:"Value"`
	} `json:"Ratings"`
}

// Ratings returns IMDb, Rotten Tomatoes, and Metacritic scores for an imdb id,
// each normalized to 0–10 and carrying its native display string. A title OMDb
// does not know is not an error — it is a normal empty result, since coverage
// thins out for older, non-US, and TV titles.
func (c *Client) Ratings(ctx context.Context, imdbID string) ([]meta.Rating, error) {
	if !c.Configured() {
		return nil, nil
	}
	imdbID = normalizeIMDbID(imdbID)
	if imdbID == "" {
		return nil, nil
	}

	var doc response
	if err := c.get(ctx, imdbID, &doc); err != nil {
		return nil, err
	}
	if !strings.EqualFold(doc.Response, "True") {
		// "Incorrect IMDb ID" / "Movie not found" — a miss, not a failure.
		return nil, nil
	}

	var out []meta.Rating
	for _, r := range doc.Ratings {
		source, score, ok := parseRating(r.Source, r.Value)
		if !ok {
			continue
		}
		rating := meta.Rating{Source: source, Score: score, Display: displayFor(r.Value)}
		if source == SourceIMDb {
			rating.Votes = parseVotes(doc.IMDbVotes)
		}
		out = append(out, rating)
	}
	return out, nil
}

// parseRating maps an OMDb rating entry to a source id and a normalized 0–10
// score. IMDb is already /10; Rotten Tomatoes ("94%") and Metacritic ("81/100")
// are /100, so their leading number is divided by ten. Unknown sources are
// skipped rather than guessed at.
func parseRating(source, value string) (string, float64, bool) {
	n, ok := leadingNumber(value)
	if !ok {
		return "", 0, false
	}
	switch source {
	case "Internet Movie Database":
		return SourceIMDb, n, true
	case "Rotten Tomatoes":
		return SourceRottenTomatoes, n / 10, true
	case "Metacritic":
		return SourceMetacritic, n / 10, true
	}
	return "", 0, false
}

// leadingNumber parses the numeric prefix of "7.9/10", "94%", or "81/100".
func leadingNumber(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	end := 0
	for end < len(value) && (value[end] == '.' || (value[end] >= '0' && value[end] <= '9')) {
		end++
	}
	if end == 0 {
		return 0, false
	}
	n, err := strconv.ParseFloat(value[:end], 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// displayFor keeps each source's native presentation: the part before a slash
// ("7.9/10" → "7.9", "81/100" → "81") and a percentage as-is ("94%").
func displayFor(value string) string {
	value = strings.TrimSpace(value)
	if i := strings.IndexByte(value, '/'); i >= 0 {
		return value[:i]
	}
	return value
}

// parseVotes turns OMDb's grouped count ("1,234,567") into an int; "N/A" and
// junk yield 0.
func parseVotes(s string) int {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// normalizeIMDbID ensures the tt-prefixed form OMDb expects, tolerating a bare
// numeric id.
func normalizeIMDbID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if strings.HasPrefix(id, "tt") {
		return id
	}
	if _, err := strconv.Atoi(id); err == nil {
		return "tt" + id
	}
	return id
}

// get performs a cached, rate-limited, retrying lookup by imdb id.
func (c *Client) get(ctx context.Context, imdbID string, out any) error {
	if c.cache != nil {
		if payload, ok, err := c.cache.CachedResponse(ctx, ID, imdbID, cacheTTL); err == nil && ok {
			return json.Unmarshal(payload, out)
		}
	}

	params := url.Values{"i": {imdbID}, "apikey": {c.apiKey}}
	endpoint := c.baseURL + "/?" + params.Encode()

	body, err := c.doWithRetry(ctx, endpoint)
	if err != nil {
		return err
	}
	if c.cache != nil {
		_ = c.cache.CacheResponse(ctx, ID, imdbID, body)
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
				return nil, fmt.Errorf("omdb: unauthorized — check the API key")
			case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
				lastErr = fmt.Errorf("omdb: status %d", resp.StatusCode)
			default:
				return nil, fmt.Errorf("omdb: status %d", resp.StatusCode)
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
