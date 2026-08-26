package meta

import "testing"

/*
 * Smart collections (the MCU rule).
 *
 * The facts these are written against were checked against the live TMDB API,
 * not recalled: Avengers: Endgame's `belongs_to_collection` is "The Avengers
 * Collection", its keywords include 180547 `marvel cinematic universe (mcu)`,
 * and no field anywhere in the payload names the umbrella. 81 films carry the
 * keyword, and unreleased ones — Spider-Man: Brand New Day, Avengers: Doomsday
 * — already carry it too, which is what makes the rule self-updating rather
 * than a list somebody has to maintain.
 */

func kwRec(source string, keywords ...int) Record {
	r := Record{Source: source}
	for _, id := range keywords {
		r.Keywords = append(r.Keywords, Keyword{ID: id, Name: "k"})
	}
	return r
}

func TestSmartCollectionsForMatchesTheMCUKeyword(t *testing.T) {
	got := SmartCollectionsFor(kwRec("tmdb", 9715, 180547, 4565))
	if len(got) != 1 {
		t.Fatalf("got %d smart collections, want 1", len(got))
	}
	if got[0].Name != "Marvel Cinematic Universe" {
		t.Errorf("name = %q", got[0].Name)
	}
}

// The overwhelming majority of films are in none, which is the point: this
// adds a tile for a grouping somebody goes looking for and nothing otherwise.
func TestAnOrdinaryFilmJoinsNothing(t *testing.T) {
	if got := SmartCollectionsFor(kwRec("tmdb", 9715, 4565)); len(got) != 0 {
		t.Errorf("got %d smart collections, want none: %+v", len(got), got)
	}
	if got := SmartCollectionsFor(kwRec("tmdb")); len(got) != 0 {
		t.Errorf("a film with no keywords joined %d collections", len(got))
	}
}

/*
 * Keyword spaces are per-provider. Two sources numbering their tags
 * independently must not collide — 180547 meaning the MCU at TMDB says nothing
 * about what 180547 means anywhere else.
 */
func TestAnotherProvidersKeywordDoesNotMatch(t *testing.T) {
	if got := SmartCollectionsFor(kwRec("someoneelse", 180547)); len(got) != 0 {
		t.Errorf("a foreign keyword space matched: %+v", got)
	}
}

/*
 * The identity is namespaced, and this is the assertion that stops a real
 * collision: TMDB collection 86311 is "The Avengers Collection" and keyword
 * 180547 is the MCU. Both are small integers from one provider, and without the
 * prefix they meet in (provider, external_id) and one absorbs the other's
 * films.
 */
func TestSmartCollectionIDCannotCollideWithAFranchise(t *testing.T) {
	sc := SmartCollection{Source: "tmdb", KeywordID: 180547, Name: "MCU"}
	if got := sc.ExternalID(); got != "keyword:180547" {
		t.Errorf("ExternalID = %q, want keyword:180547", got)
	}
	// A franchise id is a bare number; a smart collection's never is.
	if sc.ExternalID() == "180547" {
		t.Error("a smart collection shares an id space with a franchise")
	}
}

/*
 * Every entry must be a place people go, not merely a property films share.
 * "superhero" is a keyword too, and a collection of every superhero film is a
 * genre filter wearing a collection's clothes -- this guards the list from
 * growing into a second genre system by accident.
 */
func TestTheCuratedSetStaysCurated(t *testing.T) {
	if len(SmartCollections) > 8 {
		t.Errorf("%d smart collections: this has become a genre system",
			len(SmartCollections))
	}
	seen := map[string]bool{}
	for _, sc := range SmartCollections {
		if sc.Name == "" || sc.Source == "" || sc.KeywordID == 0 {
			t.Errorf("incomplete smart collection: %+v", sc)
		}
		key := sc.Source + sc.ExternalID()
		if seen[key] {
			t.Errorf("duplicate smart collection: %+v", sc)
		}
		seen[key] = true
	}
}
