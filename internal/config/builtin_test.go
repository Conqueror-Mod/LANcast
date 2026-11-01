package config

import "testing"

func withBuiltin(t *testing.T, tmdb, osub string) {
	t.Helper()
	prevT, prevO := BuiltinTMDBKey, BuiltinOpenSubtitlesKey
	BuiltinTMDBKey, BuiltinOpenSubtitlesKey = tmdb, osub
	t.Cleanup(func() { BuiltinTMDBKey, BuiltinOpenSubtitlesKey = prevT, prevO })
}

func TestKeyInUse(t *testing.T) {
	cases := []struct {
		name       string
		builtin    string
		s          Settings
		wantKey    string
		wantSource string
	}{
		{"a build with no key and none entered", "", Settings{}, "", KeySourceNone},
		{"a build with no key, one entered", "", Settings{TMDBKey: "mine"}, "mine", KeySourceOwn},
		{"the built-in key when none is entered", "shipped", Settings{}, "shipped", KeySourceBuiltIn},
		{"the person's key beats the built-in one", "shipped", Settings{TMDBKey: "mine"}, "mine", KeySourceOwn},
		{"switched off, nothing is used", "shipped", Settings{BuiltinKeysOff: true}, "", KeySourceNone},
		{"switched off, the person's key still is", "shipped", Settings{TMDBKey: "mine", BuiltinKeysOff: true}, "mine", KeySourceOwn},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withBuiltin(t, c.builtin, "")
			if got := c.s.TMDBKeyInUse(); got != c.wantKey {
				t.Errorf("TMDBKeyInUse = %q, want %q", got, c.wantKey)
			}
			if got := c.s.TMDBKeySource(); got != c.wantSource {
				t.Errorf("TMDBKeySource = %q, want %q", got, c.wantSource)
			}
		})
	}
}

func TestOpenSubtitlesKeyInUseFollowsTheSameRule(t *testing.T) {
	withBuiltin(t, "", "shipped")
	if got := (Settings{}).OpenSubtitlesKeyInUse(); got != "shipped" {
		t.Errorf("OpenSubtitlesKeyInUse = %q, want the built-in key", got)
	}
	if got := (Settings{OpenSubtitlesKey: "mine"}).OpenSubtitlesKeyInUse(); got != "mine" {
		t.Errorf("OpenSubtitlesKeyInUse = %q, want the person's own", got)
	}
	if got := (Settings{BuiltinKeysOff: true}).OpenSubtitlesKeySource(); got != KeySourceNone {
		t.Errorf("OpenSubtitlesKeySource = %q with the switch off, want none", got)
	}
}

// A plugin granted tmdb_key reads only a key the person entered (ADR 0110).
func TestBuiltinSecretNeverHandsOutTheShippedKey(t *testing.T) {
	withBuiltin(t, "shipped-tmdb", "shipped-osub")
	s := Settings{}
	for _, name := range []string{"tmdb_key", "opensubtitles_key"} {
		if got := s.BuiltinSecret(name); got != "" {
			t.Errorf("BuiltinSecret(%q) = %q, want empty: the shipped key is LANcast's own", name, got)
		}
	}
}
