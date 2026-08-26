package meta

import "strconv"

/*
 * Smart collections: a grouping defined by a rule rather than by a list.
 *
 * A provider's franchise field answers "which sequence is this film part of"
 * and always answers narrowly. Avengers: Endgame belongs to "The Avengers
 * Collection". Iron Man belongs to "Iron Man Collection". Blade to "Blade
 * Collection". There is no field anywhere in the TMDB movie payload that says
 * *Marvel Cinematic Universe* — verified against the live API rather than
 * assumed, because it is the kind of thing that is easy to be confidently wrong
 * about.
 *
 * The umbrella exists as a **keyword**: 180547, `marvel cinematic universe
 * (mcu)`, carried by 81 films from Iron Man (2008) onward.
 *
 * Which makes the rule the whole feature. A static list of 81 ids would be
 * wrong the day the 82nd film is announced; a keyword is a membership test that
 * keeps answering. TMDB tags films **before release** — Spider-Man: Brand New
 * Day and Avengers: Doomsday both carry 180547 already — so a smart collection
 * is correct about a film the moment its file appears, with nothing to update
 * here and no release calendar to track.
 *
 * ## Why this is a keyword id and not a name
 *
 * The id is stable; the display name is not. TMDB has renamed keywords, and a
 * rule keyed on "marvel cinematic universe" would silently stop matching the
 * day it became "marvel cinematic universe (mcu)" — which is what it is called
 * today. A rule that fails by matching nothing is the worst kind, because the
 * collection does not break, it just quietly stops growing.
 *
 * ## Why the set is in code
 *
 * It is a short list of groupings that are worth a tile, and each entry is a
 * judgement — "superhero" is a keyword too, and a collection of every superhero
 * film is a genre filter wearing a collection's clothes. Putting the list in
 * settings would invite exactly that, and turn a curated idea into a
 * configuration surface nobody asked for. Adding an entry is one line and a
 * test; that is the right amount of friction for a decision like this.
 */

// SmartCollection is a grouping every item carrying `KeywordID` joins.
type SmartCollection struct {
	// KeywordID is the provider's id for the tag, not its name. See above.
	KeywordID int
	// Name is what the collection is called in the library. It is ours rather
	// than the provider's: "marvel cinematic universe (mcu)" is a tag, and
	// "Marvel Cinematic Universe" is a title.
	Name string
	// Source names which provider's keyword space `KeywordID` belongs to. Two
	// providers numbering their tags differently must not collide, and this is
	// also half of the collection's own identity.
	Source string
}

/*
 * SmartCollections is the curated set.
 *
 * Deliberately short. Every entry is a claim that a grouping is a *place people
 * go*, not merely a property films share — the test that keeps this from
 * becoming a second genre system.
 */
var SmartCollections = []SmartCollection{
	{Source: "tmdb", KeywordID: 180547, Name: "Marvel Cinematic Universe"},
}

/*
 * ExternalID is the collection's identity, and it is namespaced so a smart
 * collection can never be confused with a franchise the provider handed us.
 *
 * TMDB collection 86311 is "The Avengers Collection"; keyword 180547 is the
 * MCU. Both are small integers from the same provider, and without a prefix
 * they would eventually meet in `(provider, external_id)` and one would
 * silently absorb the other's films.
 */
func (s SmartCollection) ExternalID() string {
	return "keyword:" + strconv.Itoa(s.KeywordID)
}

// SmartCollectionsFor returns the smart collections a record's keywords place
// it in. Empty for the overwhelming majority of films, which is the point:
// this adds a tile for a grouping somebody would go looking for, and nothing
// at all for everything else.
func SmartCollectionsFor(rec Record) []SmartCollection {
	if len(rec.Keywords) == 0 {
		return nil
	}
	var out []SmartCollection
	for _, sc := range SmartCollections {
		if sc.Source != rec.Source {
			continue
		}
		for _, k := range rec.Keywords {
			if k.ID == sc.KeywordID {
				out = append(out, sc)
				break
			}
		}
	}
	return out
}
