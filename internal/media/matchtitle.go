package media

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

/*
 * MatchTitle is the form of a title that matching compares: SortTitle, then
 * accents folded, punctuation dropped and runs of space made one.
 *
 * It is built on SortTitle rather than beside it, so the two cannot disagree
 * about articles or case (CLAUDE.md, "One normalizer"), and it is separate
 * from SortTitle so that folding accents for matching does not move a single
 * tile in a sorted grid.
 *
 * Letters and digits are kept in every script. The matcher's old normalizer
 * kept only a to z and 0 to 9, so "七人の侍" and "Брат" became "" and scored
 * zero against their own exact titles, and "Léon" became "lon" (R08-3). Music
 * met the same fault first and fixed it in albumText (ADR 0115).
 *
 * Punctuation is dropped rather than turned into a space, so "Ocean's" is
 * "oceans" and not "ocean s", and "Spider-Man" is "spiderman".
 */
func MatchTitle(title string) string {
	var b strings.Builder
	space := false
	for _, r := range foldAccents(SortTitle(title)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.M, r):
			b.WriteRune(r)
			space = false
		case unicode.IsSpace(r):
			if !space && b.Len() > 0 {
				b.WriteRune(' ')
				space = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

/*
 * foldAccents takes the accents off Latin and Greek letters, so "Léon" is
 * "leon" and "Ταξίδι" is "ταξιδι", and leaves every other mark where it is.
 *
 * Only those two scripts, and that is the care in this function. Decomposing
 * and dropping every combining mark would also turn the Japanese "ガ" into
 * "カ" and strip the vowel signs a Devanagari word cannot be read without:
 * folding there loses the word, not a diacritic. A mark on any other base is
 * kept, and the string is recomposed, so those scripts come back as they went in.
 *
 * Greek's final sigma is folded to σ as well, since ToLower makes a capital Σ
 * at the end of a word σ, not ς, and the two spellings are one title.
 */
func foldAccents(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	var base rune
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) && (unicode.Is(unicode.Latin, base) || unicode.Is(unicode.Greek, base)) {
			continue
		}
		if !unicode.Is(unicode.M, r) {
			base = r
		}
		if r == 'ς' {
			r = 'σ'
		}
		b.WriteRune(r)
	}
	return norm.NFC.String(b.String())
}
