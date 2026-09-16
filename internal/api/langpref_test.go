package api

import "testing"

/*
 * Every case here needs a real account, because the preference lives on one.
 * An unconfigured loopback server has none, which is its own case below rather
 * than the default this file works against.
 */
func langHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.secure(t, "a long enough password")
	return h
}

/*
 * Language preferences at the boundary.
 *
 * The rules themselves live in internal/store and are tested there against
 * streams. What is worth guarding here is the shape of the exchange: that a
 * caller can send back exactly what it read, that a bad code is refused rather
 * than stored, and that all three fields move together.
 *
 * The last is the one that would not fail loudly. A mode stored without a
 * language is silently inert — subtitles simply never appear — and somebody
 * would report it as the setting not working.
 */

func TestAnAccountStartsWithNoPreference(t *testing.T) {
	/*
	 * Every account that existed before this feature has empty columns, and
	 * must read as "no preference" rather than as English. Defaulting in a
	 * migration would choose on behalf of somebody who never asked, and on a
	 * library of foreign films it would change what plays on the next start.
	 */
	h := newHarness(t)
	var body map[string]any
	decode(t, h.do(t, "GET", "/api/profile/languages", nil), &body)

	if got := body["preferred_audio_lang"]; got != "" {
		t.Errorf("audio = %#v, want empty", got)
	}
	if got := body["subtitle_mode"]; got != "off" {
		t.Errorf("mode = %#v, want off", got)
	}
}

func TestWhatIsSentBackIsWhatWasRead(t *testing.T) {
	// A client must be able to read, change one field, and send the object
	// back. A GET whose shape differs from what PUT accepts makes that a
	// translation step, and a translation step is where a field gets dropped.
	h := langHarness(t)
	h.authed(t, "PUT", "/api/profile/languages", map[string]any{
		"preferred_audio_lang": "en", "preferred_subtitle_lang": "en",
		"subtitle_mode": "foreign",
	})

	var got map[string]any
	decode(t, h.authed(t, "GET", "/api/profile/languages", nil), &got)
	if got["preferred_audio_lang"] != "en" || got["preferred_subtitle_lang"] != "en" {
		t.Errorf("languages = %#v", got)
	}
	if got["subtitle_mode"] != "foreign" {
		t.Errorf("mode = %#v, want foreign", got["subtitle_mode"])
	}
}

func TestTheAnswerIsReturnedWithoutASecondRequest(t *testing.T) {
	// PUT answers with the stored state, so a page need not re-fetch to know
	// what it now has — and cannot render a value the server did not accept.
	h := langHarness(t)
	var body map[string]any
	decode(t, h.authed(t, "PUT", "/api/profile/languages", map[string]any{
		"preferred_audio_lang": "JPN", "preferred_subtitle_lang": "en",
		"subtitle_mode": "always",
	}), &body)

	// Normalised on the way in: one spelling is stored, so two clients cannot
	// disagree about whether a preference is set.
	if got := body["preferred_audio_lang"]; got != "jpn" {
		t.Errorf("audio = %#v, want the lower-cased code", got)
	}
}

func TestABadCodeIsRefusedRatherThanStored(t *testing.T) {
	/*
	 * Refused rather than ignored, for the same reason the certification
	 * country is: a setting that stores a value it will not act on is one
	 * somebody sets, watches change nothing, and reports as broken.
	 */
	h := langHarness(t)
	for _, body := range []map[string]any{
		{"preferred_audio_lang": "e", "preferred_subtitle_lang": "", "subtitle_mode": "off"},
		{"preferred_audio_lang": "engl", "preferred_subtitle_lang": "", "subtitle_mode": "off"},
		{"preferred_audio_lang": "e1", "preferred_subtitle_lang": "", "subtitle_mode": "off"},
		// A region belongs to a file, not to a preference.
		{"preferred_audio_lang": "en-US", "preferred_subtitle_lang": "", "subtitle_mode": "off"},
		{"preferred_audio_lang": "en", "preferred_subtitle_lang": "en", "subtitle_mode": "sometimes"},
	} {
		wantError(t, h.authed(t, "PUT", "/api/profile/languages", body), 400, "bad_request")
	}
}

