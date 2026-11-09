package api

import (
	"testing"

	"lancast/internal/meta"
)

/*
 * The metadata language setting (ADR 0122): the list is served, only a
 * language in it is stored, and the default reads back as en-US whether it
 * was never set or set to en-US.
 */

func TestTheOfferedLanguagesAreServed(t *testing.T) {
	h := newHarness(t)
	var body map[string]any
	decode(t, h.do(t, "GET", "/api/settings", nil), &body)

	list, ok := body["metadata_languages"].([]any)
	if !ok || len(list) != len(meta.Languages) {
		t.Fatalf("metadata_languages = %#v, want the %d languages", body["metadata_languages"], len(meta.Languages))
	}
	for _, entry := range list {
		l, _ := entry.(map[string]any)
		code, _ := l["code"].(string)
		name, _ := l["name"].(string)
		native, _ := l["native"].(string)
		if !meta.KnownLanguage(code) || name == "" || native == "" {
			t.Errorf("language %#v is incomplete", l)
		}
	}
	if got := body["metadata_language"]; got != "en-US" {
		t.Errorf("metadata_language = %#v, want en-US by default", got)
	}
}

func TestALanguageNotOfferedIsRefused(t *testing.T) {
	h := newHarness(t)
	for _, code := range []string{"de", "DE-de", "xx-YY", "klingon"} {
		wantError(t, h.do(t, "PUT", "/api/settings",
			map[string]any{"metadata_language": code}), 400, "bad_request")
	}
}

func TestChoosingALanguageIsStoredAndReadBack(t *testing.T) {
	h := newHarness(t)
	h.do(t, "PUT", "/api/settings", map[string]any{"metadata_language": "de-DE"})
	var body map[string]any
	decode(t, h.do(t, "GET", "/api/settings", nil), &body)
	if got := body["metadata_language"]; got != "de-DE" {
		t.Errorf("metadata_language = %#v, want de-DE", got)
	}
	// Back to English, which is stored as the default.
	h.do(t, "PUT", "/api/settings", map[string]any{"metadata_language": "en-US"})
	decode(t, h.do(t, "GET", "/api/settings", nil), &body)
	if got := body["metadata_language"]; got != "en-US" {
		t.Errorf("metadata_language = %#v, want en-US", got)
	}
	if h.settings.Get().MetadataLanguage != "" {
		t.Errorf("en-US was stored as %q, want the default \"\"", h.settings.Get().MetadataLanguage)
	}
}
