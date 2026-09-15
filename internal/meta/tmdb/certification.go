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
 * countryOrder is which country's certificate to take.
 *
 * Measured before it was chosen: of thirty films sampled at random from a real
 * library, **thirty had a US certificate and thirty had a GB one**, so the
 * order decides the label rather than whether there is one. US leads because
 * this server's other metadata is US-shaped and because a household reading
 * "PG-13" recognises it.
 *
 * GB is the fallback rather than "any country", deliberately. A German FSK or
 * an Australian MA15+ on a film's detail page is a surprise to a household that
 * has never seen one, and internal/rating places them all on the same ladder
 * anyway — so the ceiling gains nothing from a label nobody recognises. A
 * country *setting* is the obvious next step and is not this change.
 */
var countryOrder = []string{"US", "GB"}

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
// in a country this reads.
func movieCertification(block releaseDatesBlock) string {
	for _, country := range countryOrder {
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
func showCertification(block contentRatingsBlock) string {
	for _, country := range countryOrder {
		for _, result := range block.Results {
			if result.Country == country && result.Rating != "" {
				return result.Rating
			}
		}
	}
	return ""
}