func TestARefusedChangeStoresNothing(t *testing.T) {
	/*
	 * The half-applied case. A request carrying a good audio language and a bad
	 * mode must leave *both* alone — storing the language and refusing the mode
	 * would leave somebody with a preference they did not finish setting and no
	 * error to explain the state.
	 */
	h := langHarness(t)
	h.authed(t, "PUT", "/api/profile/languages", map[string]any{
		"preferred_audio_lang": "fr", "preferred_subtitle_lang": "fr", "subtitle_mode": "always",
	})
	wantError(t, h.authed(t, "PUT", "/api/profile/languages", map[string]any{
		"preferred_audio_lang": "de", "preferred_subtitle_lang": "de", "subtitle_mode": "nonsense",
	}), 400, "bad_request")

	var got map[string]any
	decode(t, h.authed(t, "GET", "/api/profile/languages", nil), &got)
	if got["preferred_audio_lang"] != "fr" {
		t.Errorf("audio = %#v; a refused request changed the stored value", got["preferred_audio_lang"])
	}
}

func TestClearingAPreferenceIsAllowed(t *testing.T) {
	// Empty is a valid answer, not a missing one: it is how somebody undoes the
	// choice, and refusing it would make the setting one-way.
	h := langHarness(t)
	h.authed(t, "PUT", "/api/profile/languages", map[string]any{
		"preferred_audio_lang": "en", "preferred_subtitle_lang": "en", "subtitle_mode": "always",
	})
	h.authed(t, "PUT", "/api/profile/languages", map[string]any{
		"preferred_audio_lang": "", "preferred_subtitle_lang": "", "subtitle_mode": "off",
	})

	var got map[string]any
	decode(t, h.authed(t, "GET", "/api/profile/languages", nil), &got)
	if got["preferred_audio_lang"] != "" || got["subtitle_mode"] != "off" {
		t.Errorf("after clearing: %#v", got)
	}
}

func TestRenamingDoesNotDisturbALanguagePreference(t *testing.T) {
	/*
	 * The reason this is its own route rather than another field on PATCH
	 * /api/profile. Two unrelated acts sharing a handler is how a rename comes
	 * to silently clear a preference.
	 */
	h := langHarness(t)
	h.authed(t, "PUT", "/api/profile/languages", map[string]any{
		"preferred_audio_lang": "en", "preferred_subtitle_lang": "en", "subtitle_mode": "foreign",
	})
	h.authed(t, "PATCH", "/api/profile", map[string]any{"name": "Renamed"})

	var got map[string]any
	decode(t, h.authed(t, "GET", "/api/profile/languages", nil), &got)
	if got["subtitle_mode"] != "foreign" || got["preferred_audio_lang"] != "en" {
		t.Errorf("a rename disturbed the preference: %#v", got)
	}
}

func TestAServerWithNoAccountsAnswersRatherThanFailing(t *testing.T) {
	/*
	 * The unconfigured loopback state, which is the one case that deliberately
	 * uses the bare harness. Reading answers the empty preference so the page
	 * renders; writing is 409, because there is no row to write to and
	 * pretending otherwise would be a setting that silently does nothing.
	 */
	h := newHarness(t)
	var body map[string]any
	decode(t, h.do(t, "GET", "/api/profile/languages", nil), &body)
	if body["subtitle_mode"] != "off" {
		t.Errorf("mode = %#v, want off", body["subtitle_mode"])
	}
	wantError(t, h.do(t, "PUT", "/api/profile/languages", map[string]any{
		"preferred_audio_lang": "en", "preferred_subtitle_lang": "en", "subtitle_mode": "off",
	}), 409, "no_account")
}
