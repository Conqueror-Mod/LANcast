package meta

import (
	"strings"
	"unicode"

	"lancast/internal/media"
)

// Match states, mirrored in media_item.match_state.
const (
	StateMatched   = "matched"
	StateReview    = "review"
	StateUnmatched = "unmatched"
	StateLocked    = "locked"

	// StateLocal means the identity came from a local source — an NFO the
	// user or another tool wrote. There is nothing to review: the user
	// already said what this is, and reporting it as "no match found" would
	// bury real problems under items that are perfectly resolved.
	StateLocal = "local"
)

// Confidence thresholds. The middle band carries the design: a low-confidence
// match is usually still right, so applying it is a good default — but it is
// recorded as uncertain rather than presented as fact.
const (
	ThresholdAuto   = 0.85 // at or above: applied silently
	ThresholdReview = 0.55 // at or above: applied, and flagged for review
)

// Scoring weights. Popularity is a tiebreak only and must never rescue a weak
// title match — that is how servers confidently match obscure files to
// blockbusters.
const (
	weightTitle      = 0.60
	weightYear       = 0.30
	weightPopularity = 0.10
)

// StateFor maps a score to the match state it produces.
func StateFor(score float64) string {
	switch {
	case score >= ThresholdAuto:
		return StateMatched
	case score >= ThresholdReview:
		return StateReview
	default:
		return StateUnmatched
	}
}

// Breakdown is the anatomy of a score: the component sub-scores (each 0..1)
// that combine, by their weights, into the total. It exists so the match can
// explain itself — "title matched but the year is off by 27" — rather than
// presenting a bare number the user cannot argue with.
type Breakdown struct {
	Title      float64 `json:"title"`      // title similarity, 0..1
	Year       float64 `json:"year"`       // year agreement, 0..1 (1.0 for episodes: n/a)
	Popularity float64 `json:"popularity"` // popularity, 0..1
	Total      float64 `json:"total"`      // the weighted sum, 0..1
	// Artist is an album's artist agreement, 0..1, and absent for every other
	// kind (an album's Popularity is 0: MusicBrainz has none).
	Artist  float64 `json:"artist,omitempty"`
	YearGap int     `json:"year_gap"` // |want-got| years, 0 when either is unknown
}

// Score rates a candidate against a query, 0.0 to 1.0.
func Score(q Query, c Candidate) float64 { return ScoreBreakdown(q, c).Total }

// ScoreBreakdown is Score with its work shown. Score delegates here so the two
// can never disagree about what a candidate is worth.
func ScoreBreakdown(q Query, c Candidate) Breakdown {
	if q.Kind == KindEpisode {
		return scoreEpisode(q, c)
	}
	if q.Kind == KindAlbum {
		return scoreAlbum(q, c)
	}

	// An absent year scores neutral (half credit) rather than redistributing
	// its weight onto the title. That distinction matters: with no year, an
	// exact title match is genuinely ambiguous — "Solaris" is two different
	// films — so the result lands in the review band instead of auto-applying.
	// Inflating the title weight to compensate would manufacture confidence
	// the evidence does not support.
	t := titleScore(q.Title, c.Title)
	y := yearScore(q.Year, c.Year)
	p := popularityScore(c.Popularity)
	b := Breakdown{
		Title:      t,
		Year:       y,
		Popularity: p,
		Total:      clamp(t*weightTitle + y*weightYear + p*weightPopularity),
	}
	if q.Year != 0 && c.Year != 0 {
		b.YearGap = abs(q.Year - c.Year)
	}
	return b
}

// scoreEpisode matches on the parent series name. Season and episode numbers
// are not scored — they are looked up exactly when fetching the episode, so a
// numbering mismatch is not a weak match, it is a different episode and never
// reaches scoring at all.
func scoreEpisode(q Query, c Candidate) Breakdown {
	series := q.Series
	if series == "" {
		series = q.Title
	}
	t := titleScore(series, c.Title)
	p := popularityScore(c.Popularity)
	// The year weight folds into the title for episodes; report Year as 1.0 so
	// the breakdown does not read as a year failure where year does not apply.
	return Breakdown{
		Title:      t,
		Year:       1,
		Popularity: p,
		Total:      clamp(t*(weightTitle+weightYear) + p*weightPopularity),
	}
}

// Album weights (ADR 0115). The artist carries real weight because an album
// title alone is weak evidence: "Greatest Hits" is a title hundreds of artists
// share. The year carries little, because a tag's year is often a reissue's.
const (
	albumWeightTitle  = 0.55
	albumWeightArtist = 0.30
	albumWeightYear   = 0.15
	// artistFloor is the artist agreement below which a candidate is another
	// artist's record, however well its title matches.
	artistFloor = 0.80
	// titleFloor is the same for the title: the right artist's other record
	// is not this one. Measured: シド's "Shout at the Devil" scored into
	// review for a live album of theirs on the artist alone.
	titleFloor = 0.75
	// wrongArtistCap holds another record under the review band, so it is
	// neither applied nor offered as a probable match.
	wrongArtistCap = ThresholdReview - 0.05
)

