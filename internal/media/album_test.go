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

// From the test library's review band: a tag that names the artist inside the
// album title. And the names a careless strip would break.
func TestAlbumSearchTitle(t *testing.T) {
	cases := []struct{ title, artist, want string }{
		{"AUDIOSLAVE - BEST COLLECTION", "Audioslave", "BEST COLLECTION"},
		{"The Beatles – Abbey Road (Remastered)", "The Beatles", "Abbey Road"},
		{"Beatles - Abbey Road", "The Beatles", "Abbey Road"}, // articles normalize away
		{"Weezer - Weezer", "Weezer", "Weezer"},
		{"Weezer -", "Weezer", "Weezer -"}, // nothing after it: the title is what it is
		{"Live - Throwing Copper", "Live", "Throwing Copper"},
		{"Jay-Z - The Blueprint", "Jay-Z", "The Blueprint"},
		{"Jay-Z & Linkin Park - Collision Course", "Jay-Z", "Jay-Z & Linkin Park - Collision Course"},
		{"Side A - Side B", "Somebody", "Side A - Side B"}, // not the artist: part of the name
		{"Hello Nasty", "", "Hello Nasty"},
	}
	for _, c := range cases {
		if got := AlbumSearchTitle(c.title, c.artist); got != c.want {
			t.Errorf("AlbumSearchTitle(%q, %q) = %q, want %q", c.title, c.artist, got, c.want)
		}
	}
}
