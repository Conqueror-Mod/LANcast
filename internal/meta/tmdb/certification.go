package tmdb

/*
 * Which certificate, out of the dozen a title carries.
 *
 * TMDB returns one per country, and a film's country entry holds one per
 * release type on top of that. Choosing is a rule rather than a lookup, so it
 * lives here, pure, and is tested against fixtures.
 *
 * This exists because `content_rating` was NULL on all 19,460 items of a real
 * library. Nothing in this project had ever written it but an NFO sidecar, so
 * the account rating ceiling stood on an empty column — and the check that
 * would have caught it before the ceiling was designed is the one the photo
 * timeline ran and this did not.
 */

/*
 * defaultCountryOrder is which country's certificate to take when nobody has
 * chosen.
 *
 * Measured before it was chosen: of thirty films sampled at random from a real
 * library, **thirty had a US certificate and thirty had a GB one**, so the
 * order decides the label rather than whether there is one. US leads because
 * this server's other metadata is US-shaped and because a household reading
 * "PG-13" recognises it.
 *
 * A configured country is placed *in front of* this rather than replacing it —
 * see certificationOrder. The fallback is the whole reason a setting is safe
 * to offer: TMDB's coverage is uneven, and a household that picks Germany
 * still wants a label on the films that carry no FSK entry.
 */
var defaultCountryOrder = []string{"US", "GB"}

/*
 * certificationOrder is the order to read countries in for one client.
 *
 * The configured country first, then the default order with it removed, so
 * choosing "GB" reorders rather than narrowing and choosing "DE" adds a rung
 * on top without taking anything away. An empty or unrecognised preference is
 * simply the default — this is reached on every fetch and is not the place to
 * discover that a setting is wrong, which is what the API's validation is for.
 *
 * Which countries may be configured at all is `rating.Countries`, and the
 * constraint behind that list is not cosmetic: a certificate the ladder cannot
 * place reads as unrated, and a ceiling blocks unrated.
 */
func certificationOrder(preferred string) []string {
	if preferred == "" {
		return defaultCountryOrder
	}
	order := []string{preferred}
	for _, c := range defaultCountryOrder {
		if c != preferred {
			order = append(order, c)
		}
	}
	return order
}

/*
 * releaseTypeOrder is which release's certificate to take within a country.
 *
 * The theatrical certificate is the one people mean. A film's entry routinely
 * carries several — a premiere with no certificate at all, a theatrical one, a
 * digital re-rate — and taking the first non-empty one gets whichever order
 * TMDB happened to return. 0 means "any type", tried last so that a
 * direct-to-digital title still reports something.
 */
var releaseTypeOrder = []int{3, 2, 0}

// movieCertification picks a film's certificate, or empty when it carries none
// in a country this reads. The order is a parameter rather than a package
// global so that one server's preference cannot leak into another's tests.
func movieCertification(block releaseDatesBlock, order []string) string {
	for _, country := range order {
		for _, result := range block.Results {
			if result.Country != country {
				continue
			}
			for _, wantType := range releaseTypeOrder {
				for _, release := range result.Dates {
					// An empty certificate is extremely common — most premiere
					// entries carry one — and is not an answer.
					if release.Certification == "" {
						continue
					}
					if wantType == 0 || release.Type == wantType {
						return release.Certification
					}
				}
			}
		}
	}
	return ""
}

// showCertification picks a programme's rating. Television carries one per
// country with no release types, so this is the same country rule without the
// inner one.
func showCertification(block contentRatingsBlock, order []string) string {
	for _, country := range order {
		for _, result := range block.Results {
			if result.Country == country && result.Rating != "" {
				return result.Rating
			}
		}
	}
	return ""
}
