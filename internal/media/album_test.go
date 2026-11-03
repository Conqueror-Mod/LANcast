package media

import "testing"

// Titles from the real library's unmatched albums (ADR 0115 measurement),
// and the ones that must survive untouched.
func TestAlbumBaseTitle(t *testing.T) {
	cases := map[string]string{
		"> album title goes here < (Deluxe)":               "> album title goes here <",
		"2001 (censored version)":                          "2001",
		"Nevermind (Remastered)":                           "Nevermind",
		"Abbey Road [2019 Remaster]":                       "Abbey Road",
		"Thriller (25th Anniversary Edition) [Remastered]": "Thriller",
		"'VIEWS (feat. ROUSSO)'":                           "'VIEWS (feat. ROUSSO)'", // the quote closes after the bracket
		"Lose Yourself (feat. Someone)":                    "Lose Yourself",
		"(What's the Story) Morning Glory?":                "(What's the Story) Morning Glory?",
		"Alive! (Live)":                                    "Alive! (Live)",
		"Explicit Content":                                 "Explicit Content",
		"Greatest Hits (Vol. 2)":                           "Greatest Hits (Vol. 2)",
		"(Deluxe)":                                         "(Deluxe)",
		"The (Special Edition)":                            "The (Special Edition)",
		"  Hello Nasty  ":                                  "Hello Nasty",
	}
	for in, want := range cases {
		if got := AlbumBaseTitle(in); got != want {
			t.Errorf("AlbumBaseTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
