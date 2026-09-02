package meta

// Merge resolves every field independently through the precedence chain:
//
//	user lock → local sources (NFO) → providers → existing value
//
// A locked field is never touched, however good the incoming data looks. That
// guarantee is what makes correcting metadata safe, and therefore something
// users will actually do (ADR 0008).
//
// current is the item as it stands; locals and remotes are ordered best-first
// within their tier. The returned Record carries the resolved values.
func Merge(current Record, locked map[string]bool, locals, remotes []Record) Record {
	out := current

	// Local sources outrank providers for every field.
	ordered := make([]Record, 0, len(locals)+len(remotes))
	ordered = append(ordered, locals...)
	ordered = append(ordered, remotes...)

	pick(locked, FieldTitle, &out.Fields.Title, ordered, func(r Record) *string { return r.Fields.Title })
	pick(locked, FieldSortTitle, &out.Fields.SortTitle, ordered, func(r Record) *string { return r.Fields.SortTitle })
	pick(locked, FieldYear, &out.Fields.Year, ordered, func(r Record) *int { return r.Fields.Year })
	pick(locked, FieldOverview, &out.Fields.Overview, ordered, func(r Record) *string { return r.Fields.Overview })
	pick(locked, FieldRating, &out.Fields.Rating, ordered, func(r Record) *float64 { return r.Fields.Rating })
	pick(locked, FieldContentRating, &out.Fields.ContentRating, ordered, func(r Record) *string { return r.Fields.ContentRating })
	pick(locked, FieldReleasedAt, &out.Fields.ReleasedAt, ordered, func(r Record) *int64 { return r.Fields.ReleasedAt })

	// Duration is measured, not described, and this is the one field where a
	// provider is answering a different question. TMDB's runtime is the
	// theatrical one in whole minutes; the file is what a viewer actually
	// plays, and across a 40-file sample of this library one in eight
	// disagreed by more than 2%: The Book of Eli declared 118 minutes against
	// a 110.2-minute file, Ghostbusters 117 against 133.7.
	//
	// That corrupts every percentage computed from a duration, and the watched
	// threshold worst of all, because it is a denominator. An overstated
	// runtime puts 90% past the end of the file, so the item cannot be
	// finished by watching it — which is exactly how it was reported.
	//
	// So a source may supply a duration only when nothing has measured one.
	// An NFO still carries a file ffprobe has not reached yet, and stops
	// arguing with ffprobe the moment it has.
	fill(locked, FieldDurationMS, &out.Fields.DurationMS, ordered, func(r Record) *int64 { return r.Fields.DurationMS })
	pick(locked, FieldSeries, &out.Fields.Series, ordered, func(r Record) *string { return r.Fields.Series })
	pick(locked, FieldSeason, &out.Fields.Season, ordered, func(r Record) *int { return r.Fields.Season })
	pick(locked, FieldEpisode, &out.Fields.Episode, ordered, func(r Record) *int { return r.Fields.Episode })

	// List fields are all-or-nothing: a partial cast list is worse than either
	// the old one or the new one, so the first source with any entries wins.
	if !locked[FieldGenres] {
		for _, r := range ordered {
			if len(r.Genres) > 0 {
				out.Genres = r.Genres
				break
			}
		}
	}
	if !locked[FieldCredits] {
		for _, r := range ordered {
			if len(r.Credits) > 0 {
				out.Credits = r.Credits
				break
			}
		}
	}
	if !locked[FieldArtwork] {
		for _, r := range ordered {
			if len(r.Artwork) > 0 {
				out.Artwork = r.Artwork
				break
			}
		}
	}

	// Provenance follows the highest-priority remote that contributed, so the
	// item can be refreshed or re-matched later.
	for _, r := range remotes {
		if r.ExternalID != "" {
			out.Source, out.ExternalID = r.Source, r.ExternalID
			break
		}
	}
	return out
}

// pick assigns the first non-nil value from sources, unless the field is
// locked, in which case dst keeps whatever it already held.
func pick[T any](locked map[string]bool, field string, dst **T, sources []Record, get func(Record) *T) {
	if locked[field] {
		return
	}
	for _, r := range sources {
		if v := get(r); v != nil {
			*dst = v
			return
		}
	}
}

// fill is pick for a field the file itself owns: a source may offer a value
// when there is none, and may never replace one that exists.
func fill[T any](locked map[string]bool, field string, dst **T, sources []Record, get func(Record) *T) {
	if *dst != nil {
		return
	}
	pick(locked, field, dst, sources, get)
}

// LockedSet turns a slice of field names into the set Merge expects, ignoring
// anything that is not a real field.
func LockedSet(fields []string) map[string]bool {
	out := make(map[string]bool, len(fields))
	for _, f := range fields {
		if IsField(f) {
			out[f] = true
		}
	}
	return out
}
