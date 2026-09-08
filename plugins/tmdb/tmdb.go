// Command tmdb is the first-party TMDB provider plugin (ADR 0007 / 0020 / 0063).
//
// It is the second first-party plugin, and it exists for the same reason the
// OMDb one did: to prove the boundary carries a real extension point rather
// than a convenient one. OMDb was the narrowest thing a plugin can be — one id
// in, some scores out, no identity, no images, no failure worth naming. A
// provider is the opposite end: it decides what a file *is*, it returns images
// the host will fetch, and getting "nothing" and "I could not ask" the wrong
// way round writes a permanent answer from a temporary outage.
//
// The parse and mapping logic mirrors internal/meta/tmdb on the host, and
// internal/plugin's equivalence test asserts the two produce identical records
// and candidates from identical payloads. If they drift, that test fails.
//
// It reads its key via the host secret capability and fetches through the host,
// so it holds no ambient authority: no key in the binary, no socket of its own.
//
// # What this plugin deliberately does not do
//
// No caching, no rate limiting, no retries. The native client has all three,
// and a plugin cannot: they belong to whoever holds the socket, which is the
// host. Putting them in a guest would also mean trusting a guest to be polite
// to somebody else's API on the server's IP address, which is exactly the sort
// of thing the capability model exists to not do. See the PR notes — the right
// home for them is host_http_get itself.
//
// Build: GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
package main

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"lancastplugins/sdk"
)

func main() {}

//go:wasmexport alloc
func alloc(size uint32) uint32 { return sdk.Alloc(size) }

//go:wasmexport search
func search(ptr, length uint32) uint64 {
	return sdk.HandleSearch(sdk.Input(ptr, length), tmdbSearch)
}

//go:wasmexport fetch
func fetch(ptr, length uint32) uint64 {
	return sdk.HandleFetch(sdk.Input(ptr, length), tmdbFetch)
}

const (
	baseURL      = "https://api.themoviedb.org/3"
	imageBaseURL = "https://image.tmdb.org/t/p/original"

	// profileBaseURL is w300 and not w185 for a reason worth keeping: the host's
	// artwork cache generates a 185px-wide thumb whatever it is handed, so a
	// 185px source means deriving 185 from 185 — no resampling and no headroom.
	// Larger than 300 is bytes nobody sees, since the served variant is capped.
	profileBaseURL = "https://image.tmdb.org/t/p/w300"

	/*
	 * The language TMDB is asked for.
	 *
	 * Stated rather than assumed. TMDB's default happens to be en-US and things
	 * mostly looked right without it, which is precisely what made it worth
	 * setting — a behaviour nobody asked for and nobody can see is one that
	 * changes underneath you.
	 */
	language = "en-US"
)

// errUnavailable is what a failed or denied host fetch becomes. It is an error
// and not an empty result on purpose: "no such title" is a conclusion the
// enricher records and stops re-asking, and an outage must never write it.
var errUnavailable = errors.New("tmdb request failed or was denied")

// get fetches a TMDB path through the host and decodes it.
func get(path string, params url.Values, out any) error {
	key := sdk.Secret("tmdb_key")
	if key == "" {
		return errNotConfigured
	}
	if params == nil {
		params = url.Values{}
	}
	params.Set("api_key", key)
	// Set here rather than at each call site so a request cannot be added later
	// that forgets it.
	params.Set("language", language)

	body := sdk.HTTPGet(baseURL + path + "?" + params.Encode())
	if len(body) == 0 {
		return errUnavailable
	}
	if err := json.Unmarshal(body, out); err != nil {
		return errors.New("tmdb returned a response this plugin could not read")
	}
	return nil
}

// errNotConfigured mirrors the native client: a server with no TMDB key is a
// working server, and the enricher treats this as "no metadata available"
// rather than something to show somebody.
var errNotConfigured = errors.New("tmdb: no API key configured")

