/*
 * Package musicbrainz identifies albums and artists in MusicBrainz (ADR 0115).
 *
 * It reports what MusicBrainz says and nothing more. It does not decide what
 * an album keeps: the file's tags win for every field, and MusicBrainz fills
 * only what an album has none of. That rule is applied where records are
 * merged into music items, because the shared merge puts a provider above the
 * value an item already holds, which is right for a film named after its file
 * and wrong for an album named by its tags.
 *
 * Its two obligations to MusicBrainz are not optional: one request a second
 * on average, and a User-Agent naming the application and a way to reach its
 * maintainers. A 503 means busy, not "no such album", and is returned as
 * ErrBusy so nothing records a busy hour as a missing match.
 */
package musicbrainz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"lancast/internal/meta"
)

const (
	// ID is the provider id stored beside a matched item.
	ID = "musicbrainz"

	defaultBaseURL = "https://musicbrainz.org/ws/2"
	contact        = "https://github.com/Conqueror-Mod/LANcast"
	cacheTTL       = 24 * time.Hour
	// searchLimit is wide because a famous title has many records: the studio
	// "Ace of Spades" came sixth, behind three compilations and two singles.
	searchLimit = 25
	// maxGenres keeps the strongest few. A popular record carries a dozen
	// tags, most with a single vote.
	maxGenres = 4
)

// ErrBusy is MusicBrainz declining requests (503, or 429), after retries.
// It means try later. It never means the album has no match.
var ErrBusy = errors.New("musicbrainz: busy, try later")

// ErrNotFound is an id MusicBrainz does not know.
var ErrNotFound = errors.New("musicbrainz: not found")

// Cache keeps responses so a refresh does not ask again. The same contract as
// the TMDB client's, so one store serves both.
type Cache interface {
	CachedResponse(ctx context.Context, provider, key string, maxAge time.Duration) ([]byte, bool, error)
	CacheResponse(ctx context.Context, provider, key string, payload []byte) error
}

// Client talks to the MusicBrainz web service.
type Client struct {
	baseURL   string
	userAgent string
	http      *http.Client
	limiter   *meta.Limiter
	cache     Cache
	// MaxRetries is how many times a busy answer is retried before ErrBusy.
	MaxRetries int
	// retryBase is the first wait after a busy answer; it doubles each time.
	retryBase time.Duration
}

// Option configures a Client.
type Option func(*Client)

func WithBaseURL(u string) Option          { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }
func WithCache(cache Cache) Option         { return func(c *Client) { c.cache = cache } }
func WithLimiter(l *meta.Limiter) Option   { return func(c *Client) { c.limiter = l } }

// WithRetryBase shortens the wait after a busy answer, for tests.
func WithRetryBase(d time.Duration) Option { return func(c *Client) { c.retryBase = d } }

/*
 * New builds a client for this build's version.
 *
 * The User-Agent names the project's address, never the server owner's: the
 * maintainers are who MusicBrainz needs to reach, and nothing about the
 * person running a server is MusicBrainz's business.
 *
 * The limiter allows a little under one request a second with no burst. The
 * rule is "on average", and a client that sits exactly on it is over it the
 * first time two answers arrive close together.
 */
func New(version string, opts ...Option) *Client {
	c := &Client{
		baseURL:    defaultBaseURL,
		userAgent:  UserAgent(version),
		http:       &http.Client{Timeout: 20 * time.Second},
		limiter:    meta.NewLimiter(0.9, 1),
		MaxRetries: 3,
		retryBase:  2 * time.Second,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// UserAgent is the header MusicBrainz asks every client to send.
func UserAgent(version string) string {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if v == "" {
		v = "dev"
	}
	return "LANcast/" + v + " ( " + contact + " )"
}

func (c *Client) ID() string { return ID }

// Caps: albums and artists, nothing else. Cover art is a separate service
// (the Cover Art Archive) and a separate step.
func (c *Client) Caps() meta.Caps { return meta.Caps{Music: true} }

/*
 * Search finds release groups for an album, by its title and album artist.
 *
 * Only albums are searched. An artist is identified through its matched
 * albums (ADR 0115): a name search for "Nirvana" returns several artists and
 * nothing in a library says which one.
 */
func (c *Client) Search(ctx context.Context, q meta.Query) ([]meta.Candidate, error) {
	if q.Kind != meta.KindAlbum {
		return nil, nil
	}
	/*
	 * The exact search first, then looser ones only when it finds nothing.
	 *
	 * A phrase is exact about characters a tag rarely is: "4x4=12" against
	 * MusicBrainz's "4×4=12" finds nothing as a phrase. The words find it, and
	 * the artist check in scoring still guards what comes back. The fallbacks
	 * cost a request each and only for albums the first search missed.
	 */
	for _, query := range []string{
		SearchQuery(q.Title, q.Artist),
		wordQuery(q.Title, q.Artist),
		wordQuery(q.Title, ""),
	} {
		if query == "" {
			continue
		}
		params := url.Values{}
		params.Set("query", query)
		params.Set("limit", strconv.Itoa(searchLimit))
		var resp searchResponse
		if err := c.get(ctx, "/release-group", params, &resp); err != nil {
			return nil, err
		}
		if len(resp.ReleaseGroups) > 0 {
			return candidates(resp), nil
		}
	}
	return []meta.Candidate{}, nil
}

// Fetch reads one release group (KindAlbum) or artist (KindArtist) by id.
func (c *Client) Fetch(ctx context.Context, ref meta.Ref) (*meta.Record, error) {
	if !validID(ref.ExternalID) {
		return nil, fmt.Errorf("musicbrainz: %q is not an id", ref.ExternalID)
	}
	switch ref.Kind {
	case meta.KindAlbum:
		params := url.Values{"inc": {"artist-credits genres"}}
		var rg releaseGroup
		if err := c.get(ctx, "/release-group/"+ref.ExternalID, params, &rg); err != nil {
			return nil, err
		}
		return albumRecord(rg), nil
	case meta.KindArtist:
		params := url.Values{"inc": {"genres"}}
		var a artist
		if err := c.get(ctx, "/artist/"+ref.ExternalID, params, &a); err != nil {
			return nil, err
		}
		return artistRecord(a), nil
	}
	return nil, fmt.Errorf("musicbrainz: cannot fetch a %s", ref.Kind)
}

// validID is the shape of an MBID. It goes into a URL path, so anything else
// is refused rather than escaped.
func validID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
				return false
			}
		}
	}
	return true
}

