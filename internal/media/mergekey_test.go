package media

import "testing"

/*
 * Every pair here came off a real music library, where each produced two
 * artists in the grid — each holding some of the records, neither showing the
 * whole discography.
 */
func TestMergeKeyFoldsTagSpellings(t *testing.T) {
	for _, tt := range []struct{ a, b, why string }{
		{"t.A.T.u", "t.A.T.u.", "a trailing full stop"},
		{"Blut Engel", "Blutengel", "a space that should not be there"},
		{"Box Car Racer", "Boxcar Racer", "the same"},
		{"alt-J", "alt‐J", "U+002D against U+2010 — visually identical"},
		{"9VoltRevolt", "9voltRevolt", "case, the first form of this trap"},
		{"AC/DC", "AC-DC", "a separator nobody means as identity"},
	} {
		t.Run(tt.a+" = "+tt.b, func(t *testing.T) {
			if MergeKey(tt.a) != MergeKey(tt.b) {
				t.Errorf("%q and %q key differently (%q vs %q) — %s",
					tt.a, tt.b, MergeKey(tt.a), MergeKey(tt.b), tt.why)
			}
		})
	}
}

// Different acts stay different. The key strips punctuation, not meaning.
func TestMergeKeyKeepsDifferentActsApart(t *testing.T) {
	for _, tt := range [][2]string{
		{"Weezer", "Wheezer"},
		{"The Doors", "The Doobies"},
		{"Sublime", "Sublime with Rome"},
		{"Nirvana", "Nirvana UK"},
		{"3 Doors Down", "3 Days Grace"},
	} {
		if MergeKey(tt[0]) == MergeKey(tt[1]) {
			t.Errorf("%q and %q collapsed into one key %q", tt[0], tt[1], MergeKey(tt[0]))
		}
	}
}

/*
 * Not SortTitle, and this is the case that says why.
 *
 * SortTitle drops leading articles, which is right for ordering a shelf and
 * wrong for identity: a band called "The The" would key as "the", and every
 * other band whose name is an article away from another would join it.
 */
func TestMergeKeyDoesNotDropArticles(t *testing.T) {
	if MergeKey("The The") == MergeKey("The") {
		t.Error(`"The The" keyed the same as "The"`)
	}
	if got := MergeKey("The Doors"); got != "thedoors" {
		t.Errorf("MergeKey(\"The Doors\") = %q, want thedoors — the article is kept", got)
	}
}

// A name with nothing to key on yields an empty string rather than something
// invented, so the caller can decide what that means.
func TestMergeKeyOfNothing(t *testing.T) {
	for _, s := range []string{"", "  ", "...", "—"} {
		if got := MergeKey(s); got != "" {
			t.Errorf("MergeKey(%q) = %q, want empty", s, got)
		}
	}
}
