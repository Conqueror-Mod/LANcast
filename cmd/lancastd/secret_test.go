package main

import (
	"testing"

	"lancast/internal/config"
)

/*
 * Which value a plugin reads, and in which order.
 *
 * Before this existed the host resolved from a fixed list of first-party names
 * and answered "" for everything else — so a third-party plugin could be
 * granted a secret, be shown as granted, and read nothing for ever, while the
 * approval dialog told the operator it would read a key it never could.
 */
func TestResolveSecret(t *testing.T) {
	settings := config.Settings{OMDbKey: "server-omdb", TMDBKey: "server-tmdb"}

	cases := []struct {
		name   string
		stored string
		secret string
		want   string
		why    string
	}{
		{
			name: "a plugin's own credential", stored: "plugin-key", secret: "example_key",
			want: "plugin-key",
			why:  "the whole point: a name the server knows nothing about now resolves",
		},
		{
			name: "granted but never given a value", stored: "", secret: "example_key",
			want: "",
			why:  "empty, and secrets_configured is what tells the operator so",
		},
		{
			name: "a server key, with nothing stored", stored: "", secret: "omdb_key",
			want: "server-omdb",
			why:  "the built-in plugins must keep working untouched",
		},
		{
			name: "a stored value beats the server's", stored: "plugin-omdb", secret: "omdb_key",
			want: "plugin-omdb",
			why:  "most specific wins: it was stored for this plugin",
		},
		{
			name: "a server key that is not configured", stored: "", secret: "opensubtitles_key",
			want: "",
			why:  "a name the server knows but has no value for is still empty",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveSecret(tc.stored, settings, tc.secret); got != tc.want {
				t.Errorf("resolveSecret(%q, …, %q) = %q, want %q — %s",
					tc.stored, tc.secret, got, tc.want, tc.why)
			}
		})
	}
}
