package api

import "testing"

/*
 * Subtitle style at the boundary. The rules (what a player can draw) are in
 * internal/store; here, the exchange: none until chosen, read back what was
 * written, a style no player can draw is refused and stores nothing, and a
 * server with no accounts answers rather than failing.
 */
func TestASubtitleStyleIsNoneUntilChosen(t *testing.T) {
	h := langHarness(t)
	var body map[string]any
	decode(t, h.authed(t, "GET", "/api/profile/subtitle-style", nil), &body)
	if v, ok := body["style"]; !ok || v != nil {
		t.Fatalf("a new account's style = %#v; want null, so each device keeps its own", body)
	}
}

func TestASubtitleStyleIsReadBackAsWritten(t *testing.T) {
	h := langHarness(t)
	style := map[string]any{"color": "#ffe066", "font": "serif", "size": 1.35, "position": 12.0}
	var put map[string]any
	decode(t, h.authed(t, "PUT", "/api/profile/subtitle-style", style), &put)

	var got map[string]any
	decode(t, h.authed(t, "GET", "/api/profile/subtitle-style", nil), &got)
	s, _ := got["style"].(map[string]any)
	for k, v := range style {
		if s[k] != v {
			t.Errorf("%s = %#v, want %#v (whole answer %#v)", k, s[k], v, got)
		}
	}
}

func TestASubtitleStyleNoPlayerCanDrawIsRefused(t *testing.T) {
	h := langHarness(t)
	wantError(t, h.authed(t, "PUT", "/api/profile/subtitle-style",
		map[string]any{"color": "red", "font": "sans", "size": 1, "position": 8}), 400, "bad_request")

	var got map[string]any
	decode(t, h.authed(t, "GET", "/api/profile/subtitle-style", nil), &got)
	if got["style"] != nil {
		t.Errorf("a refused style was stored: %#v", got)
	}
}

func TestASubtitleStyleWithNoAccountsAnswersRatherThanFailing(t *testing.T) {
	h := newHarness(t)
	var body map[string]any
	decode(t, h.do(t, "GET", "/api/profile/subtitle-style", nil), &body)
	if body["style"] != nil {
		t.Errorf("style = %#v, want null", body["style"])
	}
	wantError(t, h.do(t, "PUT", "/api/profile/subtitle-style",
		map[string]any{"color": "#ffffff", "font": "sans", "size": 1, "position": 8}), 409, "no_account")
}
