package meta

import "testing"

func cand(title string, year int, pop float64) Candidate {
	return Candidate{Kind: KindMovie, Title: title, Year: year, Popularity: pop}
}

// The suite that encodes what "correct" means for the feature most likely to
// disappoint. Every case here is a real-world confusion.
func TestScoreDisambiguation(t *testing.T) {
	tests := []struct {
		name   string
		query  Query
		right  Candidate
		wrong  Candidate
		strict bool // right must also clear the auto-accept threshold
	}{
		{
			name:   "Blade Runner 2049 is not Blade Runner",
			query:  Query{Kind: KindMovie, Title: "Blade Runner 2049", Year: 2017},
			right:  cand("Blade Runner 2049", 2017, 40),
			wrong:  cand("Blade Runner", 1982, 60),
			strict: true,
		},
		{
			name:   "The Thing 1982 is not the 2011 remake",
			query:  Query{Kind: KindMovie, Title: "The Thing", Year: 1982},
			right:  cand("The Thing", 1982, 30),
			wrong:  cand("The Thing", 2011, 35),
			strict: true,
		},
		{
			name:   "Ocean's Eleven 2001 is not Ocean's 11 from 1960",
			query:  Query{Kind: KindMovie, Title: "Ocean's Eleven", Year: 2001},
			right:  cand("Ocean's Eleven", 2001, 50),
			wrong:  cand("Ocean's 11", 1960, 10),
			strict: true,
		},
		{
			name:   "Dune 2021 is not Dune 1984",
			query:  Query{Kind: KindMovie, Title: "Dune", Year: 2021},
			right:  cand("Dune", 2021, 90),
			wrong:  cand("Dune", 1984, 25),
			strict: true,
		},
		{
			name:   "Dune 1984 is not Dune 2021 even though 2021 is popular",
			query:  Query{Kind: KindMovie, Title: "Dune", Year: 1984},
			right:  cand("Dune", 1984, 25),
			wrong:  cand("Dune", 2021, 90),
			strict: true,
		},
		{
			name:  "popularity must not rescue a weak title match",
			query: Query{Kind: KindMovie, Title: "Obscure Documentary", Year: 2014},
			right: cand("Obscure Documentary", 2014, 1),
			wrong: cand("Avengers Endgame", 2019, 500),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs, ws := Score(tt.query, tt.right), Score(tt.query, tt.wrong)
			if rs <= ws {
				t.Errorf("right %.3f <= wrong %.3f — wrong candidate would win", rs, ws)
			}
			if tt.strict && rs < ThresholdAuto {
				t.Errorf("right scored %.3f, below auto threshold %.2f", rs, ThresholdAuto)
			}
			if ws >= ThresholdAuto {
				t.Errorf("wrong scored %.3f, at or above auto threshold — it would apply silently", ws)
			}
		})
	}
}

// A file with no parseable year must not be confidently guessed. Queuing it for
// review is the correct outcome; picking the most popular title is not.
func TestUnyearedAmbiguousTitleIsNotAutoAccepted(t *testing.T) {
	q := Query{Kind: KindMovie, Title: "Solaris"}
	for _, c := range []Candidate{cand("Solaris", 1972, 12), cand("Solaris", 2002, 18)} {
		if s := Score(q, c); s >= ThresholdAuto {
			t.Errorf("Solaris (%d) scored %.3f — would auto-accept despite genuine ambiguity", c.Year, s)
		}
	}
}

func TestGenericNameScoresBelowThreshold(t *testing.T) {
	q := Query{Kind: KindMovie, Title: "untitled 1"}
	if s := Score(q, cand("The Shawshank Redemption", 1994, 300)); s >= ThresholdReview {
		t.Errorf("score %.3f, want below review threshold %.2f", s, ThresholdReview)
	}
}

func TestExactMatchScoresHigh(t *testing.T) {
	q := Query{Kind: KindMovie, Title: "Arrival", Year: 2016}
	if s := Score(q, cand("Arrival", 2016, 40)); s < 0.9 {
		t.Errorf("exact title and year scored %.3f, want >= 0.9", s)
	}
}

