package tmdb

import "testing"

/*
 * A character name that is only a number is not a character name.
 *
 * Found by looking at a real detail page: War Machine (2026) came back from
 * TMDB with characters "81", "7", "15", "60", "109", "96" and "122" beside four
 * properly named ones, and those digits rendered under the actors' faces. The
 * data is TMDB's, not LANcast's — but it reads as LANcast leaking an internal
 * id, and a user cannot tell the difference between a provider's bad row and
 * ours. They will report ours.
 */
func TestCharacterOf(t *testing.T) {
	for _, tc := range []struct {
		in, want, why string
	}{
		{"81", "", "the reported case: a bare number is not a character"},
		{"122", "", "and a longer one"},
		{" 7 ", "", "surrounding space does not make it a name"},
		{"", "", "nothing stays nothing"},
		{"   ", "", "and so does whitespace"},

		// The other half, and the reason this checks digits *only* rather than
		// anything containing one. These are real character names.
		{"Agent 47", "Agent 47", "a number inside a name is part of the name"},
		{"Army Sgt Maj Sheridan", "Army Sgt Maj Sheridan", "an ordinary name"},
		{"K-2SO", "K-2SO", "digits and letters"},
		{"Apollo 13", "Apollo 13", "a number at the end of a real name"},
		{"7 of Nine", "7 of Nine", "a number at the start of a real name"},
		{"Herself", "Herself", "the documentary case"},
	} {
		if got := characterOf(tc.in); got != tc.want {
			t.Errorf("characterOf(%q) = %q, want %q — %s", tc.in, got, tc.want, tc.why)
		}
	}
}
