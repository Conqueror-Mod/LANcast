// Package meta defines the metadata provider contract and the merge engine.
//
// This is the project's first real extension point. At M2 the registry is
// populated in-process; at M4 the plugin runtime registers into the same
// registry and nothing downstream changes. See ADR 0007.
package meta

import (
	"context"
	"sort"
)

// Kind is the media taxonomy the core understands. It is deliberately small —
// library types become plugin-defined later (ADR 0002).
type Kind string

const (
	KindMovie   Kind = "movie"
	KindShow    Kind = "show"
	KindSeason  Kind = "season"
	KindEpisode Kind = "episode"
)

// Caps declares what a provider can answer for.
type Caps struct {
	Movie   bool
	Show    bool
	Episode bool
	Artwork bool
}

// Supports reports whether the provider handles this kind.
func (c Caps) Supports(k Kind) bool {
	switch k {
	case KindMovie:
		return c.Movie
	case KindShow, KindSeason:
		return c.Show
	case KindEpisode:
		return c.Episode
	}
	return false
}

// Query is a search request built from what the scanner guessed.
type Query struct {
	Kind    Kind
	Title   string
	Year    int // 0 when unknown
	Series  string
	Season  int
	Episode int
}

// Ref identifies one record within a provider.
type Ref struct {
	Kind       Kind
	ExternalID string
	Season     int
	Episode    int
}

// Candidate is one possible match, before scoring.
type Candidate struct {
	Provider   string
	ExternalID string
	Kind       Kind
	Title      string
	Year       int
	Overview   string
	Popularity float64
	PosterURL  string

	// Score and Breakdown are filled in by Rank, not by the provider.
	Score     float64
	Breakdown Breakdown
}

// ArtKind names an image role.
type ArtKind string

const (
	ArtPoster ArtKind = "poster"
	ArtFanart ArtKind = "fanart"
	ArtThumb  ArtKind = "thumb"
)

// ArtRef is an image a source knows about but has not downloaded.
type ArtRef struct {
	Kind ArtKind
	URL  string
}

// Credit is one person's involvement.
type Credit struct {
	Name      string
	Role      string // actor | director | writer
	Character string
	Order     int
}

// Fields are the per-field metadata values. Pointers distinguish "this source
// has nothing to say about this field" from "this source says empty", which is
// what makes field-level precedence possible (ADR 0008).
type Fields struct {
	Title         *string
	SortTitle     *string
	Year          *int
	Overview      *string
	Rating        *float64
	ContentRating *string
	ReleasedAt    *int64
	DurationMS    *int64
	Series        *string
	Season        *int
	Episode       *int
}

// Field names are the keys used by item_lock. They are a stable part of the
// API contract — clients send these strings to release a lock.
const (
	FieldTitle         = "title"
	FieldSortTitle     = "sort_title"
	FieldYear          = "year"
	FieldOverview      = "overview"
	FieldRating        = "rating"
	FieldContentRating = "content_rating"
	FieldReleasedAt    = "released_at"
	FieldDurationMS    = "duration_ms"
	FieldSeries        = "series"
	FieldSeason        = "season"
	FieldEpisode       = "episode"
	FieldGenres        = "genres"
	FieldCredits       = "credits"
	FieldArtwork       = "artwork"
)

// AllFields is every lockable field name.
var AllFields = []string{
	FieldTitle, FieldSortTitle, FieldYear, FieldOverview, FieldRating,
	FieldContentRating, FieldReleasedAt, FieldDurationMS, FieldSeries,
	FieldSeason, FieldEpisode, FieldGenres, FieldCredits, FieldArtwork,
}

// IsField reports whether name is a lockable field.
func IsField(name string) bool {
	for _, f := range AllFields {
		if f == name {
			return true
		}
	}
	return false
}

// CollectionRef is a grouping a record belongs to — TMDB's belongs_to_collection
// for a movie in a franchise or series. It is membership, not containment
// (ADR 0017): the item stays a top-level work; this only names the set it is
// part of, so the enricher can create the collection and link the item into it.
type CollectionRef struct {
	ExternalID string
	Name       string
	Artwork    []ArtRef
}

// Record is the normalized payload every source produces. Normalizing at the
// source boundary is what keeps everything downstream provider-agnostic.
type Record struct {
	Source     string // provider or local-source id
	ExternalID string
	Kind       Kind

	// IMDbID is the external imdb id when the source knows it (TMDB via
	// external_ids). It is the key third-party rating sources join on (ADR 0019),
	// and empty for sources that do not carry it.
	IMDbID string

	Fields  Fields
	Genres  []string
	Credits []Credit
	Artwork []ArtRef

	// Collection is set when the source knows this item belongs to a franchise
	// or series. Nil for the common standalone case.
	Collection *CollectionRef
}

// Provider is a searchable remote metadata source.
type Provider interface {
	ID() string
	Caps() Caps
	Search(ctx context.Context, q Query) ([]Candidate, error)
	Fetch(ctx context.Context, ref Ref) (*Record, error)
}

// LocalSource reads metadata that already sits beside the media. It cannot
// search, and forcing it to would mean lying about confidence — see ADR 0007.
type LocalSource interface {
	ID() string
	Read(ctx context.Context, path string, kind Kind) (*Record, error)
}

