package media

import (
	"regexp"
	"strings"
)

/*
 * reAlbumEdition matches a bracketed edition note at the end of an album
 * title: "(Deluxe)", "[Remastered 2011]", "(Censored Version)", "(feat. X)".
 *
 * Bracketed and at the end only. A bare word is part of a name ("Explicit
 * Content" is somebody's album), and a leading bracket is part of the title
 * itself ("(What's the Story) Morning Glory?"). "Live" is deliberately absent:
 * a live recording is a different record, not an edition of the studio one.
 */
var reAlbumEdition = regexp.MustCompile(`(?i)\s*[\(\[][^\(\)\[\]]*\b(` +
	`deluxe|edition|remaster|remastered|version|censored|explicit|clean|` +
	`expanded|anniversary|bonus|special|collector'?s|reissue|mono|stereo|` +
	`feat\.?|ft\.?|featuring` +
	`)\b[^\(\)\[\]]*[\)\]]\s*$`)

/*
 * AlbumBaseTitle is an album's title without its edition notes, for finding
 * the record it is an edition of (ADR 0115).
 *
 * A MusicBrainz release group holds every edition of a record, so the deluxe
 * one is found as the record. Notes are stripped from the end one at a time,
 * since a title can carry two ("… (Deluxe) [Remastered]"). It never strips a
 * title to nothing or to an article: then the brackets were the name.
 *
 * Matching only. The album keeps the title its tags give it.
 */
func AlbumBaseTitle(title string) string {
	s := strings.TrimSpace(title)
	for {
		loc := reAlbumEdition.FindStringIndex(s)
		if loc == nil {
			return s
		}
		out := strings.TrimSpace(s[:loc[0]])
		if out == "" || isArticleOnly(out) {
			return s
		}
		s = out
	}
}

// albumArtistSeparators are what joins an artist to a title in a tag that
// carries both: "AUDIOSLAVE - BEST COLLECTION". Spaced, so a hyphen inside a
// name ("Jay-Z", "AC-DC") never splits it.
var albumArtistSeparators = []string{" - ", " – ", " — "}

/*
 * AlbumSearchTitle is the title to look an album up by (ADR 0115): its own
 * title without the album artist in front of it, then without edition notes.
 *
 * Some rips tag the album as "Artist - Title". Searched whole, the artist's
 * name counts against the record's real title, and the right record scores
 * below the wrong ones (Audioslave's "Best Collection" at 0.50, behind three
 * other records). The prefix is dropped only when it is the album artist, by
 * the one normalizer, and never when that would leave nothing: a self-titled
 * "Weezer - Weezer" keeps "Weezer".
 *
 * Matching only, like AlbumBaseTitle. The album keeps the title its tags give.
 */
func AlbumSearchTitle(title, albumArtist string) string {
	s := strings.TrimSpace(title)
	artist := SortTitle(albumArtist)
	if artist != "" {
		for _, sep := range albumArtistSeparators {
			left, right, ok := strings.Cut(s, sep)
			if ok && SortTitle(left) == artist && strings.TrimSpace(right) != "" {
				s = strings.TrimSpace(right)
				break
			}
		}
	}
	return AlbumBaseTitle(s)
}