/*
 * scoreAlbum rates a release group against an album's tags.
 *
 * An artist that disagrees caps the score below review instead of only
 * costing its weight: an exact "Greatest Hits" by the wrong artist would
 * otherwise score 0.55 + year and land in the review band as a likely match,
 * which is the music version of matching an obscure film to a blockbuster.
 *
 * No artist on the album (an untagged rip) cannot be checked, and scores
 * neutral, which keeps an exact title out of the silent band.
 */
func scoreAlbum(q Query, c Candidate) Breakdown {
	t := albumTitleScore(q.Title, media.AlbumBaseTitle(c.Title))
	a := 0.5
	if q.Artist != "" {
		a = artistScore(q.Artist, append([]string{c.Artist}, c.ArtistNames...))
	}
	y := albumYearScore(q.Year, c.Year)
	b := Breakdown{
		Title:  t,
		Year:   y,
		Artist: a,
		Total:  clamp(t*albumWeightTitle + a*albumWeightArtist + y*albumWeightYear),
	}
	wrong := t < titleFloor || q.Artist != "" && a < artistFloor
	if wrong && b.Total > wrongArtistCap {
		b.Total = wrongArtistCap
	}
	if q.Year != 0 && c.Year != 0 {
		b.YearGap = abs(q.Year - c.Year)
	}
	return b
}

/*
 * albumText is normalize for music: every script's letters, not only a to z.
 *
 * normalize keeps a-z and 0-9, which is right for the film titles it was
 * written for and blind to "Страшно" and "夜更けと雨と", which it reduces
 * to nothing, so an exact Cyrillic match scored zero (measured, ADR 0115).
 * Built on the same SortTitle, so the two agree on everything Latin.
 */
func albumText(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(media.SortTitle(s)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
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

func albumTitleScore(a, b string) float64 {
	na, nb := albumText(a), albumText(b)
	if na == "" || nb == "" {
		return 0
	}
	if na == nb {
		return 1
	}
	return jaroWinkler(na, nb)
}

// artistScore is the best agreement between the album's artist and any name
// the candidate credits.
func artistScore(local string, names []string) float64 {
	best := 0.0
	for _, n := range names {
		if s := artistAgreement(local, n); s > best {
			best = s
		}
	}
	return best
}

/*
 * artistAgreement compares two artist names by their words, not their
 * letters.
 *
 * The title measure rewards a shared opening, which is right for titles and
 * wrong for names: measured, "Billy the Kit" came within reach of Billy Joel
 * and "Fueled By Ramen" of "Fueled by Fire". Names agree when they are equal,
 * when they share most of their words, or when every word of a name of two or
 * more is in the other ("Biz Markie" in "The Diabolical Biz Markie"). A
 * near-identical spelling still counts ("Blut Engel", "Blutengel"), at a
 * similarity high enough that a different first name cannot reach it.
 */
func artistAgreement(a, b string) float64 {
	na, nb := albumText(a), albumText(b)
	if na == "" || nb == "" {
		return 0
	}
	if na == nb {
		return 1
	}
	wa, wb := strings.Fields(na), strings.Fields(nb)
	inB := map[string]bool{}
	for _, w := range wb {
		inB[w] = true
	}
	shared := 0
	for _, w := range wa {
		if inB[w] {
			shared++
		}
	}
	union := len(wa) + len(wb) - shared
	best := float64(shared) / float64(union)
	if len(wa) >= 2 && shared == len(wa) && best < 0.9 {
		best = 0.9
	}
	if jw := jaroWinkler(na, nb); jw >= 0.95 && jw > best {
		best = jw
	}
	return best
}

// albumYearScore is gentler than a film's. A film's year is its one release;
// an album's tag is often a remaster's or a compilation's date, years after
// the record first came out, and that is still the record.
func albumYearScore(want, got int) float64 {
	if want == 0 || got == 0 {
		return 0.6
	}
	switch d := abs(want - got); {
	case d == 0:
		return 1.0
	case d <= 2:
		return 0.8
	default:
		return 0.4
	}
}

// tieMargin is how close the runner-up may come before the best is no longer
// the answer but one of two.
const tieMargin = 0.02

/*
 * Decide picks the answer from ranked candidates and the state it earns.
 *
 * The best candidate's score sets the state, except when a different record
 * scores within tieMargin of it: then the pick is a coin toss, and a coin toss
 * is not "matched". Queen has three release groups called Greatest Hits
 * (1981, 2008, 2023); an album tagged with no year scores all three alike,
 * and applying the first silently would be a guess presented as a fact. It is
 * applied and flagged for review instead, as a weak match is.
 *
 * One tie is not a coin toss: a studio album against a single, an EP or a
 * compilation of the same name. Prince's "1999" is a single and an album a
 * month apart, and Motörhead's "Ace of Spades" is an album, two singles and
 * three compilations. An album in a library called "1999" is the album; when
 * exactly one of the tied records is a plain Album, it is the answer. Two
 * plain albums tied is a coin toss again.
 *
 * cands must already be ranked (Rank). Empty gives the zero candidate and
 * StateUnmatched.
 */
func Decide(cands []Candidate) (Candidate, string) {
	if len(cands) == 0 {
		return Candidate{}, StateUnmatched
	}
	best := cands[0]
	state := StateFor(best.Score)
	if state != StateMatched {
		return best, state
	}
	var tied []Candidate
	for _, c := range cands {
		if best.Score-c.Score < tieMargin {
			tied = append(tied, c)
		}
	}
	distinct := map[string]bool{}
	for _, c := range tied {
		distinct[c.ExternalID] = true
	}
	if len(distinct) < 2 {
		return best, state
	}
	var albums []Candidate
	for _, c := range tied {
		if c.ReleaseType == "Album" {
			albums = append(albums, c)
		}
	}
	if len(albums) == 1 {
		return albums[0], StateFor(albums[0].Score)
	}
	return best, StateReview
}

/*
 * AlbumQuery is the query for an album as a library holds it: its title
 * without edition notes, because a release group is every edition of a
 * record and "(Deluxe)" is not in its name (ADR 0115). The one place an album
 * query is built, so enrichment and the measurement cannot ask differently.
 */
func AlbumQuery(title, albumArtist string, year int) Query {
	return Query{
		Kind:   KindAlbum,
		Title:  media.AlbumBaseTitle(title),
		Artist: strings.TrimSpace(albumArtist),
		Year:   year,
	}
}

// SortTitleOf derives the sort key for a title. It exists so callers outside
// this package do not reach past it into internal/media and grow a second
// opinion about normalization.
func SortTitleOf(title string) string { return media.SortTitle(title) }

// titleScore compares normalized titles. It reuses the normalizer in
// internal/media so scanning and matching cannot disagree about what a title
// is — two normalizers that drift is a bug factory (see CLAUDE.md).
func titleScore(a, b string) float64 {
	na, nb := normalize(a), normalize(b)
	if na == "" || nb == "" {
		return 0
	}
	if na == nb {
		return 1
	}
	return jaroWinkler(na, nb)
}

// normalize lowercases, drops leading articles, and strips punctuation so that
// "Ocean's Eleven" and "Oceans Eleven" compare equal.
func normalize(s string) string {
	s = media.SortTitle(s)
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevSpace = false
		case r == ' ':
			if !prevSpace && b.Len() > 0 {
				b.WriteRune(' ')
				prevSpace = true
			}
		default:
			// Punctuation is dropped entirely rather than becoming a space, so
			// "Ocean's" normalizes to "oceans" and not "ocean s".
		}
	}
	return strings.TrimSpace(b.String())
}