func TestStateFor(t *testing.T) {
	tests := []struct {
		score float64
		want  string
	}{
		{0.95, StateMatched},
		{0.85, StateMatched},
		{0.84, StateReview},
		{0.55, StateReview},
		{0.54, StateUnmatched},
		{0.0, StateUnmatched},
	}
	for _, tt := range tests {
		if got := StateFor(tt.score); got != tt.want {
			t.Errorf("StateFor(%.2f) = %q, want %q", tt.score, got, tt.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"The Matrix", "matrix"},
		{"Ocean's Eleven", "oceans eleven"},
		{"Spider-Man: No Way Home", "spiderman no way home"},
		{"WALL·E", "walle"},
		// "A." is not the article "a ", so nothing is stripped here.
		{"A.I. Artificial Intelligence", "ai artificial intelligence"},
	}
	for _, tt := range tests {
		if got := normalize(tt.in); got != tt.want {
			t.Errorf("normalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPunctuationInsensitivity(t *testing.T) {
	q := Query{Kind: KindMovie, Title: "Oceans Eleven", Year: 2001}
	if s := Score(q, cand("Ocean's Eleven", 2001, 40)); s < ThresholdAuto {
		t.Errorf("score %.3f — an apostrophe should not block an auto match", s)
	}
}

func TestYearScore(t *testing.T) {
	tests := []struct {
		want, got int
		expect    float64
	}{
		{2017, 2017, 1.0},
		{2017, 2018, 0.8},
		{2017, 2019, 0.25},
		{2017, 1982, 0},
		{0, 2017, 0.5},
	}
	for _, tt := range tests {
		if got := yearScore(tt.want, tt.got); got != tt.expect {
			t.Errorf("yearScore(%d,%d) = %.2f, want %.2f", tt.want, tt.got, got, tt.expect)
		}
	}
}

func TestPopularityHasDiminishingReturns(t *testing.T) {
	if popularityScore(500) >= 1.0 {
		t.Error("popularity is not bounded below 1")
	}
	if popularityScore(1000)-popularityScore(500) > 0.05 {
		t.Error("popularity does not flatten at the top end")
	}
	if popularityScore(0) != 0 {
		t.Error("zero popularity should score zero")
	}
}

func TestJaroWinklerBounds(t *testing.T) {
	if got := jaroWinkler("arrival", "arrival"); got != 1 {
		t.Errorf("identical strings = %.3f, want 1", got)
	}
	if got := jaroWinkler("arrival", ""); got != 0 {
		t.Errorf("empty comparison = %.3f, want 0", got)
	}
	if got := jaroWinkler("abc", "xyz"); got != 0 {
		t.Errorf("disjoint strings = %.3f, want 0", got)
	}
}

// The breakdown must explain the total: components combine by their weights,
// and a wrong-year candidate reports the gap that sank it.
func TestScoreBreakdownExplainsTheTotal(t *testing.T) {
	q := Query{Kind: KindMovie, Title: "Aladdin", Year: 1992}
	b := ScoreBreakdown(q, cand("Aladdin", 2019, 800))

	if b.Title < 0.99 {
		t.Errorf("title sub-score = %.3f, want ~1.0 for an exact title", b.Title)
	}
	if b.YearGap != 27 {
		t.Errorf("year gap = %d, want 27", b.YearGap)
	}
	if b.Year != 0 {
		t.Errorf("year sub-score = %.3f, want 0 for a 27-year gap", b.Year)
	}
	// The reported total must equal the weighted recombination and Score().
	want := b.Title*weightTitle + b.Year*weightYear + b.Popularity*weightPopularity
	if abs2(b.Total-want) > 1e-9 {
		t.Errorf("total %.4f != weighted sum %.4f", b.Total, want)
	}
	if s := Score(q, cand("Aladdin", 2019, 800)); abs2(s-b.Total) > 1e-9 {
		t.Errorf("Score %.4f disagrees with Breakdown.Total %.4f", s, b.Total)
	}
	// An exact title with a 27-year gap lands in review, not matched — applied
	// but flagged. The breakdown is what tells a user the year is why: without
	// it, "0.70, review" is a mystery.
	if StateFor(b.Total) != StateReview {
		t.Errorf("wrong-year candidate scored %.3f (%s), want review", b.Total, StateFor(b.Total))
	}
	if b.Total >= ThresholdAuto {
		t.Errorf("wrong-year candidate scored %.3f, should not auto-apply", b.Total)
	}
}

func abs2(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func TestRankSortsBestFirst(t *testing.T) {
	q := Query{Kind: KindMovie, Title: "Dune", Year: 2021}
	cands := []Candidate{
		cand("Dune", 1984, 25),
		cand("Dune Drifter", 2020, 1),
		cand("Dune", 2021, 90),
	}
	Rank(q, cands)
	if cands[0].Year != 2021 {
		t.Errorf("first = %q (%d), want Dune 2021", cands[0].Title, cands[0].Year)
	}
	for i := 1; i < len(cands); i++ {
		if cands[i-1].Score < cands[i].Score {
			t.Error("candidates are not sorted descending by score")
		}
	}
}
