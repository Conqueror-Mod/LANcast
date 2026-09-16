package rating

/*
 * Which countries' certificates this ladder can actually place.
 *
 * This list exists because of what a ceiling does with a label it does not
 * recognise: it blocks it. So offering somebody a certification country whose
 * labels are absent from `ages` would populate `content_rating` with strings
 * that every ceiling reads as *unrated*, and a household that set a limit for
 * a child would watch their library empty itself — the same failure the
 * unrated-kinds exemption was written for, arriving by a different door.
 *
 * France is the worked example of why this is a named list rather than "any
 * ISO code". TMDB returns "Tous publics" for a French U-equivalent; the ladder
 * has never heard of it, `Rank` answers Unknown, and the film disappears for
 * any account with a ceiling. Nothing fails, nothing logs, and the setting
 * that caused it is three screens away.
 *
 * Adding a country here is therefore two pieces of work and not one: the entry
 * *and* its labels in `ages`. TestEveryOfferedCountryCanBePlaced holds the two
 * together, because a list that drifts from the table it depends on is exactly
 * the bug this file is meant to prevent.
 */

// Country is a certification system this package understands.
type Country struct {
	// Code is the ISO 3166-1 alpha-2 code TMDB keys its certificates on.
	Code string
	// Name is what a person picking one reads.
	Name string
	/*
	 * Labels are the certificates this country's system issues, every one of
	 * which must be placeable on the ladder.
	 *
	 * Recorded rather than derived, and that is the point: `ages` is one flat
	 * table with no notion of country, so nothing else in the program can say
	 * which labels belong to which system. Writing them out is what makes the
	 * coupling testable.
	 */
	Labels []string
}

/*
 * Countries is the offered list, in the order a picker shows them.
 *
 * US first because it leads the default order, then GB which is its fallback,
 * then the rest alphabetically. Ireland is deliberately absent: its 15A and
 * 16 certificates are not both on the ladder, and half a system is worse than
 * none — it would place some Irish films and silently block the others.
 */
var Countries = []Country{
	{Code: "US", Name: "United States", Labels: []string{
		"G", "PG", "PG-13", "R", "NC-17",
		"TV-Y", "TV-Y7", "TV-G", "TV-PG", "TV-14", "TV-MA",
	}},
	{Code: "GB", Name: "United Kingdom", Labels: []string{
		"U", "UC", "PG", "12", "12A", "15", "18",
	}},
	{Code: "AU", Name: "Australia", Labels: []string{
		"G", "PG", "M", "MA15+", "R18+", "X18+",
	}},
	{Code: "DE", Name: "Germany", Labels: []string{
		"FSK 0", "FSK 6", "FSK 12", "FSK 16", "FSK 18",
	}},
	{Code: "NL", Name: "Netherlands", Labels: []string{
		"AL", "6", "9", "12", "16",
	}},
}

// KnownCountry reports whether a code may be set as the certification country.
//
// An unknown code is refused at the API rather than accepted and ignored: a
// setting that stores a value it will not act on is one somebody sets, watches
// change nothing, and reports as broken.
func KnownCountry(code string) bool {
	for _, c := range Countries {
		if c.Code == code {
			return true
		}
	}
	return false
}
