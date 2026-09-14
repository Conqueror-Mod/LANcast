/*
Package rating ranks content ratings, so that one can be compared with another.

A ceiling set for an account is worth nothing without an ordering, and there is
no ordering in the data: `content_rating` is whatever string a provider or an
NFO wrote, drawn from a dozen national systems that do not agree with each
other. "15" and "R" and "TV-MA" all mean roughly the same thing and none of
them sorts next to the others.

So this maps the labels actually seen in libraries onto a small ladder of
**ages**, which is the one thing every system is really saying. A rung is an
age in years, not a position in a list, because that is what makes two systems
comparable at all: BBFC 15 and TV-14 land one year apart, which is the truth,
where "the fifth one" and "the fourth one" would be an accident of how long
each list happens to be.

Three rules govern everything here, and they are the whole design:

**The scale is deliberately coarse.** It is not a classification authority and
must never be read as one. It exists to answer one question — is this label at
or under that label — for a household that has set a limit for a child's
account.

**An unknown label is not treated as permissive.** A ceiling that let through
everything it did not recognise would be a ceiling with a hole in it exactly
where the unlabelled and the foreign-rated sit, which in a real library is a
great deal of the catalogue. Unknown reports itself as unknown, and the caller
decides — see Allowed, which blocks it.

**Nothing here reads the database or a request.** It is a pure lookup with a
table, which is what lets every one of these cases be a test.
*/
package rating

import "strings"

// Unknown is the rank of a label this package does not recognise, and of an
// item carrying no rating at all. It is deliberately distinct from every real
// age so that a caller cannot accidentally treat it as "suitable for
// everybody" by comparing numbers.
const Unknown = -1

/*
 * The ladder.
 *
 * Ages rather than list positions, so that systems of different lengths line
 * up: the US has five film certificates and the UK has six, and pairing them
 * by index would put R beside 15 in one direction and 18 in the other
 * depending on which list was counted.
 *
 * The US television ratings sit beside the film ones on the same ladder for
 * the same reason. A household setting a limit does not hold two opinions, one
 * for films and one for episodes, and a ceiling that governed only half a
 * library would be the kind of gap somebody finds by accident.
 */
var ages = map[string]int{
	// United States — film (MPA).
	"G":     0,
	"PG":    8,
	"PG-13": 13,
	"R":     17,
	"NC-17": 18,
	// United States — television.
	"TV-Y":  0,
	"TV-Y7": 7,
	"TV-G":  0,
	"TV-PG": 8,
	"TV-14": 14,
	"TV-MA": 17,
	// United Kingdom (BBFC). "12A" is a cinema certificate that appears on
	// library items anyway, and it means 12 accompanied — treated as 12, since
	// the accompaniment is not something a media server can know about.
	"U":   0,
	"UC":  0,
	"12":  12,
	"12A": 12,
	"15":  15,
	"18":  18,
	// Ireland, Germany, the Netherlands and Australia contribute labels that
	// turn up in provider data often enough to be worth naming rather than
	// leaving to the unknown rule.
	"FSK 0":  0,
	"FSK 6":  6,
	"FSK 12": 12,
	"FSK 16": 16,
	"FSK 18": 18,
	"AL":     0,
	"6":      6,
	"9":      9,
	"16":     16,
	"M":      15,
	"MA15+":  15,
	"R18+":   18,
	"X18+":   18,
	// Explicitly unrated labels. Named so that "NR" is recognised as *saying*
	// nothing rather than as unrecognised — the rank is the same, and the
	// distinction is worth keeping in the table for anybody reading it.
	"NR":        Unknown,
	"UNRATED":   Unknown,
	"NOT RATED": Unknown,
}

/*
 * Rank returns the minimum age a label implies, or Unknown.
 *
 * Case and surrounding space are forgiving because the label comes from
 * provider data and from NFO files people wrote by hand, where "tv-ma" and
 * "TV-MA " are the same statement. A prefix like "US:" or "GB:" is dropped for
 * the same reason: several providers qualify the certificate with the country
 * that issued it, and the qualifier does not change what it means.
 */
func Rank(label string) int {
	key := strings.ToUpper(strings.TrimSpace(label))
	if key == "" {
		return Unknown
	}
	// "US:PG-13" and "DE:FSK 16" — the country says where it was issued, which
	// is not part of the certificate.
	if i := strings.LastIndex(key, ":"); i >= 0 {
		key = strings.TrimSpace(key[i+1:])
	}
	// "Rated R" is what a couple of sources write.
	key = strings.TrimPrefix(key, "RATED ")
	if age, ok := ages[key]; ok {
		return age
	}
	return Unknown
}

// Known reports whether a label is one this package can place on the ladder.
// The settings screen offers only these, so nobody can set a ceiling that
// silently means nothing.
func Known(label string) bool {
	return Rank(label) != Unknown
}

/*
 * Allowed reports whether an item carrying `item` may be shown under `ceiling`.
 *
 * Two decisions are recorded here rather than left to each caller, because a
 * rule enforced in four places with three interpretations is not a rule.
 *
 * **An empty ceiling allows everything.** No limit is the default and the
 * ordinary case; an account without one is not a restricted account.
 *
 * **An unrated item is blocked by any ceiling.** This is the uncomfortable half
 * and it is deliberate. The alternative — letting through everything carrying
 * no label — puts the hole precisely where the unlabelled sits, and in a real
 * library that is home video, anything a provider never matched, and most of
 * what somebody added by hand. A limit that stops at the catalogued and waves
 * the rest past is not a limit; it is a filter that looks like one. The cost is
 * that a restricted account sees less than the household might expect, which is
 * visible, complainable-about, and fixable by rating the item. The other
 * failure is invisible.
 */
func Allowed(itemRating, ceiling string) bool {
	limit := Rank(ceiling)
	if limit == Unknown {
		return true
	}
	age := Rank(itemRating)
	if age == Unknown {
		return false
	}
	return age <= limit
}

/*
 * AllowedLabels returns every label at or under a ceiling.
 *
 * This is what turns the rule into a database query: the store filters on an
 * exact set of strings it already indexes, rather than growing an opinion about
 * what a rating means. The ordering of the result is not meaningful and callers
 * must not depend on it — it feeds an `IN` clause.
 */
func AllowedLabels(ceiling string) []string {
	limit := Rank(ceiling)
	if limit == Unknown {
		return nil
	}
	out := make([]string, 0, len(ages))
	for label, age := range ages {
		if age != Unknown && age <= limit {
			out = append(out, label)
		}
	}
	return out
}
