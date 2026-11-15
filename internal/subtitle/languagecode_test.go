package subtitle

import "testing"

// LanguageCode guards a value that becomes part of a file name (R07-1): the
// table's spellings and plain language tags pass, and nothing that can name a
// folder does.
func TestLanguageCode(t *testing.T) {
	for in, want := range map[string]string{
		"en": "en", "English": "en", " ENG ": "en", "fre": "fr",
		"pt-br": "pt-br", "PT-BR": "pt-br", "zh-hant": "zh-hant", "haw": "haw",
	} {
		if got, ok := LanguageCode(in); !ok || got != want {
			t.Errorf("LanguageCode(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
	for _, in := range []string{
		"", "x/../../escaped", `x\..\escaped`, "..", ".", "en.srt", "c:",
		"e", "abcd", "en-", "en-us-x", "en_us", "en/us", "english language",
		"en-toolongsubtag",
	} {
		if got, ok := LanguageCode(in); ok {
			t.Errorf("LanguageCode(%q) = %q, true; want refused", in, got)
		}
	}
}
