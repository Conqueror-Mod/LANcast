package rating

import "testing"

/*
 * The coupling between the offered countries and the ladder that has to place
 * their labels.
 *
 * This is the only test in the package that can fail because of something
 * *absent*. Everything else here asks what Rank does with a label somebody
 * wrote down; this asks whether the program is offering a choice it cannot
 * honour, which is the failure that does not surface as an error — a ceiling
 * blocks an unplaceable label, so the symptom is a library that quietly
 * shrinks for one account.
 */
func TestEveryOfferedCountryCanBePlaced(t *testing.T) {
	if len(Countries) == 0 {
		t.Fatal("no certification countries are offered at all")
	}
	for _, c := range Countries {
		if c.Code == "" || c.Name == "" {
			t.Errorf("country %+v is missing a code or a name", c)
		}
		if len(c.Labels) == 0 {
			t.Errorf("%s (%s) offers no labels, so nothing proves the ladder can place it",
				c.Name, c.Code)
		}
		for _, label := range c.Labels {
			if !Known(label) {
				t.Errorf("%s (%s) issues %q and the ladder cannot place it — "+
					"a ceiling would read it as unrated and block the title",
					c.Name, c.Code, label)
			}
		}
	}
}

// The default order has to be offerable, or the setting cannot express the
// behaviour the server already has.
func TestTheDefaultCountriesAreOffered(t *testing.T) {
	for _, code := range []string{"US", "GB"} {
		if !KnownCountry(code) {
			t.Errorf("%s is in the default order but is not an offered country", code)
		}
	}
}

func TestAnUnofferedCountryIsRefused(t *testing.T) {
	/*
	 * France is the case this list exists to refuse, and it is refused for a
	 * reason that is easy to mistake for an oversight: "Tous publics" is a
	 * perfectly real certificate, and the ladder simply has no rung for it.
	 * Adding FR without adding its labels is the mistake, so the guard is
	 * here as well as in the test above.
	 */
	for _, code := range []string{"FR", "JP", "", "us", "USA", "ZZ"} {
		if KnownCountry(code) {
			t.Errorf("%q was accepted as a certification country", code)
		}
	}
}

// Lower case is refused rather than normalised, so that the one place a code
// is compared is the one place it is spelled. Callers upper-case first.
func TestCodesAreComparedExactly(t *testing.T) {
	if KnownCountry("gb") {
		t.Error(`"gb" was accepted; callers normalise before asking`)
	}
	if !KnownCountry("GB") {
		t.Error(`"GB" was refused`)
	}
}
