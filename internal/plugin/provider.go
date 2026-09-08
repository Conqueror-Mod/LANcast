package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"lancast/internal/meta"
)

// The exported functions a provider plugin implements. Two, mirroring the host
// interface, because searching and fetching are different questions with
// different answers — and the ABI has always been one packed (ptr,len) per
// call, so a second export needs nothing new.
const (
	searchEntrypoint = "search"
	fetchEntrypoint  = "fetch"
)

/*
 * The provider wire shapes.
 *
 * These live here, on the host side, as the authoritative form a guest SDK
 * mirrors — the same arrangement ratingsource.go uses.
 *
 * Two omissions are load-bearing rather than tidy:
 *
 *   - A candidate carries no score and no breakdown. Ranking is the host's
 *     (ADR 0063), and a field a plugin cannot spell is a stronger statement
 *     than one the host overwrites afterwards. A plugin able to score its own
 *     candidates could promote itself over TMDB, and match confidence is the
 *     one number the library's identity rests on.
 *   - A record carries no source. The host fills it with the manifest name, so
 *     a plugin cannot attribute its own answers to somebody else — which
 *     matters because that id is what merge precedence and item_lock key on.
 */
type searchRequest struct {
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Year    int    `json:"year,omitempty"`
	Series  string `json:"series,omitempty"`
	Season  int    `json:"season,omitempty"`
	Episode int    `json:"episode,omitempty"`
}

type wireCandidate struct {
	ExternalID string  `json:"external_id"`
	Kind       string  `json:"kind"`
	Title      string  `json:"title"`
	Year       int     `json:"year,omitempty"`
	Overview   string  `json:"overview,omitempty"`
	Popularity float64 `json:"popularity,omitempty"`
	PosterURL  string  `json:"poster_url,omitempty"`
}

type fetchRequest struct {
	Kind       string `json:"kind"`
	ExternalID string `json:"external_id"`
	Season     int    `json:"season,omitempty"`
	Episode    int    `json:"episode,omitempty"`
}

// wireFields keeps the pointers. "This source has nothing to say about this
// field" and "this source says empty" are different answers (ADR 0008), and
// flattening them at the boundary would make a plugin unable to express a
// distinction every native provider can.
type wireFields struct {
	Title         *string  `json:"title,omitempty"`
	SortTitle     *string  `json:"sort_title,omitempty"`
	Year          *int     `json:"year,omitempty"`
	Overview      *string  `json:"overview,omitempty"`
	Rating        *float64 `json:"rating,omitempty"`
	ContentRating *string  `json:"content_rating,omitempty"`
	ReleasedAt    *int64   `json:"released_at,omitempty"`
	DurationMS    *int64   `json:"duration_ms,omitempty"`
	Series        *string  `json:"series,omitempty"`
	Season        *int     `json:"season,omitempty"`
	Episode       *int     `json:"episode,omitempty"`
}

type wireArt struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

type wireCredit struct {
	Name      string `json:"name"`
	Role      string `json:"role"`
	Character string `json:"character,omitempty"`
	Order     int    `json:"order,omitempty"`
	Image     string `json:"image,omitempty"`
}

type wireCollection struct {
	ExternalID string    `json:"external_id"`
	Name       string    `json:"name"`
	Artwork    []wireArt `json:"artwork,omitempty"`
}

type wireKeyword struct {
	ID   int    `json:"id,omitempty"`
	Name string `json:"name"`
}

type wireRecord struct {
	ExternalID string          `json:"external_id"`
	Kind       string          `json:"kind"`
	IMDbID     string          `json:"imdb_id,omitempty"`
	Fields     wireFields      `json:"fields"`
	Genres     []string        `json:"genres,omitempty"`
	Credits    []wireCredit    `json:"credits,omitempty"`
	Artwork    []wireArt       `json:"artwork,omitempty"`
	Collection *wireCollection `json:"collection,omitempty"`
	Keywords   []wireKeyword   `json:"keywords,omitempty"`
}

