package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"lancast/internal/meta"
)

// ratingsEntrypoint is the exported function a rating_source plugin implements.
const ratingsEntrypoint = "ratings"

// ratingsRequest and ratingItem are the ABI payloads for the ratings call —
// JSON in, JSON out. Kept here, on the host side of the boundary, as the
// authoritative shape a guest SDK mirrors.
type ratingsRequest struct {
	IMDbID string `json:"imdb_id"`
}

type ratingItem struct {
	Source  string  `json:"source"`
	Score   float64 `json:"score"`
	Display string  `json:"display"`
	Votes   int     `json:"votes"`
}

// ratingSource adapts a rating_source plugin to meta.RatingSource, so a loaded
// module registers into the same Registry as the native OMDb/TMDB sources with
// nothing downstream aware it is a plugin (ADR 0007).
type ratingSource struct {
	p *Plugin
}

// NewRatingSource wraps a rating_source plugin as a meta.RatingSource. It
// returns an error for a plugin of the wrong kind rather than failing later at
// call time.
func NewRatingSource(p *Plugin) (meta.RatingSource, error) {
	if p.Manifest.Kind != KindRatingSource {
		return nil, fmt.Errorf("plugin %q is kind %q, not %q", p.Manifest.Name, p.Manifest.Kind, KindRatingSource)
	}
	return &ratingSource{p: p}, nil
}

// ID is the plugin's manifest name, so its scores are attributed to it in
// item_rating and the logs.
func (rs *ratingSource) ID() string { return rs.p.Manifest.Name }

// Ratings marshals the imdb id across the boundary, invokes the module, and
// unmarshals the scores back. An empty imdb id short-circuits — nothing to look
// up — matching the native sources' behaviour.
func (rs *ratingSource) Ratings(ctx context.Context, imdbID string) ([]meta.Rating, error) {
	if imdbID == "" {
		return nil, nil
	}
	in, err := json.Marshal(ratingsRequest{IMDbID: imdbID})
	if err != nil {
		return nil, err
	}
	out, err := rs.p.Call(ctx, ratingsEntrypoint, in)
	if err != nil {
		return nil, fmt.Errorf("plugin %q ratings: %w", rs.p.Manifest.Name, err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	var items []ratingItem
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("plugin %q returned malformed ratings: %w", rs.p.Manifest.Name, err)
	}
	ratings := make([]meta.Rating, 0, len(items))
	for _, it := range items {
		if it.Source == "" {
			continue
		}
		ratings = append(ratings, meta.Rating{
			Source: it.Source, Score: it.Score, Display: it.Display, Votes: it.Votes,
		})
	}
	return ratings, nil
}