// TrailerProvider is an optional capability. Kept off Provider because most
// sources have no trailers to offer, and a required method they answer with
// "unsupported" is an abstraction paying no rent (ADR 0007).
type TrailerProvider interface {
	Trailer(ctx context.Context, ref Ref) (*Trailer, error)
}

// Trailer is a promotional video for an item.
type Trailer struct {
	Site string `json:"site"` // YouTube
	Key  string `json:"key"`
	Name string `json:"name"`
}

// RatingSource returns third-party scores for an item already identified by its
// IMDb id. It is deliberately not a Provider: it cannot search for identity, and
// forcing Search/Fetch on it would mean faking a confidence-scored candidate for
// a service that has no opinion on what a title is (ADR 0007, ADR 0019). The
// identity is resolved before it is ever called.
type RatingSource interface {
	ID() string
	Ratings(ctx context.Context, imdbID string) ([]Rating, error)
}

// Rating is one score from one source. Score is normalized to 0–10 so scores
// from different scales sort and compare uniformly; Display keeps the
// source-native form ("92%", "74", "7.7") for the UI.
type Rating struct {
	Source  string  // imdb | rotten_tomatoes | metacritic | …
	Score   float64 // normalized 0–10
	Display string  // source-native string
	Votes   int     // 0 when the source reports no count
}

// Trailer finds a trailer from the first provider that can supply one.
func (r *Registry) Trailer(ctx context.Context, providerID string, ref Ref) (*Trailer, error) {
	for _, p := range r.providers {
		if providerID != "" && p.ID() != providerID {
			continue
		}
		tp, ok := p.(TrailerProvider)
		if !ok {
			continue
		}
		t, err := tp.Trailer(ctx, ref)
		if err != nil || t == nil {
			continue
		}
		return t, nil
	}
	return nil, nil
}

// Registry holds the registered sources in precedence order.
type Registry struct {
	providers []Provider
	locals    []LocalSource
	ratings   []RatingSource
}

func NewRegistry() *Registry { return &Registry{} }

// AddProvider registers a remote provider. Order of registration is priority
// order.
func (r *Registry) AddProvider(p Provider) { r.providers = append(r.providers, p) }

// AddLocal registers a local source. Order of registration is priority order,
// and all local sources outrank all providers during merge.
func (r *Registry) AddLocal(l LocalSource) { r.locals = append(r.locals, l) }

// Providers returns the providers that support this kind, in priority order.
func (r *Registry) Providers(k Kind) []Provider {
	out := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		if p.Caps().Supports(k) {
			out = append(out, p)
		}
	}
	return out
}

// Locals returns every registered local source in priority order.
func (r *Registry) Locals() []LocalSource { return r.locals }

// AddRatingSource registers a third-party rating source (ADR 0019).
func (r *Registry) AddRatingSource(rs RatingSource) { r.ratings = append(r.ratings, rs) }

// HasRatingSources reports whether any rating source is configured, so the
// enricher can skip the rating pass entirely rather than resolve imdb ids for a
// lookup nothing will answer.
func (r *Registry) HasRatingSources() bool { return len(r.ratings) > 0 }

// Ratings collects scores for one imdb id from every rating source, in
// registration order. A source failing is logged by the caller, not fatal: a
// partial set is the expected state, since coverage differs per source.
func (r *Registry) Ratings(ctx context.Context, imdbID string) ([]Rating, error) {
	if imdbID == "" {
		return nil, nil
	}
	var all []Rating
	var firstErr error
	for _, rs := range r.ratings {
		got, err := rs.Ratings(ctx, imdbID)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		all = append(all, got...)
	}
	return all, firstErr
}

// Provider looks up a registered provider by id.
func (r *Registry) Provider(id string) (Provider, bool) {
	for _, p := range r.providers {
		if p.ID() == id {
			return p, true
		}
	}
	return nil, false
}

// Search asks every capable provider and returns candidates ranked against the
// query, best first.
func (r *Registry) Search(ctx context.Context, q Query) ([]Candidate, error) {
	var all []Candidate
	var firstErr error
	for _, p := range r.Providers(q.Kind) {
		cands, err := p.Search(ctx, q)
		if err != nil {
			// One provider failing must not sink the whole search.
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for i := range cands {
			cands[i].Provider = p.ID()
		}
		all = append(all, cands...)
	}
	if len(all) == 0 {
		return nil, firstErr
	}
	Rank(q, all)
	return all, nil
}

// Rank scores candidates against the query and sorts them best first.
func Rank(q Query, cands []Candidate) {
	for i := range cands {
		cands[i].Breakdown = ScoreBreakdown(q, cands[i])
		cands[i].Score = cands[i].Breakdown.Total
	}
	sort.SliceStable(cands, func(i, j int) bool {
		return cands[i].Score > cands[j].Score
	})
}

// Helpers for building Fields without a local variable at every call site.

func S(v string) *string   { return &v }
func I(v int) *int         { return &v }
func I64(v int64) *int64   { return &v }
func F(v float64) *float64 { return &v }