// provider adapts a provider plugin to meta.Provider, so a loaded module
// registers into the same Registry as native TMDB with nothing downstream aware
// it is a plugin (ADR 0007).
type provider struct {
	p *Plugin
}

// NewProvider wraps a provider plugin as a meta.Provider, refusing a plugin of
// the wrong kind now rather than at call time.
func NewProvider(p *Plugin) (meta.Provider, error) {
	if p.Manifest.Kind != KindProvider {
		return nil, fmt.Errorf("plugin %q is kind %q, not %q", p.Manifest.Name, p.Manifest.Kind, KindProvider)
	}
	return &provider{p: p}, nil
}

// ID is the manifest name, which is what merge precedence, item_lock and the
// logs key on.
func (pr *provider) ID() string { return pr.p.Manifest.Name }

// Caps comes from the signed manifest, not from the module (ADR 0063).
func (pr *provider) Caps() meta.Caps {
	c := pr.p.Manifest.Caps
	return meta.Caps{Movie: c.Movie, Show: c.Show, Episode: c.Episode, Artwork: c.Artwork}
}

// Search asks the module for possible matches. It returns them unscored; Rank
// scores them.
func (pr *provider) Search(ctx context.Context, q meta.Query) ([]meta.Candidate, error) {
	in, err := json.Marshal(searchRequest{
		Kind: string(q.Kind), Title: q.Title, Year: q.Year,
		Series: q.Series, Season: q.Season, Episode: q.Episode,
	})
	if err != nil {
		return nil, err
	}
	payload, err := pr.call(ctx, searchEntrypoint, in)
	if err != nil || len(payload) == 0 {
		return nil, err
	}
	var wire []wireCandidate
	if err := json.Unmarshal(payload, &wire); err != nil {
		return nil, fmt.Errorf("plugin %q returned malformed candidates: %w", pr.ID(), err)
	}
	out := make([]meta.Candidate, 0, len(wire))
	for _, c := range wire {
		if c.ExternalID == "" {
			// A candidate the host cannot Fetch is not a candidate. Dropping it
			// here beats carrying it into ranking and failing later with the
			// title as the only clue.
			continue
		}
		out = append(out, meta.Candidate{
			Provider:   pr.ID(),
			ExternalID: c.ExternalID,
			Kind:       meta.Kind(c.Kind),
			Title:      c.Title,
			Year:       c.Year,
			Overview:   c.Overview,
			Popularity: c.Popularity,
			PosterURL:  pr.keepURL(c.PosterURL),
		})
	}
	return out, nil
}

