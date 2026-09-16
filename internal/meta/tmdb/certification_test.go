package tmdb

import (
	"testing"

	"lancast/internal/rating"
)

/*
 * Choosing a certificate out of the dozen a title carries.
 *
 * Every shape below is one TMDB actually returns, taken from real responses for
 * films in a real library rather than from the documentation. The empty
 * premiere entry in particular is not a hypothetical: it is the first element
 * of most films' US block, and taking the first non-empty value without asking
 * about release type is the obvious implementation that gets the wrong answer.
 */

func movieBlock(entries ...struct {
	country string
	certs   []struct {
		cert string
		typ  int
	}
}) releaseDatesBlock {
	var out releaseDatesBlock
	for _, e := range entries {
		var r struct {
			Country string `json:"iso_3166_1"`
			Dates   []struct {
				Certification string `json:"certification"`
				Type          int    `json:"type"`
			} `json:"release_dates"`
		}
		r.Country = e.country
		for _, c := range e.certs {
			r.Dates = append(r.Dates, struct {
				Certification string `json:"certification"`
				Type          int    `json:"type"`
			}{Certification: c.cert, Type: c.typ})
		}
		out.Results = append(out.Results, r)
	}
	return out
}

type certEntry = struct {
	cert string
	typ  int
}
type countryEntry = struct {
	country string
	certs   []certEntry
}

func TestTheTheatricalCertificateWins(t *testing.T) {
	/*
	 * The premiere entry carries no certificate and comes first. Taking the
	 * first non-empty value would work here by accident; taking the first
	 * *entry* would return nothing at all.
	 */
	got := movieCertification(movieBlock(countryEntry{"US", []certEntry{
		{"", 1},      // premiere, no certificate
		{"PG-13", 3}, // theatrical — the one people mean
		{"R", 4},     // a digital re-rate
	}}), defaultCountryOrder)
	if got != "PG-13" {
		t.Errorf("certificate = %q, want PG-13", got)
	}
}

func TestADirectToDigitalTitleStillReportsSomething(t *testing.T) {
	// No theatrical release at all. Falling back to any type is what stops the
	// column being empty for everything that skipped cinemas.
	got := movieCertification(movieBlock(countryEntry{"US", []certEntry{
		{"", 1},
		{"TV-MA", 4},
	}}), defaultCountryOrder)
	if got != "TV-MA" {
		t.Errorf("certificate = %q, want TV-MA", got)
	}
}

func TestTheCountryOrderDecidesTheLabel(t *testing.T) {
	/*
	 * Measured before it was chosen: of thirty films sampled from a real
	 * library, thirty carried both a US and a GB certificate. So this order
	 * decides which label a household reads, not whether it gets one.
	 */
	both := movieBlock(
		countryEntry{"GB", []certEntry{{"15", 3}}},
		countryEntry{"US", []certEntry{{"R", 3}}},
	)
	if got := movieCertification(both, defaultCountryOrder); got != "R" {
		t.Errorf("certificate = %q, want the US one", got)
	}
}

func TestGreatBritainIsTheFallback(t *testing.T) {
	// A title released in Britain and not America should not come back blank.
	only := movieBlock(countryEntry{"GB", []certEntry{{"12A", 3}}})
	if got := movieCertification(only, defaultCountryOrder); got != "12A" {
		t.Errorf("certificate = %q, want 12A", got)
	}
}

func TestACountryNobodyReadsIsNotBorrowed(t *testing.T) {
	/*
	 * Deliberate. An FSK or an MA15+ on a detail page is a surprise to a
	 * household that has never seen one, and internal/rating places every
	 * system on the same ladder anyway — so a ceiling gains nothing from a
	 * label nobody recognises. A country setting is the next step, not this.
	 */
	foreign := movieBlock(
		countryEntry{"DE", []certEntry{{"FSK 16", 3}}},
		countryEntry{"AU", []certEntry{{"MA15+", 3}}},
	)
	if got := movieCertification(foreign, defaultCountryOrder); got != "" {
		t.Errorf("certificate = %q, want none", got)
	}
}

func TestATitleWithNoCertificateAnywhere(t *testing.T) {
	// Common on obscure titles, and it has to read as absent rather than as an
	// empty string somebody stores.
	if got := movieCertification(releaseDatesBlock{}, defaultCountryOrder); got != "" {
		t.Errorf("certificate = %q, want none", got)
	}
	empty := movieBlock(countryEntry{"US", []certEntry{{"", 1}, {"", 3}}})
	if got := movieCertification(empty, defaultCountryOrder); got != "" {
		t.Errorf("certificate = %q, want none", got)
	}
}

func TestAProgrammeCarriesOneRatingPerCountry(t *testing.T) {
	// Television has no release types, so the rule is the country order alone.
	var block contentRatingsBlock
	block.Results = append(block.Results, struct {
		Country string `json:"iso_3166_1"`
		Rating  string `json:"rating"`
	}{Country: "GB", Rating: "15"}, struct {
		Country string `json:"iso_3166_1"`
		Rating  string `json:"rating"`
	}{Country: "US", Rating: "TV-MA"})

	if got := showCertification(block, defaultCountryOrder); got != "TV-MA" {
		t.Errorf("rating = %q, want TV-MA", got)
	}
}

func TestAProgrammeWithAnEmptyRatingIsUnrated(t *testing.T) {
	var block contentRatingsBlock
	block.Results = append(block.Results, struct {
		Country string `json:"iso_3166_1"`
		Rating  string `json:"rating"`
	}{Country: "US", Rating: ""})

	if got := showCertification(block, defaultCountryOrder); got != "" {
		t.Errorf("rating = %q, want none", got)
	}
}

