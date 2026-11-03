package meta

import "testing"

/*
 * Album scoring against the cases the first measurement over a real library
 * got wrong or nearly wrong (ADR 0115). Each is a pair the library held.
 */

func albumState(q Query, c Candidate) (float64, string) {
	c.Kind = KindAlbum
	cands := []Candidate{c}
	Rank(q, cands)
	_, state := Decide(cands)
	return cands[0].Score, state
}

func TestAlbumScoringOnMeasuredCases(t *testing.T) {
	cases := []struct {
		name  string
		q     Query
		c     Candidate
		state string
	}{
		{"a prefix is not the same artist",
			AlbumQuery("Billy The Kit", "Billy the Kit", 2000),
			Candidate{ExternalID: "x", Title: "Billy The Demo", Artist: "Billy Joel"}, StateUnmatched},
		{"a label in the artist tag finds nothing",
			AlbumQuery("Panic! At The Disco", "Fueled By Ramen", 2015),
			Candidate{ExternalID: "x", Title: "Spread the Fire", Artist: "Fueled by Fire", Year: 2007}, StateUnmatched},
		{"the right artist's other record is not this one",
			AlbumQuery("夜更けと雨と Live at 日本武道館 2017.05.12", "シド", 2014),
			Candidate{ExternalID: "x", Title: "Shout at the Devil", Artist: "シド", Year: 2012}, StateUnmatched},
		{"Cyrillic agrees with itself",
			AlbumQuery("Страшно", "Shortparis", 2018),
			Candidate{ExternalID: "x", Title: "Страшно", Artist: "Shortparis", Year: 2018}, StateMatched},
		{"every word of the artist inside the credit",
			AlbumQuery("The Biz Never Sleeps", "Biz Markie", 1989),
			Candidate{ExternalID: "x", Title: "The Biz Never Sleeps", Artist: "The Diabolical Biz Markie", Year: 1989}, StateMatched},
		{"one name of a collaboration",
			AlbumQuery("Error 404", "Martin Garrix", 0),
			Candidate{ExternalID: "x", Title: "Error 404", Artist: "Martin Garrix & Jay Hardway",
				ArtistNames: []string{"Martin Garrix", "Jay Hardway"}, Year: 2013}, StateMatched},
		{"credited in another script, known by the tag's name",
			AlbumQuery("200 По Встречной", "t.A.T.u.", 2001),
			Candidate{ExternalID: "x", Title: "200 по встречной", Artist: "Тату",
				ArtistNames: []string{"Тату", "t.A.T.u."}, Year: 2001}, StateMatched},
		{"an edition finds its record",
			AlbumQuery("Fantasies: Expanded Edition", "Metric", 2011),
			Candidate{ExternalID: "x", Title: "Fantasies", Artist: "Metric", Year: 2009}, StateMatched},
		{"a near-identical spelling still agrees",
			AlbumQuery("Schuld und Sühne", "Blut Engel", 2001),
			Candidate{ExternalID: "x", Title: "Schuld und Sühne", Artist: "Blutengel", Year: 2001}, StateMatched},
	}
	for _, c := range cases {
		score, state := albumState(c.q, c.c)
		if state != c.state {
			t.Errorf("%s: %s at %.2f, want %s", c.name, state, score, c.state)
		}
	}
}
