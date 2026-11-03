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
