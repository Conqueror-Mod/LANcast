package rating

import "testing"

/*
 * The ladder, one case per claim.
 *
 * The model here is decide_test.go in internal/probe: a named test per case,
 * asserting the decision *and* the reason it was made, so a change that alters
 * one system's placement says which one it broke.
 */

func TestLabelsFromDifferentCountriesLineUp(t *testing.T) {
	/*
	 * The reason ranks are ages rather than list positions. The US has five
	 * film certificates and the UK six; pairing them by index puts R beside 15
	 * counting one way and beside 18 counting the other.
	 */
	cases := []struct{ a, b string }{
		{"PG-13", "12"}, // both adolescent
		{"R", "TV-MA"},  // both 17
		{"NC-17", "18"}, // both adult
		{"G", "U"},      // both everybody
	}
	for _, c := range cases {
		ra, rb := Rank(c.a), Rank(c.b)
		if ra == Unknown || rb == Unknown {
			t.Errorf("%s/%s: one of them is unknown (%d/%d)", c.a, c.b, ra, rb)
			continue
		}
		if diff := ra - rb; diff > 3 || diff < -3 {
			t.Errorf("%s ranks %d and %s ranks %d; they should be neighbours", c.a, ra, c.b, rb)
		}
	}
}

func TestTelevisionAndFilmShareOneLadder(t *testing.T) {
	// A household setting a limit does not hold two opinions, one for films
	// and one for episodes. A ceiling governing half a library is a gap
	// somebody finds by accident.
	if !Allowed("TV-14", "R") {
		t.Error("an episode rated TV-14 was blocked by a ceiling of R")
	}
	if Allowed("TV-MA", "PG-13") {
		t.Error("a TV-MA episode passed a PG-13 ceiling")
	}
}

func TestAnUnratedItemIsBlockedByAnyCeiling(t *testing.T) {
	/*
	 * The uncomfortable half, and the one worth a permanent test.
	 *
	 * Letting unrated items through puts the hole exactly where the unlabelled
	 * sits — home video, anything a provider never matched, most of what
	 * somebody added by hand. A limit that stops at the catalogued and waves
	 * the rest past is not a limit.
	 */
	for _, label := range []string{"", "NR", "Unrated", "Not Rated", "Certificate 27"} {
		if Allowed(label, "PG") {
			t.Errorf("an item rated %q passed a PG ceiling", label)
		}
	}
}

func TestNoCeilingAllowsEverything(t *testing.T) {
	// No limit is the default and the ordinary case. An account without one is
	// not a restricted account, and must not be treated as one.
	for _, label := range []string{"", "NC-17", "18", "who knows"} {
		if !Allowed(label, "") {
			t.Errorf("%q was blocked with no ceiling set", label)
		}
	}
}

func TestACeilingNobodyCanPlaceIsNotACeiling(t *testing.T) {
	/*
	 * Rather than blocking the entire library.
	 *
	 * The settings screen offers only known labels, so this is a
	 * belt-and-braces case — but the failure it guards is a restricted account
	 * whose library is empty and whose owner has no way to tell why.
	 */
	if !Allowed("R", "Certificate 27") {
		t.Error("an unrecognised ceiling hid an item instead of being ignored")
	}
	if Known("Certificate 27") {
		t.Error("an unrecognised label reported itself as usable")
	}
}

func TestTheSameCertificateWrittenDifferently(t *testing.T) {
	// Labels come from provider data and from NFO files people wrote by hand.
	for _, label := range []string{"tv-ma", "TV-MA ", " TV-MA", "US:TV-MA", "Rated TV-MA"} {
		if got := Rank(label); got != Rank("TV-MA") {
			t.Errorf("Rank(%q) = %d, want the same as TV-MA (%d)", label, got, Rank("TV-MA"))
		}
	}
}

func TestACountryQualifierIsNotPartOfTheCertificate(t *testing.T) {
	// Several providers qualify the certificate with who issued it, and the
	// qualifier does not change what it means.
	if Rank("DE:FSK 16") != Rank("FSK 16") {
		t.Error("a country prefix changed what a German certificate meant")
	}
}

func TestAnItemAtTheCeilingIsAllowed(t *testing.T) {
	// At or under. A ceiling of PG-13 exists in order to permit PG-13.
	if !Allowed("PG-13", "PG-13") {
		t.Error("an item exactly at the ceiling was blocked")
	}
}

func TestTheLabelsAQueryFiltersOn(t *testing.T) {
	/*
	 * This is what turns the rule into a database query, so the set must agree
	 * with Allowed exactly — a divergence here would show one library in the
	 * grid and permit a different one at playback, which is the failure mode
	 * this whole feature exists to prevent.
	 */
	labels := AllowedLabels("PG-13")
	if len(labels) == 0 {
		t.Fatal("no labels at all under PG-13")
	}
	for _, l := range labels {
		if !Allowed(l, "PG-13") {
			t.Errorf("AllowedLabels offered %q, which Allowed refuses", l)
		}
	}
	for _, blocked := range []string{"R", "TV-MA", "18", "NC-17"} {
		for _, l := range labels {
			if l == blocked {
				t.Errorf("%q is under a PG-13 ceiling", blocked)
			}
		}
	}
	if AllowedLabels("") != nil {
		t.Error("no ceiling produced a filter; it must produce none at all")
	}
}

func TestUnknownIsNotSuitableForEverybody(t *testing.T) {
	// Distinct from every real age, so a caller comparing numbers cannot
	// accidentally read "unknown" as "suitable for all".
	if Unknown >= 0 {
		t.Fatal("Unknown sits on the ladder, where a comparison can reach it")
	}
	if Rank("G") == Unknown {
		t.Error("G is not unknown")
	}
}