// Fetch asks the module for one record in full.
func (pr *provider) Fetch(ctx context.Context, ref meta.Ref) (*meta.Record, error) {
	if ref.ExternalID == "" {
		return nil, nil
	}
	in, err := json.Marshal(fetchRequest{
		Kind: string(ref.Kind), ExternalID: ref.ExternalID,
		Season: ref.Season, Episode: ref.Episode,
	})
	if err != nil {
		return nil, err
	}
	payload, err := pr.call(ctx, fetchEntrypoint, in)
	if err != nil || len(payload) == 0 {
		// Nothing, and no error, is "there is no such record" — a real answer,
		// and now a different one from "I could not ask".
		return nil, err
	}
	var w wireRecord
	if err := json.Unmarshal(payload, &w); err != nil {
		return nil, fmt.Errorf("plugin %q returned a malformed record: %w", pr.ID(), err)
	}
	rec := &meta.Record{
		Source:     pr.ID(),
		ExternalID: w.ExternalID,
		Kind:       meta.Kind(w.Kind),
		IMDbID:     w.IMDbID,
		Fields: meta.Fields{
			Title: w.Fields.Title, SortTitle: w.Fields.SortTitle, Year: w.Fields.Year,
			Overview: w.Fields.Overview, Rating: w.Fields.Rating,
			ContentRating: w.Fields.ContentRating, ReleasedAt: w.Fields.ReleasedAt,
			DurationMS: w.Fields.DurationMS, Series: w.Fields.Series,
			Season: w.Fields.Season, Episode: w.Fields.Episode,
		},
		Genres: w.Genres,
	}
	if rec.ExternalID == "" {
		rec.ExternalID = ref.ExternalID
	}
	for _, c := range w.Credits {
		if c.Name == "" {
			continue
		}
		rec.Credits = append(rec.Credits, meta.Credit{
			Name: c.Name, Role: c.Role, Character: c.Character,
			Order: c.Order, Image: pr.keepURL(c.Image),
		})
	}
	rec.Artwork = pr.artwork(w.Artwork)
	if w.Collection != nil && w.Collection.ExternalID != "" {
		rec.Collection = &meta.CollectionRef{
			ExternalID: w.Collection.ExternalID,
			Name:       w.Collection.Name,
			Artwork:    pr.artwork(w.Collection.Artwork),
		}
	}
	for _, k := range w.Keywords {
		if k.Name == "" {
			continue
		}
		rec.Keywords = append(rec.Keywords, meta.Keyword{ID: k.ID, Name: k.Name})
	}
	return rec, nil
}

// call invokes an entrypoint and unwraps the ABI 2 envelope.
func (pr *provider) call(ctx context.Context, fn string, in []byte) (json.RawMessage, error) {
	out, err := pr.p.Call(ctx, fn, in)
	if err != nil {
		return nil, fmt.Errorf("plugin %q %s: %w", pr.ID(), fn, err)
	}
	// A guest-reported failure travels. "No candidates" means this item is
	// unmatched and the enricher stops asking; "the API is down" means ask
	// again later, and collapsing the two writes a permanent conclusion from a
	// temporary failure (ADR 0063).
	return decodeEnvelope(pr.ID(), out)
}

func (pr *provider) artwork(in []wireArt) []meta.ArtRef {
	var out []meta.ArtRef
	for _, a := range in {
		url := pr.keepURL(a.URL)
		if url == "" {
			continue
		}
		out = append(out, meta.ArtRef{Kind: meta.ArtKind(a.Kind), URL: url})
	}
	return out
}

/*
 * keepURL drops an image URL the plugin was never granted (ADR 0063).
 *
 * An artwork URL is the one field where a plugin makes the *host* fetch
 * something. The capability model was built to stop a plugin reaching the
 * network on its own, and it does — this is the host reaching the network on
 * the plugin's behalf, through a field nobody had thought of as a capability.
 *
 * internal/netguard already refuses private and local addresses, which closes
 * the dangerous half: the server itself, the LAN, the cloud metadata endpoint.
 * What it cannot do is stop a plugin pointing the host at an arbitrary *public*
 * address — an unattributed fetch on a schedule the plugin influences, which is
 * a serviceable beacon and a way to make the server talk to a host nobody
 * granted. So the URL is checked against this plugin's own grant, with the same
 * matching host_http_get applies.
 *
 * The grant is the plugin's, not a global list: two plugins may legitimately
 * use different image hosts, and a shared allowlist would be the union of
 * everything anybody was ever granted. And it happens before the fetch, not by
 * filtering afterwards, because a request has already done its damage once it
 * is answered.
 *
 * First-party providers are unaffected. They are not plugins and their URLs come
 * from our own code; putting them behind a list would mean maintaining an
 * allowlist for ourselves, against ourselves.
 */
func (pr *provider) keepURL(raw string) string {
	if raw == "" {
		return ""
	}
	if pr.p.Manifest.allowsURL(raw) {
		return raw
	}
	pr.p.rt.log.Warn("dropped a plugin image URL outside its granted hosts",
		"plugin", pr.ID(), "url", raw)
	return ""
}
