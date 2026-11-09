package meta

import (
	"context"
	"strings"
)

/*
 * The languages metadata may be fetched in (ADR 0122).
 *
 * A list rather than any tag, for the reason certification countries are a
 * list: a value the server will store but a provider will not honour is a
 * setting somebody chooses, watches change nothing, and reports as broken.
 * These are tags TMDB accepts, each with its region, because TMDB treats
 * `pt-BR` and `pt-PT` as different translations and the person choosing
 * knows which one they mean.
 *
 * Named in English and in the language itself, so the list can be read by
 * whoever is choosing from it.
 */

// Language is one metadata language: its tag and how to name it.
type Language struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Native string `json:"native"`
}

// DefaultLanguage is what metadata is fetched in when nothing is chosen.
const DefaultLanguage = "en-US"

// Languages is every language that may be chosen, English first, then by
// English name.
var Languages = []Language{
	{"en-US", "English (United States)", "English (US)"},
	{"en-GB", "English (United Kingdom)", "English (UK)"},
	{"ar-SA", "Arabic", "العربية"},
	{"zh-CN", "Chinese (Simplified)", "简体中文"},
	{"zh-TW", "Chinese (Traditional)", "繁體中文"},
	{"cs-CZ", "Czech", "Čeština"},
	{"da-DK", "Danish", "Dansk"},
	{"nl-NL", "Dutch", "Nederlands"},
	{"fi-FI", "Finnish", "Suomi"},
	{"fr-FR", "French (France)", "Français (France)"},
	{"fr-CA", "French (Canada)", "Français (Canada)"},
	{"de-DE", "German", "Deutsch"},
	{"el-GR", "Greek", "Ελληνικά"},
	{"he-IL", "Hebrew", "עברית"},
	{"hi-IN", "Hindi", "हिन्दी"},
	{"hu-HU", "Hungarian", "Magyar"},
	{"id-ID", "Indonesian", "Bahasa Indonesia"},
	{"it-IT", "Italian", "Italiano"},
	{"ja-JP", "Japanese", "日本語"},
	{"ko-KR", "Korean", "한국어"},
	{"nb-NO", "Norwegian", "Norsk bokmål"},
	{"pl-PL", "Polish", "Polski"},
	{"pt-BR", "Portuguese (Brazil)", "Português (Brasil)"},
	{"pt-PT", "Portuguese (Portugal)", "Português (Portugal)"},
	{"ro-RO", "Romanian", "Română"},
	{"ru-RU", "Russian", "Русский"},
	{"es-ES", "Spanish (Spain)", "Español (España)"},
	{"es-MX", "Spanish (Mexico)", "Español (México)"},
	{"sv-SE", "Swedish", "Svenska"},
	{"th-TH", "Thai", "ไทย"},
	{"tr-TR", "Turkish", "Türkçe"},
	{"uk-UA", "Ukrainian", "Українська"},
	{"vi-VN", "Vietnamese", "Tiếng Việt"},
}

// KnownLanguage reports whether code is one of Languages, exactly.
func KnownLanguage(code string) bool {
	for _, l := range Languages {
		if l.Code == code {
			return true
		}
	}
	return false
}

// LanguageOrDefault is code when it is known, else DefaultLanguage: a stored
// value this build no longer offers fetches in English rather than in
// something nobody chose.
func LanguageOrDefault(code string) string {
	if KnownLanguage(code) {
		return code
	}
	return DefaultLanguage
}

// IsEnglish reports whether a language tag is English, in any region.
func IsEnglish(code string) bool {
	return code == "" || strings.HasPrefix(strings.ToLower(code), "en")
}

/*
 * One fetch's language, carried on its context (ADR 0123).
 *
 * A library may be fetched in a language of its own. The caller that knows
 * which item it is fetching for — the enrichment worker, Fix match, a trailer
 * lookup — places the library's language here, and a provider whose answers
 * depend on the language reads it before its own default. The Provider
 * interface is unchanged, so a provider with no languages never hears of them.
 */
type languageKey struct{}

// WithLanguage returns ctx carrying code as the language to fetch in. An
// empty code returns ctx unchanged, so "follow the server" needs no branch at
// the call site.
func WithLanguage(ctx context.Context, code string) context.Context {
	if code == "" {
		return ctx
	}
	return context.WithValue(ctx, languageKey{}, code)
}

// LanguageFrom is the language ctx carries, or "" when it carries none.
func LanguageFrom(ctx context.Context) string {
	code, _ := ctx.Value(languageKey{}).(string)
	return code
}
