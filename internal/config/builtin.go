package config

/*
 * The provider keys a release build carries (ADR 0110).
 *
 * Set by the linker from repository secrets, never committed:
 *
 *	-X lancast/internal/config.BuiltinTMDBKey=…
 *	-X lancast/internal/config.BuiltinOpenSubtitlesKey=…
 *
 * Empty in any build from a checkout, which then behaves as LANcast always
 * has: no key until somebody enters one. Variables rather than constants
 * because -X can only set a string variable.
 */
var (
	BuiltinTMDBKey          string
	BuiltinOpenSubtitlesKey string
)

// Where the key a provider uses comes from, as Settings and the API report it.
const (
	KeySourceOwn     = "own"      // the person entered one
	KeySourceBuiltIn = "built_in" // the one this release carries
	KeySourceNone    = "none"     // neither, so the provider is off
)

/*
 * TMDBKeyInUse is the key the TMDB provider uses: the person's own if they set
 * one, else the built-in one unless they switched it off, else nothing.
 *
 * This and OpenSubtitlesKeyInUse are the only places that choice is made.
 * Every caller asks them, so a server can never report one key while
 * fetching with another.
 */
func (s Settings) TMDBKeyInUse() string {
	return keyInUse(s.TMDBKey, BuiltinTMDBKey, s.BuiltinKeysOff)
}

// OpenSubtitlesKeyInUse is TMDBKeyInUse for subtitle search.
func (s Settings) OpenSubtitlesKeyInUse() string {
	return keyInUse(s.OpenSubtitlesKey, BuiltinOpenSubtitlesKey, s.BuiltinKeysOff)
}

// TMDBKeySource says which key TMDBKeyInUse picked.
func (s Settings) TMDBKeySource() string {
	return keySource(s.TMDBKey, BuiltinTMDBKey, s.BuiltinKeysOff)
}

// OpenSubtitlesKeySource says which key OpenSubtitlesKeyInUse picked.
func (s Settings) OpenSubtitlesKeySource() string {
	return keySource(s.OpenSubtitlesKey, BuiltinOpenSubtitlesKey, s.BuiltinKeysOff)
}

func keyInUse(own, builtin string, off bool) string {
	switch keySource(own, builtin, off) {
	case KeySourceOwn:
		return own
	case KeySourceBuiltIn:
		return builtin
	}
	return ""
}

func keySource(own, builtin string, off bool) string {
	switch {
	case own != "":
		return KeySourceOwn
	case builtin != "" && !off:
		return KeySourceBuiltIn
	}
	return KeySourceNone
}
