package media

import (
	"strings"
	"unicode"
)

/*
 * MergeKey reduces a name to the form used for deciding whether two spellings
 * are the same thing.
 *
 * Music metadata comes from tags typed by many different people over many
 * years, and the same act arrives spelled several ways. From a real library:
 *
 *	"t.A.T.u"      and "t.A.T.u."       — a trailing full stop
 *	"Blut Engel"   and "Blutengel"      — a space that should not be there
 *	"Box Car Racer" and "Boxcar Racer"  — the same
 *	"alt-J"        and "alt‐J"          — U+002D against U+2010, indistinguishable on screen
 *
 * Each pair produced two artists in the grid, each holding some of the records,
 * neither showing the whole discography. The last pair is the one that makes the
 * case: the two strings are *visually identical* and no amount of care while
 * tagging would catch it.
 *
 * Letters and digits only, folded to lower case. Everything else goes: spaces,
 * punctuation, and the several Unicode characters that render as a hyphen.
 *
 * **Not SortTitle.** That drops leading articles, which is right for ordering a
 * shelf and wrong for identity — an artist called "The The" must not key as
 * "the", and "The Doors" and "Doors" being one act is a judgement about
 * *sorting*, not about who made the record. This is deliberately a third thing
 * from `clean` (display) and `SortTitle` (ordering), and it is here rather than
 * beside its caller so that a fourth one does not get written in the scanner.
 */
func MergeKey(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case unicode.IsLetter(r):
			b.WriteRune(unicode.ToLower(r))
		case unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}
