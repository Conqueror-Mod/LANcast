package subtitle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrNoProvider is returned when subtitle search is not configured.
var ErrNoProvider = errors.New("no subtitle provider is configured")

// ErrQuotaExhausted is returned when the daily download allowance is spent.
var ErrQuotaExhausted = errors.New("subtitle download quota exhausted for today")

const (
	osBaseURL = "https://api.opensubtitles.com/api/v1"
	// OpenSubtitles requires a descriptive User-Agent and rejects requests
	// without one.
	osUserAgent = "LANcast v0.3"
)

// Candidate is one search result, before or after scoring.
type Candidate struct {
	FileID          int64   `json:"file_id"`
	FileName        string  `json:"file_name"`
	Release         string  `json:"release"`
	Language        string  `json:"language"`
	DownloadCount   int     `json:"download_count"`
	FPS             float64 `json:"fps,omitempty"`
	HashMatch       bool    `json:"hash_match"`
	HearingImpaired bool    `json:"hearing_impaired"`
	Forced          bool    `json:"forced"`
	Uploader        string  `json:"uploader,omitempty"`

	// Filled in by Rank.
	Score  float64 `json:"score"`
	Reason string  `json:"reason,omitempty"`
}

// SearchQuery describes what to look for.
type SearchQuery struct {
	Query     string
	Year      int
	Languages []string
	MovieHash string
	IMDBID    string
	TMDBID    string
	Season    int
	Episode   int
}

// OpenSubtitles is a client for the v1 REST API.
type OpenSubtitles struct {
	apiKey string
	token  string // optional JWT from login, raises the download quota
	http   *http.Client
	base   string
}

// NewOpenSubtitles builds a client. An empty key yields a client that reports
// itself unconfigured rather than failing at call time.
func NewOpenSubtitles(apiKey string) *OpenSubtitles {
	return &OpenSubtitles{
		apiKey: strings.TrimSpace(apiKey),
		http:   &http.Client{Timeout: 20 * time.Second},
		base:   osBaseURL,
	}
}

// SetBaseURL points the client at a different host, for tests.
func (c *OpenSubtitles) SetBaseURL(u string) { c.base = strings.TrimRight(u, "/") }

// SetToken supplies a login token for a higher download quota.
func (c *OpenSubtitles) SetToken(t string) { c.token = strings.TrimSpace(t) }

// Configured reports whether an API key is present.
func (c *OpenSubtitles) Configured() bool { return c.apiKey != "" }

// Search finds subtitle candidates.
func (c *OpenSubtitles) Search(ctx context.Context, q SearchQuery) ([]Candidate, error) {
	if !c.Configured() {
		return nil, ErrNoProvider
	}

	params := url.Values{}
	if q.MovieHash != "" {
		params.Set("moviehash", q.MovieHash)
	}
	if q.Query != "" {
		params.Set("query", q.Query)
	}
	if q.Year > 0 {
		params.Set("year", strconv.Itoa(q.Year))
	}
	if q.IMDBID != "" {
		params.Set("imdb_id", strings.TrimPrefix(q.IMDBID, "tt"))
	}
	if q.TMDBID != "" {
		params.Set("tmdb_id", q.TMDBID)
	}
	if q.Season > 0 {
		params.Set("season_number", strconv.Itoa(q.Season))
	}
	if q.Episode > 0 {
		params.Set("episode_number", strconv.Itoa(q.Episode))
	}
	if len(q.Languages) > 0 {
		params.Set("languages", strings.Join(q.Languages, ","))
	}
	if len(params) == 0 {
		return nil, fmt.Errorf("subtitle search: nothing to search for")
	}

	var doc osSearchResponse
	if err := c.get(ctx, "/subtitles?"+params.Encode(), &doc); err != nil {
		return nil, err
	}

	out := make([]Candidate, 0, len(doc.Data))
	for _, d := range doc.Data {
		a := d.Attributes
		if len(a.Files) == 0 {
			continue
		}
		out = append(out, Candidate{
			FileID:          a.Files[0].FileID,
			FileName:        a.Files[0].FileName,
			Release:         a.Release,
			Language:        NormalizeLanguage(a.Language),
			DownloadCount:   a.DownloadCount,
			FPS:             a.FPS,
			HashMatch:       a.MovieHashMatch,
			HearingImpaired: a.HearingImpaired,
			Forced:          a.ForeignPartsOnly,
			Uploader:        a.Uploader.Name,
		})
	}
	return out, nil
}

// DownloadLink exchanges a file id for a temporary download URL. This is the
// call that consumes daily quota.
func (c *OpenSubtitles) DownloadLink(ctx context.Context, fileID int64) (string, string, error) {
	if !c.Configured() {
		return "", "", ErrNoProvider
	}

	body, err := json.Marshal(map[string]any{"file_id": fileID})
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/download", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("subtitle download: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusPaymentRequired {
		return "", "", ErrQuotaExhausted
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("subtitle download: status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	var doc struct {
		Link      string `json:"link"`
		FileName  string `json:"file_name"`
		Remaining int    `json:"remaining"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", "", fmt.Errorf("subtitle download: %w", err)
	}
	if doc.Link == "" {
		return "", "", fmt.Errorf("subtitle download: no link returned")
	}
	return doc.Link, doc.FileName, nil
}

// Fetch retrieves the subtitle body from a download link.
func (c *OpenSubtitles) Fetch(ctx context.Context, link string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch subtitle: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch subtitle: status %d", resp.StatusCode)
	}
	// Subtitle files are text; a cap keeps a mislabelled URL from filling
	// memory.
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

func (c *OpenSubtitles) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("subtitle search: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		// Retrying a rejected key only burns quota, same as TMDB.
		return fmt.Errorf("subtitle search: API key was rejected")
	case resp.StatusCode == http.StatusTooManyRequests:
		return ErrQuotaExhausted
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("subtitle search: status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	return json.Unmarshal(raw, out)
}

func (c *OpenSubtitles) setHeaders(req *http.Request) {
	req.Header.Set("Api-Key", c.apiKey)
	req.Header.Set("User-Agent", osUserAgent)
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

type osSearchResponse struct {
	Data []struct {
		Attributes struct {
			Language         string  `json:"language"`
			DownloadCount    int     `json:"download_count"`
			HearingImpaired  bool    `json:"hearing_impaired"`
			ForeignPartsOnly bool    `json:"foreign_parts_only"`
			FPS              float64 `json:"fps"`
			Release          string  `json:"release"`
			MovieHashMatch   bool    `json:"moviehash_match"`
			Uploader         struct {
				Name string `json:"name"`
			} `json:"uploader"`
			Files []struct {
				FileID   int64  `json:"file_id"`
				FileName string `json:"file_name"`
			} `json:"files"`
		} `json:"attributes"`
	} `json:"data"`
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