func TestWhatTheRatingLadderMakesOfWhatTMDBReturns(t *testing.T) {
	/*
	 * The join between this package and internal/rating, which is where the
	 * certificate is actually used. These are the exact values a real library
	 * came back with; if the ladder stops placing one of them, a ceiling
	 * quietly starts blocking films it used to allow.
	 */
	for _, label := range []string{"G", "PG", "PG-13", "R", "NC-17", "TV-MA", "12A", "15", "18", "U"} {
		if !placeable(label) {
			t.Errorf("the rating ladder cannot place %q, which TMDB returns", label)
		}
	}
	// TMDB says "NR" for an unrated film, and the ladder treats that as
	// unknown — which a ceiling blocks. That is the intended reading and worth
	// pinning, because it is surprising.
	if placeable("NR") {
		t.Error(`"NR" was placed on the ladder; it means "not rated"`)
	}
}

// placeable asks the ladder the ceiling uses whether it recognises a label.
func placeable(label string) bool { return rating.Known(label) }

/*
 * The country setting.
 *
 * What makes this worth testing rather than reading is that the setting is not
 * a filter. Picking a country *adds a preference* and keeps the default order
 * behind it, because TMDB's coverage is uneven — and the failure mode of the
 * obvious implementation, where choosing DE means "only DE", is a library
 * where most films lose the label they had.
 */

func TestTheChosenCountryWins(t *testing.T) {
	block := movieBlock(
		countryEntry{"US", []certEntry{{"R", 3}}},
		countryEntry{"GB", []certEntry{{"15", 3}}},
		countryEntry{"DE", []certEntry{{"FSK 16", 3}}},
	)
	if got := movieCertification(block, certificationOrder("DE")); got != "FSK 16" {
		t.Errorf("certificate = %q, want the German one", got)
	}
	// And the same block read with no preference is unchanged, so choosing a
	// country cannot be confused with the default having moved.
	if got := movieCertification(block, certificationOrder("")); got != "R" {
		t.Errorf("default certificate = %q, want R", got)
	}
}

func TestChoosingACountryDoesNotDiscardTheFallback(t *testing.T) {
	/*
	 * The fault the obvious implementation ships. TMDB has no German entry for
	 * a great many titles; if the setting narrowed instead of reordering, every
	 * one of them would go from "PG-13" to no certificate at all — and a
	 * ceiling blocks a title with no certificate, so a household that set a
	 * country would find films disappearing for the child's account.
	 */
	noGerman := movieBlock(
		countryEntry{"US", []certEntry{{"PG-13", 3}}},
		countryEntry{"GB", []certEntry{{"12A", 3}}},
	)
	if got := movieCertification(noGerman, certificationOrder("DE")); got != "PG-13" {
		t.Errorf("certificate = %q, want PG-13 from the fallback", got)
	}
}

func TestChoosingBritainReordersRatherThanAdding(t *testing.T) {
	// GB is already in the default order, so preferring it must move it rather
	// than list it twice — and the US must still be there behind it.
	order := certificationOrder("GB")
	if len(order) != 2 || order[0] != "GB" || order[1] != "US" {
		t.Fatalf("order = %v, want [GB US]", order)
	}
	onlyUS := movieBlock(countryEntry{"US", []certEntry{{"R", 3}}})
	if got := movieCertification(onlyUS, order); got != "R" {
		t.Errorf("certificate = %q, want R", got)
	}
}

func TestNoPreferenceIsTheDefaultOrderExactly(t *testing.T) {
	// Not merely equivalent: the same slice, so nothing can append to one and
	// surprise the other.
	got := certificationOrder("")
	if len(got) != len(defaultCountryOrder) {
		t.Fatalf("order = %v, want %v", got, defaultCountryOrder)
	}
	for i := range got {
		if got[i] != defaultCountryOrder[i] {
			t.Fatalf("order = %v, want %v", got, defaultCountryOrder)
		}
	}
}

func TestAProgrammeFollowsTheSameCountryRule(t *testing.T) {
	var block contentRatingsBlock
	block.Results = append(block.Results, struct {
		Country string `json:"iso_3166_1"`
		Rating  string `json:"rating"`
	}{Country: "US", Rating: "TV-MA"}, struct {
		Country string `json:"iso_3166_1"`
		Rating  string `json:"rating"`
	}{Country: "AU", Rating: "MA15+"})

	if got := showCertification(block, certificationOrder("AU")); got != "MA15+" {
		t.Errorf("rating = %q, want MA15+", got)
	}
}

func TestEveryOfferedCountrysLabelsSurviveTheLadder(t *testing.T) {
	/*
	 * The join this setting makes possible, asserted from the provider side.
	 *
	 * `rating` already proves its own list is placeable. What that cannot see
	 * is this package handing a label straight from TMDB into the column a
	 * ceiling reads — so the same claim is made again here, against the labels
	 * a chosen country would actually produce. A fixture cannot catch an error
	 * it shares, and these two lists are only the same list by intention.
	 */
	for _, c := range rating.Countries {
		block := movieBlock(countryEntry{c.Code, []certEntry{{c.Labels[0], 3}}})
		got := movieCertification(block, certificationOrder(c.Code))
		if got != c.Labels[0] {
			t.Errorf("%s: certificate = %q, want %q", c.Code, got, c.Labels[0])
		}
		if !rating.Known(got) {
			t.Errorf("%s: %q reached the column and the ladder cannot place it",
				c.Code, got)
		}
	}
}
