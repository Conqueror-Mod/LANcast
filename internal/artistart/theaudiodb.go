// Package artistart fetches artist photographs from TheAudioDB (ADR 0025).
//
// Artists are the one container in this system with nothing local to source an
// image from. An album has a picture in its files; an artist has neither a file
// nor a directory of its own, and the images that sit in an artist folder turn
// out to be a media player's per-album art cache rather than a photograph of
// anyone — reading those puts an album sleeve on the artist tile *while looking
// like it found something better*, which is the worse of the two failures.
//
// So artists borrow their most-substantial album's cover today, flagged
// `inherited`. That placeholder is good, which is why this is unhurried: it
// supersedes itself with nothing to clean up.
//
// ADR 0025 chose TheAudioDB over the alternatives, checked rather than assumed:
// Last.fm returns a placeholder star at every size, and fanart.tv keys on
// MusicBrainz ids, making it two integrations rather than one.
package artistart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNoKey is returned when no API key is configured. TheAudioDB has no
// anonymous tier worth relying on, and a provider that silently does nothing is
// worse than one that says why.
var ErrNoKey = errors.New("artistart: no TheAudioDB key configured")

// ErrNotFound means the artist is not in the catalogue. Ordinary rather than
// exceptional: a local rip of somebody's band is not in anybody's database, and
// the borrowed album cover remains the answer.
var ErrNotFound = errors.New("artistart: artist not found")

// Client fetches artist images.
type Client struct {
	key  string
	http *http.Client
	base string
}

func New(key string) *Client {
	return &Client{
		key: strings.TrimSpace(key),
		// A short timeout on purpose: this is a cosmetic upgrade to a tile that
		// already has a picture, so a slow provider must never hold up
		// enrichment behind it.
		http: &http.Client{Timeout: 15 * time.Second},
		base: "https://theaudiodb.com/api/v1/json",
	}
}

func (c *Client) Available() bool { return c.key != "" }

/*
 * Image is what was found, and where it came from.
 *
 * Several sizes are returned because an artist tile and an artist page want
 * different things, and the thumbnail is the only one guaranteed to exist —
 * `strArtistFanart` is frequently null for anyone outside the top few thousand.
 */
type Image struct {
	ThumbURL  string
	FanartURL string
	// Name as the provider spells it. Kept for the log rather than to overwrite
	// anything: the library's own tags are authoritative for music (ADR 0024),
	// and a provider that renames "Beatles" to "The Beatles" would be editing a
	// field somebody's files already answered.
	ProviderName string
}

/*
 * Lookup finds an artist by name.
 *
 * Name is all there is to search on. That is the weakness of this integration
 * and it is worth stating: two bands called Nirvana exist, and this cannot tell
 * them apart. The mitigation is the fallback — a wrong photograph is worse than
 * a borrowed album cover, so anything below confident is refused rather than
 * guessed, and refusing leaves the placeholder in place.
 */
func (c *Client) Lookup(ctx context.Context, artist string) (*Image, error) {
	if !c.Available() {
		return nil, ErrNoKey
	}
	name := strings.TrimSpace(artist)
	if name == "" {
		return nil, ErrNotFound
	}

	endpoint := fmt.Sprintf("%s/%s/search.php?s=%s",
		c.base, url.PathEscape(c.key), url.QueryEscape(name))

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	// A contactable identifier, the way MusicBrainz requires and TheAudioDB
	// appreciates. A scraper with no name is the one that gets blocked.
	req.Header.Set("User-Agent", "LANcast/1.0 (self-hosted media server)")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("artistart: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("artistart: provider answered %s", resp.Status)
	}

	return parseSearch(io.LimitReader(resp.Body, 1<<20), name)
}

/*
 * parseSearch reads the search response.
 *
 * Split out from the request so every decision below — which record to take,
 * what counts as a match, what counts as an image — is testable against a
 * fixture with no network. The same split the probe package makes, and for the
 * same reason: these rules are where the bugs live, and a rule that needs a
 * live provider to exercise is a rule nobody exercises.
 */
func parseSearch(r io.Reader, want string) (*Image, error) {
	var body struct {
		Artists []struct {
			StrArtist       string `json:"strArtist"`
			StrArtistThumb  string `json:"strArtistThumb"`
			StrArtistFanart string `json:"strArtistFanart"`
		} `json:"artists"`
	}
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return nil, fmt.Errorf("artistart: unreadable response: %w", err)
	}
	// TheAudioDB answers a miss with `{"artists": null}` rather than an empty
	// list or a 404, which decodes cleanly into nothing.
	if len(body.Artists) == 0 {
		return nil, ErrNotFound
	}

	for _, a := range body.Artists {
		if !nameMatches(a.StrArtist, want) {
			continue
		}
		if a.StrArtistThumb == "" && a.StrArtistFanart == "" {
			// A record with no picture is not a find. Returning it would
			// replace a working placeholder with nothing.
			continue
		}
		return &Image{
			ThumbURL:     a.StrArtistThumb,
			FanartURL:    a.StrArtistFanart,
			ProviderName: a.StrArtist,
		}, nil
	}
	return nil, ErrNotFound
}

/*
 * nameMatches decides whether a returned record is the artist that was asked
 * for.
 *
 * A search endpoint returns near misses, and accepting the first row is how
 * "Sun" gets a photograph of "Sunn O)))". The comparison is deliberately strict
 * — case and a leading article are the only differences forgiven — because the
 * cost of a wrong match is a wrong face on a tile, and the cost of refusing is
 * the placeholder that was already there.
 */
func nameMatches(got, want string) bool {
	return normalise(got) == normalise(want)
}

func normalise(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// "The Beatles" and "Beatles" are the same band, and which one a library
	// carries depends on whoever tagged it.
	s = strings.TrimPrefix(s, "the ")
	return strings.Join(strings.Fields(s), " ")
}
