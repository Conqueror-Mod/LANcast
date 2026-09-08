// Command omdb is the first-party OMDb rating_source plugin (ADR 0019 / 0020).
//
// It exists to validate the plugin boundary: the parse/normalize logic mirrors
// internal/meta/omdb on the host, and internal/plugin's equivalence test asserts
// the two produce byte-identical ratings. If they drift, that test fails — which
// is the point of shipping a first-party plugin before the contract goes public.
//
// It reads its key via the host secret capability and fetches through the host,
// so it holds no ambient authority: no key in the binary, no socket of its own.
//
// Build: GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
package main

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"lancastplugins/sdk"
)

func main() {}

//go:wasmexport alloc
func alloc(size uint32) uint32 { return sdk.Alloc(size) }

//go:wasmexport ratings
func ratings(ptr, length uint32) uint64 {
	return sdk.HandleRatings(sdk.Input(ptr, length), omdbRatings)
}

// Source ids for the individual scores OMDb aggregates. Must match the host's
// omdb package so item_rating rows agree.
const (
	sourceIMDb           = "imdb"
	sourceRottenTomatoes = "rotten_tomatoes"
	sourceMetacritic     = "metacritic"
)

type response struct {
	Response  string `json:"Response"`
	Error     string `json:"Error"`
	IMDbVotes string `json:"imdbVotes"`
	Ratings   []struct {
		Source string `json:"Source"`
		Value  string `json:"Value"`
	} `json:"Ratings"`
}

/*
 * The error returns mirror internal/meta/omdb exactly, because the equivalence
 * test is what keeps the two honest and the boundary must not quietly change
 * the meaning of an answer.
 *
 * The native client already told a fetch failure apart from a miss: a transport
 * error is returned, while `"Response":"False"` — "Movie not found", "Incorrect
 * IMDb ID" — is a normal empty result, because OMDb's coverage thins out for
 * older, non-US and television titles. **ABI 1 could not carry that
 * distinction.** Both arrived at the host as an empty span, so a plugin whose
 * key had been rejected reported itself exactly as a film with no scores.
 */
func omdbRatings(imdbID string) ([]sdk.Rating, error) {
	imdbID = normalizeIMDbID(imdbID)
	if imdbID == "" {
		return nil, nil
	}
	// Not configured is not a failure, matching the native client's
	// Configured() check: a server with no OMDb key is a working server.
	key := sdk.Secret("omdb_key")
	if key == "" {
		return nil, nil
	}
	body := sdk.HTTPGet("https://www.omdbapi.com/?i=" + imdbID + "&apikey=" + key)
	if len(body) == 0 {
		// The host refused the fetch, or it failed. Either way this is the
		// case ABI 1 could not report.
		return nil, errors.New("omdb request failed or was denied")
	}
	var doc response
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, errors.New("omdb returned a response this plugin could not read")
	}
	if !strings.EqualFold(doc.Response, "True") {
		// "Incorrect IMDb ID" / "Movie not found" — a miss, not a failure,
		// exactly as the native client treats it.
		return nil, nil
	}
	var out []sdk.Rating
	for _, r := range doc.Ratings {
		source, score, ok := parseRating(r.Source, r.Value)
		if !ok {
			continue
		}
		rating := sdk.Rating{Source: source, Score: score, Display: displayFor(r.Value)}
		if source == sourceIMDb {
			rating.Votes = parseVotes(doc.IMDbVotes)
		}
		out = append(out, rating)
	}
	return out, nil
}

func parseRating(source, value string) (string, float64, bool) {
	n, ok := leadingNumber(value)
	if !ok {
		return "", 0, false
	}
	switch source {
	case "Internet Movie Database":
		return sourceIMDb, n, true
	case "Rotten Tomatoes":
		return sourceRottenTomatoes, n / 10, true
	case "Metacritic":
		return sourceMetacritic, n / 10, true
	}
	return "", 0, false
}

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

func displayFor(value string) string {
	value = strings.TrimSpace(value)
	if i := strings.IndexByte(value, '/'); i >= 0 {
		return value[:i]
	}
	return value
}

func parseVotes(s string) int {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

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
