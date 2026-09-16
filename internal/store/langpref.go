package store

import (
	"context"
	"fmt"
	"strings"
)

/*
 * Which language an account wants to hear, and when it wants subtitles.
 *
 * Set by the account itself, which is the opposite of the content-rating
 * ceiling beside it in the same table: a ceiling is a limit somebody else
 * decides, and a language is a taste. Nothing here checks the caller's role,
 * and the API route is the one that acts on the session rather than on a named
 * user.
 */

/*
 * SubtitleMode is when to show subtitles at all.
 *
 * Three states rather than a boolean, because "on" is two different wishes. A
 * person who wants English audio usually wants no subtitles — until the film
 * has no English track, when they want them without having to ask. That is
 * SubtitleForeign, and it is the one most people mean by "on".
 */
const (
	// SubtitleOff never turns them on. The behaviour of every account that has
	// never set this, which is why it is also what empty means.
	SubtitleOff = "off"
	/*
	 * SubtitleForeign shows them when the audio that ends up playing is not in
	 * the preferred language.
	 *
	 * Judged on what *plays*, not on what the file contains: a film with an
	 * English track that was chosen needs no subtitles, and the same film when
	 * the track is missing does. Deciding from the file alone would put
	 * subtitles over every foreign film even when its English dub is playing.
	 */
	SubtitleForeign = "foreign"
	// SubtitleAlways shows them whenever a track exists in the preferred
	// language. For a household that watches with subtitles on principle.
	SubtitleAlways = "always"
)

// KnownSubtitleMode reports whether a mode is one this server acts on. Empty is
// accepted and means off.
func KnownSubtitleMode(mode string) bool {
	switch mode {
	case "", SubtitleOff, SubtitleForeign, SubtitleAlways:
		return true
	}
	return false
}

/*
 * NormalizeLang canonicalises a language code, or returns false.
 *
 * Two or three letters, lower-cased. ISO 639 has both — `en` and `eng` name the
 * same language — and media files use both spellings depending on the muxer, so
 * neither can be refused. What matters is that the stored value and the value
 * read off a stream are compared by one rule, which is what LangMatches is for.
 *
 * Deliberately not a list of every ISO code. A closed list would have to be
 * maintained, and being wrong about it means refusing somebody's language for
 * no reason — where the cost of accepting an unknown code is a preference that
 * simply never matches anything, which is the same as having none.
 */
func NormalizeLang(code string) (string, bool) {
	c := strings.ToLower(strings.TrimSpace(code))
	if c == "" {
		return "", true // no preference, which is a valid answer
	}
	if len(c) < 2 || len(c) > 3 {
		return "", false
	}
	for _, r := range c {
		if r < 'a' || r > 'z' {
			return "", false
		}
	}
	return c, true
}

/*
 * LangMatches reports whether a stream's language is the one wanted.
 *
 * The comparison that makes the whole feature work, and the reason it is one
 * function rather than an `==` at each call site: a file says `eng`, a person
 * picks `en`, and a muxer somewhere says `en-US`. All three are English, and a
 * preference that matched only its own spelling would look broken on half a
 * library.
 *
 * So both sides are cut at the first separator and compared on their first two
 * letters — which is the whole of ISO 639-1 and the distinguishing part of
 * 639-2 for every language a media file is likely to carry. `por` and `pt`
 * agree; `pt-BR` and `pt-PT` also agree, which is deliberate: somebody who
 * asked for Portuguese would rather hear Brazilian Portuguese than nothing.
 */
func LangMatches(want, got string) bool {
	w, g := langKey(want), langKey(got)
	return w != "" && w == g
}

func langKey(code string) string {
	c := strings.ToLower(strings.TrimSpace(code))
	// `en-US`, `pt_BR`, and the `eng` an ffprobe stream carries.
	if i := strings.IndexAny(c, "-_"); i > 0 {
		c = c[:i]
	}
	/*
	 * `und` is ffprobe's "undetermined", and it is not a language. Treating it
	 * as one would make it match another `und` and pick an arbitrary track as
	 * though it were a deliberate answer.
	 */
	if c == "" || c == "und" {
		return ""
	}
	if len(c) > 2 {
		c = c[:2]
	}
	return c
}

/*
 * SetLanguagePreferences records what an account wants to hear and read.
 *
 * All three together rather than three setters, so the page cannot half-apply a
 * change — the same reasoning Prefs.Set and lancastDesktopSet use. A subtitle
 * language with no mode, or a mode with no language, are both states somebody
 * could otherwise be left in by a failed second request.
 */
func (s *Store) SetLanguagePreferences(ctx context.Context, userID, audio, subtitle, mode string) error {
	a, ok := NormalizeLang(audio)
	if !ok {
		return fmt.Errorf("set language preferences: %q is not a language code", audio)
	}
	sub, ok := NormalizeLang(subtitle)
	if !ok {
		return fmt.Errorf("set language preferences: %q is not a language code", subtitle)
	}
	if !KnownSubtitleMode(mode) {
		return fmt.Errorf("set language preferences: %q is not a subtitle mode", mode)
	}
	if mode == SubtitleOff {
		// Stored as empty so that "off" and "never set" are one state in the
		// database. Two spellings of one answer is how a default drifts.
		mode = ""
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE user
		   SET preferred_audio_lang = ?, preferred_subtitle_lang = ?, subtitle_mode = ?
		 WHERE id = ?`, a, sub, mode, userID)
	if err != nil {
		return fmt.Errorf("set language preferences: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