/*
 * SearchQuery is the Lucene query for an album: its title as a phrase, and its
 * album artist as a phrase when there is one.
 *
 * Phrases, because a bare title is parsed as syntax: "AC/DC", "Help!" and
 * "(What's the Story) Morning Glory?" are all operators to Lucene. Inside a
 * phrase only the quote and the backslash need escaping.
 */
func SearchQuery(title, artist string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	q := `releasegroup:"` + escapePhrase(title) + `"`
	if a := strings.TrimSpace(artist); a != "" {
		q += ` AND artist:"` + escapePhrase(a) + `"`
	}
	return q
}

// wordQuery is the looser search: the title's words, and the artist's when
// given, each escaped so no character is read as an operator.
func wordQuery(title, artist string) string {
	t := escapeTerms(title)
	if t == "" {
		return ""
	}
	q := "releasegroup:(" + t + ")"
	if a := escapeTerms(artist); a != "" {
		q += " AND artist:(" + a + ")"
	}
	return q
}

// escapeTerms backslash-escapes every Lucene special character.
func escapeTerms(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if strings.ContainsRune(`+-&|!(){}[]^"~*?:\/`, r) {
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func escapePhrase(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

// get is a cached, rate-limited GET that retries a busy answer.
func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	params.Set("fmt", "json")
	key := path + "?" + params.Encode()
	if c.cache != nil {
		if payload, ok, err := c.cache.CachedResponse(ctx, ID, key, cacheTTL); err == nil && ok {
			return json.Unmarshal(payload, out)
		}
	}
	body, err := c.do(ctx, c.baseURL+key)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("musicbrainz: %s: %w", path, err)
	}
	if c.cache != nil {
		_ = c.cache.CacheResponse(ctx, ID, key, body)
	}
	return nil
}

func (c *Client) do(ctx context.Context, endpoint string) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("Accept", "application/json")
		resp, err := c.http.Do(req)
		busy := false
		if err != nil {
			// A network failure is not an answer about the album either.
			busy = true
		} else {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
			resp.Body.Close()
			switch {
			case resp.StatusCode == http.StatusOK:
				return body, readErr
			case resp.StatusCode == http.StatusNotFound:
				return nil, ErrNotFound
			case resp.StatusCode == http.StatusServiceUnavailable,
				resp.StatusCode == http.StatusTooManyRequests,
				resp.StatusCode >= 500:
				busy = true
			default:
				return nil, fmt.Errorf("musicbrainz: status %d", resp.StatusCode)
			}
		}
		if !busy || attempt >= c.MaxRetries {
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrBusy, err)
			}
			return nil, ErrBusy
		}
		wait := c.retryBase << attempt
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		case <-t.C:
		}
	}
}

// ----------------------------------------------------------------- decoding

type searchResponse struct {
	ReleaseGroups []releaseGroup `json:"release-groups"`
}

type releaseGroup struct {
	ID               string         `json:"id"`
	Title            string         `json:"title"`
	PrimaryType      string         `json:"primary-type"`
	SecondaryTypes   []string       `json:"secondary-types"`
	FirstReleaseDate string         `json:"first-release-date"`
	ArtistCredit     []artistCredit `json:"artist-credit"`
	Genres           []genre        `json:"genres"`
}

type artistCredit struct {
	Name       string `json:"name"`
	JoinPhrase string `json:"joinphrase"`
	Artist     struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
}

type artist struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Genres []genre `json:"genres"`
}