func tmdbSearch(q sdk.Query) ([]sdk.Candidate, error) {
	/*
	 * A season is never searchable by name.
	 *
	 * Its name is a position within a show — "Season 2" — not the name of a
	 * work, so /search/tv returns whatever real show happens to have that
	 * phrase in its title. Those score as exact title matches and land above
	 * the auto-apply threshold, which is how a Thai drama named "... season 2"
	 * became the poster for season 2 of nine unrelated shows.
	 */
	if q.Kind == "season" {
		return nil, nil
	}

	title := q.Title
	path := "/search/movie"
	kind := "movie"
	if q.Kind == "show" || q.Kind == "episode" {
		path = "/search/tv"
		kind = "show"
		if q.Series != "" {
			title = q.Series
		}
	}
	if strings.TrimSpace(title) == "" {
		return nil, nil
	}

	/*
	 * The year is deliberately NOT sent.
	 *
	 * TMDB's `year` parameter is a hard filter, so a filename off by one — and
	 * they frequently are — returns zero results rather than a slightly weaker
	 * match. That defeats confidence scoring entirely: the provider would be
	 * rejecting imperfect data before the host code built to handle imperfect
	 * data ever runs. The host's ranking already weights year proximity, and it
	 * can tell "close" from "wrong" in a way a filter cannot.
	 */
	var raw searchResponse
	if err := get(path, url.Values{"query": {title}}, &raw); err != nil {
		return nil, err
	}

	out := make([]sdk.Candidate, 0, len(raw.Results))
	for _, r := range raw.Results {
		out = append(out, sdk.Candidate{
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

func tmdbFetch(ref sdk.Ref) (*sdk.Record, error) {
	switch ref.Kind {
	case "movie":
		return fetchMovie(ref.ExternalID)
	case "show":
		return fetchShow(ref.ExternalID)
	case "season":
		return fetchSeason(ref)
	case "episode":
		return fetchEpisode(ref)
	}
	return nil, errors.New("tmdb: unsupported kind " + ref.Kind)
}

func fetchMovie(id string) (*sdk.Record, error) {
	var m movieDetail
	// Keywords ride along on the same request rather than costing a second one.
	// They are what expresses an umbrella grouping like the MCU, which
	// belongs_to_collection structurally cannot.
	if err := get("/movie/"+id, url.Values{"append_to_response": {"credits,keywords"}}, &m); err != nil {
		return nil, err
	}

	rec := &sdk.Record{ExternalID: id, Kind: "movie", IMDbID: m.IMDbID}
	rec.Fields.Title = sdk.Str(m.Title)
	if m.Overview != "" {
		rec.Fields.Overview = sdk.Str(m.Overview)
	}
	if y := yearOf(m.ReleaseDate); y > 0 {
		rec.Fields.Year = sdk.Int(y)
		if ts, ok := parseDate(m.ReleaseDate); ok {
			rec.Fields.ReleasedAt = sdk.Int64(ts)
		}
	}
	if m.VoteAverage > 0 {
		rec.Fields.Rating = sdk.Num(m.VoteAverage)
	}
	if m.Runtime > 0 {
		rec.Fields.DurationMS = sdk.Int64(int64(m.Runtime) * 60_000)
	}
	rec.Genres = genreNames(m.Genres)
	rec.Credits = convertCredits(m.Credits)
	rec.Artwork = artRefs(m.PosterPath, m.BackdropPath)
	for _, k := range m.Keywords.Keywords {
		rec.Keywords = append(rec.Keywords, sdk.Keyword{ID: k.ID, Name: k.Name})
	}
	if m.Collection != nil && m.Collection.ID != 0 {
		rec.Collection = &sdk.Collection{
			ExternalID: strconv.Itoa(m.Collection.ID),
			Name:       collectionName(m.Collection),
			Artwork:    artRefs(m.Collection.PosterPath, m.Collection.BackdropPath),
		}
	}
	return rec, nil
}

/*
 * collectionName resolves a collection's name, in English where TMDB has one.
 *
 * `belongs_to_collection`, embedded in the movie response, does not reliably
 * honour the language parameter — it carries whatever name the collection was
 * stored under. Reported as a Hulk collection displaying as "Hulk Koleksiyonu",
 * which is Turkish, on a server whose every other field came back in English.
 * The dedicated /collection/{id} endpoint is translated, so it is asked.
 *
 * Best-effort by construction: any failure, or an empty name, falls back to the
 * embedded one. A franchise grouping is worth a wrong-language name and is not
 * worth failing an enrichment over — the film is already matched by this point.
 */
func collectionName(embedded *tmdbCollection) string {
	var full tmdbCollection
	err := get("/collection/"+strconv.Itoa(embedded.ID), nil, &full)
	if err != nil || strings.TrimSpace(full.Name) == "" {
		return embedded.Name
	}
	return full.Name
}

func fetchShow(id string) (*sdk.Record, error) {
	var s showDetail
	if err := get("/tv/"+id, url.Values{"append_to_response": {"credits,external_ids"}}, &s); err != nil {
		return nil, err
	}

	rec := &sdk.Record{ExternalID: id, Kind: "show", IMDbID: s.ExternalIDs.IMDbID}
	rec.Fields.Title = sdk.Str(s.Name)
	rec.Fields.Series = sdk.Str(s.Name)
	if s.Overview != "" {
		rec.Fields.Overview = sdk.Str(s.Overview)
	}
	if y := yearOf(s.FirstAirDate); y > 0 {
		rec.Fields.Year = sdk.Int(y)
		if ts, ok := parseDate(s.FirstAirDate); ok {
			rec.Fields.ReleasedAt = sdk.Int64(ts)
		}
	}
	if s.VoteAverage > 0 {
		rec.Fields.Rating = sdk.Num(s.VoteAverage)
	}
	rec.Genres = genreNames(s.Genres)
	rec.Credits = convertCredits(s.Credits)
	rec.Artwork = artRefs(s.PosterPath, s.BackdropPath)
	return rec, nil
}

// fetchSeason retrieves one season of a known show. ref.ExternalID is the
// *show's* id — a season has no id of its own in this system, only a position —
// so the record is the season's own name, overview and poster, never the
// show's. Applying the show's poster to every season is how a season stops
// being distinguishable from its parent in a grid.
func fetchSeason(ref sdk.Ref) (*sdk.Record, error) {
	var s seasonDetail
	path := "/tv/" + ref.ExternalID + "/season/" + strconv.Itoa(ref.Season)
	if err := get(path, nil, &s); err != nil {
		return nil, err
	}

	rec := &sdk.Record{ExternalID: ref.ExternalID, Kind: "season"}
	if s.Name != "" {
		rec.Fields.Title = sdk.Str(s.Name)
	}
	rec.Fields.Season = sdk.Int(ref.Season)
	if s.Overview != "" {
		rec.Fields.Overview = sdk.Str(s.Overview)
	}
	if y := yearOf(s.AirDate); y > 0 {
		rec.Fields.Year = sdk.Int(y)
		if ts, ok := parseDate(s.AirDate); ok {
			rec.Fields.ReleasedAt = sdk.Int64(ts)
		}
	}
	if s.VoteAverage > 0 {
		rec.Fields.Rating = sdk.Num(s.VoteAverage)
	}
	// Seasons have a poster and no backdrop of their own; artRefs skips empties.
	rec.Artwork = artRefs(s.PosterPath, "")
	return rec, nil
}

func fetchEpisode(ref sdk.Ref) (*sdk.Record, error) {
	var e episodeDetail
	path := "/tv/" + ref.ExternalID + "/season/" + strconv.Itoa(ref.Season) +
		"/episode/" + strconv.Itoa(ref.Episode)
	if err := get(path, nil, &e); err != nil {
		return nil, err
	}

	rec := &sdk.Record{ExternalID: ref.ExternalID, Kind: "episode"}
	rec.Fields.Title = sdk.Str(e.Name)
	rec.Fields.Season = sdk.Int(ref.Season)
	rec.Fields.Episode = sdk.Int(ref.Episode)
	if e.Overview != "" {
		rec.Fields.Overview = sdk.Str(e.Overview)
	}
	if y := yearOf(e.AirDate); y > 0 {
		rec.Fields.Year = sdk.Int(y)
		if ts, ok := parseDate(e.AirDate); ok {
			rec.Fields.ReleasedAt = sdk.Int64(ts)
		}
	}
	if e.VoteAverage > 0 {
		rec.Fields.Rating = sdk.Num(e.VoteAverage)
	}
	if e.StillPath != "" {
		rec.Artwork = []sdk.Art{{Kind: "thumb", URL: imageURL(e.StillPath)}}
	}
	return rec, nil
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
		// The headshot, on the credits payload the names already come from — so
		// putting faces on a detail page costs no extra provider call.
		ProfilePath string `json:"profile_path"`
		Order       int    `json:"order"`
	} `json:"cast"`
	Crew []struct {
		Name string `json:"name"`
		Job  string `json:"job"`
	} `json:"crew"`
}

type movieDetail struct {
	Title        string          `json:"title"`
	Overview     string          `json:"overview"`
	ReleaseDate  string          `json:"release_date"`
	Runtime      int             `json:"runtime"`
	VoteAverage  float64         `json:"vote_average"`
	Genres       []tmdbGenre     `json:"genres"`
	PosterPath   string          `json:"poster_path"`
	BackdropPath string          `json:"backdrop_path"`
	Credits      creditsBlock    `json:"credits"`
	Collection   *tmdbCollection `json:"belongs_to_collection"`
	IMDbID       string          `json:"imdb_id"`
	/*
	 * Keywords, which are how the umbrella groupings are expressed.
	 *
	 * belongs_to_collection gives exactly one franchise per film and it is
	 * always the narrow one: Avengers: Endgame belongs to "The Avengers
	 * Collection", not to the Marvel Cinematic Universe. The MCU is a keyword —
	 * 180547, on 81 films — and there is no other field that carries it.
	 */
	Keywords keywordsBlock `json:"keywords"`
}

type keywordsBlock struct {
	Keywords []tmdbKeyword `json:"keywords"`
}

type tmdbKeyword struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type tmdbCollection struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	PosterPath   string `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
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
	// TV imdb ids live under external_ids, not on the detail root as they do
	// for movies, so the fetch appends that block.
	ExternalIDs externalIDs `json:"external_ids"`
}

type externalIDs struct {
	IMDbID string `json:"imdb_id"`
}

type seasonDetail struct {
	Name        string  `json:"name"`
	Overview    string  `json:"overview"`
	AirDate     string  `json:"air_date"`
	VoteAverage float64 `json:"vote_average"`
	PosterPath  string  `json:"poster_path"`
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
func convertCredits(c creditsBlock) []sdk.Credit {
	var out []sdk.Credit
	for i, m := range c.Cast {
		if i >= 20 {
			break
		}
		out = append(out, sdk.Credit{
			Name: m.Name, Role: "actor", Character: characterOf(m.Character), Order: m.Order,
			Image: profileURL(m.ProfilePath),
		})
	}
	for _, m := range c.Crew {
		switch m.Job {
		case "Director":
			out = append(out, sdk.Credit{Name: m.Name, Role: "director"})
		case "Writer", "Screenplay":
			out = append(out, sdk.Credit{Name: m.Name, Role: "writer"})
		}
	}
	return out
}

/*
 * characterOf drops a character name that is only a number.
 *
 * Observed on a real library: War Machine (2026) came back with characters
 * "81", "7", "15", "60", "109", "96" and "122" beside four properly named ones,
 * and the cast row rendered those digits under the actors' faces. It looks
 * exactly like LANcast leaking an internal id, which is what makes it worth
 * fixing even though the data is TMDB's — a user cannot tell a provider's bad
 * row from our bug, and will report ours.
 *
 * Digits only: "Agent 47", "Apollo 13" and "7" mean different things, and only
 * the last is certainly not a character.
 */
func characterOf(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return ""
	}
	for _, r := range t {
		if r < '0' || r > '9' {
			return t
		}
	}
	return ""
}

func artRefs(poster, backdrop string) []sdk.Art {
	var out []sdk.Art
	if poster != "" {
		out = append(out, sdk.Art{Kind: "poster", URL: imageURL(poster)})
	}
	if backdrop != "" {
		out = append(out, sdk.Art{Kind: "fanart", URL: imageURL(backdrop)})
	}
	return out
}

func imageURL(path string) string {
	if path == "" {
		return ""
	}
	return imageBaseURL + path
}

func profileURL(path string) string {
	if path == "" {
		return ""
	}
	return profileBaseURL + path
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