// yearScore rewards exact release years and punishes distant ones hard. A
// two-year gap is almost always a different film with a similar name.
func yearScore(want, got int) float64 {
	if want == 0 || got == 0 {
		return 0.5
	}
	switch d := abs(want - got); {
	case d == 0:
		return 1.0
	case d == 1:
		return 0.8
	case d == 2:
		return 0.25
	default:
		return 0
	}
}

// popularityScore compresses an unbounded popularity figure into 0..1 with
// diminishing returns, so a runaway blockbuster cannot dominate the tiebreak.
func popularityScore(p float64) float64 {
	if p <= 0 {
		return 0
	}
	return p / (p + 20)
}

// jaroWinkler returns similarity in 0..1, favouring strings that agree on their
// opening characters — which is how release names usually differ from titles.
func jaroWinkler(a, b string) float64 {
	j := jaro(a, b)
	if j < 0.7 {
		return j
	}
	prefix := 0
	for i := 0; i < min(4, min(len(a), len(b))); i++ {
		if a[i] != b[i] {
			break
		}
		prefix++
	}
	return j + float64(prefix)*0.1*(1-j)
}

func jaro(a, b string) float64 {
	if a == b {
		return 1
	}
	la, lb := len(a), len(b)
	if la == 0 || lb == 0 {
		return 0
	}

	window := max(la, lb)/2 - 1
	if window < 0 {
		window = 0
	}

	aMatched := make([]bool, la)
	bMatched := make([]bool, lb)
	matches := 0

	for i := 0; i < la; i++ {
		lo := max(0, i-window)
		hi := min(i+window+1, lb)
		for k := lo; k < hi; k++ {
			if bMatched[k] || a[i] != b[k] {
				continue
			}
			aMatched[i] = true
			bMatched[k] = true
			matches++
			break
		}
	}
	if matches == 0 {
		return 0
	}

	transpositions := 0
	k := 0
	for i := 0; i < la; i++ {
		if !aMatched[i] {
			continue
		}
		for !bMatched[k] {
			k++
		}
		if a[i] != b[k] {
			transpositions++
		}
		k++
	}

	m := float64(matches)
	return (m/float64(la) + m/float64(lb) + (m-float64(transpositions)/2)/m) / 3
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