type genre struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func candidates(resp searchResponse) []meta.Candidate {
	out := make([]meta.Candidate, 0, len(resp.ReleaseGroups))
	for _, rg := range resp.ReleaseGroups {
		out = append(out, meta.Candidate{
			Provider:    ID,
			ExternalID:  rg.ID,
			Kind:        meta.KindAlbum,
			Title:       rg.Title,
			Year:        yearOf(rg.FirstReleaseDate),
			Artist:      creditString(rg.ArtistCredit),
			ArtistNames: creditNames(rg.ArtistCredit),
			ReleaseType: releaseType(rg.PrimaryType, rg.SecondaryTypes),
		})
	}
	return out
}

func albumRecord(rg releaseGroup) *meta.Record {
	rec := &meta.Record{
		Source:     ID,
		ExternalID: rg.ID,
		Kind:       meta.KindAlbum,
		Genres:     topGenres(rg.Genres),
		Music: &meta.MusicInfo{
			ReleaseType: releaseType(rg.PrimaryType, rg.SecondaryTypes),
			Artists:     creditArtists(rg.ArtistCredit),
		},
	}
	if rg.Title != "" {
		rec.Fields.Title = meta.S(rg.Title)
	}
	if y := yearOf(rg.FirstReleaseDate); y != 0 {
		rec.Fields.Year = meta.I(y)
	}
	return rec
}

func artistRecord(a artist) *meta.Record {
	rec := &meta.Record{
		Source:     ID,
		ExternalID: a.ID,
		Kind:       meta.KindArtist,
		Genres:     topGenres(a.Genres),
		Music:      &meta.MusicInfo{Artists: []meta.MusicArtist{{ID: a.ID, Name: a.Name}}},
	}
	if a.Name != "" {
		rec.Fields.Title = meta.S(a.Name)
	}
	return rec
}

// creditString is the credit as MusicBrainz prints it: each name and the
// phrase that joins it to the next ("Jay-Z & Linkin Park").
func creditString(cs []artistCredit) string {
	var b strings.Builder
	for _, c := range cs {
		name := c.Name
		if name == "" {
			name = c.Artist.Name
		}
		b.WriteString(name)
		b.WriteString(c.JoinPhrase)
	}
	return strings.TrimSpace(b.String())
}

// creditNames is every name the credit uses for its artists: as credited,
// and as MusicBrainz names the artist, when they differ.
func creditNames(cs []artistCredit) []string {
	var out []string
	for _, c := range cs {
		for _, n := range []string{c.Name, c.Artist.Name} {
			if n != "" && !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	return out
}

func creditArtists(cs []artistCredit) []meta.MusicArtist {
	out := make([]meta.MusicArtist, 0, len(cs))
	for _, c := range cs {
		if c.Artist.ID == "" {
			continue
		}
		out = append(out, meta.MusicArtist{ID: c.Artist.ID, Name: c.Artist.Name})
	}
	return out
}

/*
 * releaseType names what kind of record this is.
 *
 * The secondary type wins when there is one: a live album is better called
 * Live than Album, and a compilation of singles is a Compilation. MusicBrainz's
 * own words are kept (an open set: Remix, DJ-mix and others exist too); which
 * ones the browse filter groups is the client's business.
 */
func releaseType(primary string, secondary []string) string {
	for _, s := range secondary {
		if s != "" {
			return s
		}
	}
	return primary
}

// yearOf reads the year from "1998-06-29", "1998-06" or "1998".
func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, err := strconv.Atoi(date[:4])
	if err != nil || y < 1000 {
		return 0
	}
	return y
}

/*
 * topGenres keeps the genres people actually agree on.
 *
 * A genre on MusicBrainz is a count of users' tag votes, and a popular record
 * collects a long tail of single votes: Hello Nasty has eleven genres, five of
 * them with one vote ("art pop", "rap rock"). The kept ones need at least
 * three tenths of the strongest's votes, and two votes when the strongest has
 * two or more. A record whose best genre has one vote keeps its one-vote
 * genres: an obscure album has nothing better, and these only fill an album
 * that has no genre at all (ADR 0115).
 *
 * These are CC BY-NC-SA, not CC0 (users' tags), which is why they are only
 * ever shown with credit and never sent anywhere.
 */
func topGenres(gs []genre) []string {
	if len(gs) == 0 {
		return nil
	}
	sorted := append([]genre(nil), gs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Count != sorted[j].Count {
			return sorted[i].Count > sorted[j].Count
		}
		return sorted[i].Name < sorted[j].Name
	})
	top := sorted[0].Count
	floor := int(math.Ceil(0.3 * float64(top)))
	if top >= 2 && floor < 2 {
		floor = 2
	}
	var out []string
	for _, g := range sorted {
		if g.Count < floor || len(out) == maxGenres {
			break
		}
		if n := genreName(g.Name); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// genreName capitalises MusicBrainz's lower-case genres to read like the tags
// beside them: "east coast hip hop" becomes "East Coast Hip Hop", and
// "hip-hop" "Hip-Hop". The genre table joins names in any case, so this is
// how it is shown, not how it is matched.
func genreName(s string) string {
	s = strings.TrimSpace(s)
	b := []rune(s)
	up := true
	for i, r := range b {
		if up && r >= 'a' && r <= 'z' {
			b[i] = r - 'a' + 'A'
		}
		up = r == ' ' || r == '-' || r == '/'
	}
	return string(b)
}
