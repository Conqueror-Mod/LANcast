package api

import (
	"testing"

	"lancast/internal/rating"
)

/*
 * The certification country setting.
 *
 * What makes this worth guarding at the API rather than only in the provider:
 * the value decides what goes into `content_rating`, and `content_rating` is
 * what an account's rating ceiling reads. A code the ladder cannot place
 * produces labels every ceiling treats as unrated and blocks — so accepting
 * one here would empty a child's library from a settings page three screens
 * away, with nothing failing and nothing logged.
 */

func TestTheOfferedCountriesAreServed(t *testing.T) {
	h := newHarness(t)
	var body map[string]any
	decode(t, h.do(t, "GET", "/api/settings", nil), &body)

	raw, ok := body["certification_countries"]
	if !ok {
		t.Fatal("GET /api/settings omits certification_countries, so no client can offer the choice")
	}
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		t.Fatalf("certification_countries = %#v, want a non-empty list", raw)
	}
	if len(list) != len(rating.Countries) {
		t.Errorf("served %d countries, the ladder knows %d", len(list), len(rating.Countries))
	}
	for _, entry := range list {
		c, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("country entry = %#v, want an object", entry)
		}
		code, _ := c["code"].(string)
		name, _ := c["name"].(string)
		if code == "" || name == "" {
			t.Errorf("country %#v is missing a code or a name", c)
		}
		// The served list and the ladder are only the same list by intention.
		if !rating.KnownCountry(code) {
			t.Errorf("served %q, which the ladder cannot place", code)
		}
	}
	// Unset by default, which is the US-then-GB order.
	if got, ok := body["certification_country"]; !ok || got != "" {
		t.Errorf("certification_country = %#v, want it present and empty", got)
	}
}

func TestACountryTheLadderCannotPlaceIsRefused(t *testing.T) {
	/*
	 * France is the case, and it is refused for a reason that reads as a bug
	 * until you know it: "Tous publics" is a real certificate with no rung on
	 * the ladder. Storing FR would be worse than refusing it — the setting
	 * would appear to work and quietly start hiding films.
	 */
	h := newHarness(t)
	for _, code := range []string{"FR", "JP", "ZZ", "USA", "1"} {
		wantError(t, h.do(t, "PUT", "/api/settings",
			map[string]any{"certification_country": code}), 400, "bad_request")
	}
}

func TestChoosingACountryIsStoredAndReadBack(t *testing.T) {
	h := newHarness(t)
	h.do(t, "PUT", "/api/settings", map[string]any{"certification_country": "GB"})

	var body map[string]any
	decode(t, h.do(t, "GET", "/api/settings", nil), &body)
	if got := body["certification_country"]; got != "GB" {
		t.Errorf("certification_country = %#v, want GB", got)
	}
}

func TestACountryIsNormalisedBeforeItIsJudged(t *testing.T) {
	// It arrives from a form, so "gb" and " GB " are the same statement. The
	// stored value is the canonical one, or two clients would disagree about
	// whether the setting is set.
	h := newHarness(t)
	h.do(t, "PUT", "/api/settings", map[string]any{"certification_country": " gb "})

	var body map[string]any
	decode(t, h.do(t, "GET", "/api/settings", nil), &body)
	if got := body["certification_country"]; got != "GB" {
		t.Errorf("certification_country = %#v, want GB", got)
	}
}

func TestClearingTheCountryReturnsToTheDefault(t *testing.T) {
	// Empty is a valid value, not a missing one: it is how somebody undoes the
	// choice, and refusing it would make the setting one-way.
	h := newHarness(t)
	h.do(t, "PUT", "/api/settings", map[string]any{"certification_country": "DE"})
	h.do(t, "PUT", "/api/settings", map[string]any{"certification_country": ""})

	var body map[string]any
	decode(t, h.do(t, "GET", "/api/settings", nil), &body)
	if got := body["certification_country"]; got != "" {
		t.Errorf("certification_country = %#v, want it cleared", got)
	}
}

func TestOtherSettingsSurviveChoosingACountry(t *testing.T) {
	/*
	 * The partial-update rule, asserted for the new field. A PUT naming one
	 * setting must not reset the others — the same guarantee
	 * TestSettingsPartialUpdatePreservesKey makes for the API key, which is
	 * worth repeating here because this field was added to that handler.
	 */
	h := newHarness(t)
	h.do(t, "PUT", "/api/settings", map[string]any{"watched_threshold": 75})
	h.do(t, "PUT", "/api/settings", map[string]any{"certification_country": "AU"})

	var body map[string]any
	decode(t, h.do(t, "GET", "/api/settings", nil), &body)
	if got := body["watched_threshold"]; got != float64(75) {
		t.Errorf("watched_threshold = %#v, want 75 to have survived", got)
	}
	if got := body["certification_country"]; got != "AU" {
		t.Errorf("certification_country = %#v, want AU", got)
	}
}
