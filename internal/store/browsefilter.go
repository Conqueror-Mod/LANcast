package store

import (
	"context"
	"fmt"
	"strings"
)

/*
 * Browse filters that are derived rather than stored.
 *
 * Resolution is the clear case. Nothing in the database says "4K" — there is a
 * width and a height from the probe, and the label is a bucket over them. The
 * bucket rules live here as one pure function so the facet list and the filter
 * clause cannot drift: a library offering "4K" and a filter that then finds
 * nothing is worse than not offering it, and that is exactly what two copies of
 * these numbers would eventually produce.
 *
 * Bucketed on width rather than height, because height is what varies. A
 * 2.39:1 film at 4K is 3840x1608 and a 16:9 one is 3840x2160 — same format,
 * heights 550px apart — while the width holds still across aspect ratios. A
 * height-based rule silently files every scope film one tier too low.
 */

// ResolutionBucket is the browse label for a video width — one tier on the
// browse filter.
//
// The json tags are load-bearing and were missing until 2026-09-03. This struct
// is serialized straight to the client inside Facets, so without them the wire
// carried Key, Label, MinWidth and MaxWidth — while docs/api.md documented
// key/label/min_width/max_width and the client read the same. Every resolution
// chip therefore rendered with an undefined label and an undefined value, and
// pressing one sent `resolution=undefined`, which the contract says is ignored
// rather than rejected. So the filter was a silent no-op.
//
// Neither suite could see it. The Go test asserted b.MinWidth, which is the
// struct and not the JSON; the client test built its fixture in snake_case,
// which is what the client believed rather than what it received. Each half was
// correct about itself and nothing asked whether they agreed — which is why
// TestResolutionBucketWireShape now tests the bytes.
type ResolutionBucket struct {
	Key      string `json:"key"`       // stable, used in the query string
	Label    string `json:"label"`     // what the UI shows
	MinWidth int    `json:"min_width"` // inclusive
	MaxWidth int    `json:"max_width"` // inclusive; 0 means no upper bound
}

/*
 * ResolutionBuckets are the tiers a library can be filtered by, widest first.
 *
 * The boundaries sit well below the nominal widths on purpose. Real files are
 * not 3840 and 1920 exactly: a 4K remux cropped for scope is 3840, a UHD
 * web-dl is often 3840, but plenty of 1080p sources are 1912 or 1918 after
 * cropping, and DVD rips land anywhere from 700 to 720. Testing against the
 * nominal number files those one tier too low, which reads as a scanner bug
 * rather than as an arithmetic choice.
 */
var ResolutionBuckets = []ResolutionBucket{
	{Key: "uhd", Label: "4K", MinWidth: 3000},
	{Key: "hd1080", Label: "1080p", MinWidth: 1700, MaxWidth: 2999},
	{Key: "hd720", Label: "720p", MinWidth: 1100, MaxWidth: 1699},
	{Key: "sd", Label: "SD", MinWidth: 1, MaxWidth: 1099},
}

// resolutionBucket returns the bucket for a width, and whether there is one. A
// width of zero is not SD — it is a file that has not been probed, which is a
// different thing and must not be filed under a resolution it never claimed.
func resolutionBucket(width int) (ResolutionBucket, bool) {
	if width <= 0 {
		return ResolutionBucket{}, false
	}
	for _, b := range ResolutionBuckets {
		if width >= b.MinWidth && (b.MaxWidth == 0 || width <= b.MaxWidth) {
			return b, true
		}
	}
	return ResolutionBucket{}, false
}

// bucketByKey looks a bucket up by its query-string key.
func bucketByKey(key string) (ResolutionBucket, bool) {
	for _, b := range ResolutionBuckets {
		if b.Key == key {
			return b, true
		}
	}
	return ResolutionBucket{}, false
}

// CastMember is one person who appears in a library, with how much of it they
// are in. The count is what makes a type-ahead usable: "Ford" matching four
// people is answered by which of them this library actually holds.
type CastMember struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Items int    `json:"items"`
}

/*
 * SearchCast finds people credited in one library, or in all of them.
 *
 * A type-ahead rather than a facet list, and that is the whole design. Genres
 * are a dozen values and fit in a row of chips; a thousand-film library has
 * thousands of credited people, so the exhaustive list this returns for an
 * empty query is capped and ordered by how much of the library each person is
 * in — which makes the default view "who is this library actually about"
 * rather than an alphabetical wall starting at Aaron.
 *
 * Ordered by item count then name so the ordering is total: two people in the
 * same number of films must not swap places between requests, or a list that
 * is re-fetched as you type appears to shuffle itself.
 */
func (s *Store) SearchCast(ctx context.Context, libraryID int64, query, role string, limit int) ([]CastMember, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	/*
	 * libraryID 0 searches every library.
	 *
	 * "Everything Harrison Ford is in" is the question people actually have,
	 * and it does not stop at a library boundary — he is in films and he is in
	 * television, and those are two libraries here. The per-library form stays
	 * because the cast *picker* is a filter for the grid it sits above, and
	 * that grid is one library.
	 *
	 * Zero rather than a pointer or a second method: a library id of 0 cannot
	 * exist (AUTOINCREMENT starts at 1), so it is free to mean "all of them"
	 * and every caller that passes a real id is unchanged.
	 */
	args := []any{}
	where := ""
	scope := ""
	if libraryID > 0 {
		scope = ` AND m.library_id = ?`
		args = append(args, libraryID)
	}
	if role != "" {
		// Scoped to one role, because "who is in this" and "who made this" are
		// different questions with different answers, and an actor-director
		// appears under both — correctly, once in each.
		where += ` AND c.role = ?`
		args = append(args, role)
	}
	if query != "" {
		/*
		 * Substring match, anchored nowhere.
		 *
		 * It was prefix-or-word — `q%` or `% q%` — which finds a first name or
		 * a surname and nothing else. That is most of what "actor search is
		 * very limited" meant once the crash underneath it was fixed: "niro"
		 * found nobody, because De Niro's surname begins with "De", and
		 * "gyllen" found nobody either.
		 *
		 * A leading wildcard gives up the index, and that is affordable here
		 * and worth saying why rather than discovering later: `person` holds
		 * one row per credited human rather than one per credit, the result is
		 * capped at 200, and the query is already grouping and counting across
		 * `credit` — the scan is not the expensive half. A million-person table
		 * would change that answer; a home library will not have one.
		 *
		 * LIKE is case-insensitive for ASCII in SQLite, which is what a name
		 * search wants. `like` escapes the wildcards so a search for "%" is a
		 * search for a percent sign rather than for everybody.
		 */
		where += ` AND p.name LIKE ? ESCAPE '\'`
		args = append(args, "%"+like(query)+"%")
	}
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, MIN(c.role), COUNT(DISTINCT m.id) AS items
		FROM person p
		JOIN credit c ON c.person_id = p.id
		JOIN media_item m ON m.id = c.item_id
		WHERE m.missing = 0`+scope+where+`
		GROUP BY p.id, p.name
		ORDER BY items DESC, p.name
		LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("search cast: %w", err)
	}
	defer rows.Close()

	out := []CastMember{}
	for rows.Next() {
		var c CastMember
		if err := rows.Scan(&c.ID, &c.Name, &c.Role, &c.Items); err != nil {
			return nil, fmt.Errorf("search cast: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CastNames resolves person ids to names, for showing an active filter as a
// pill without the client having to hold a lookup table it did not fetch.
func (s *Store) CastNames(ctx context.Context, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name FROM person WHERE id IN (`+placeholders(len(ids))+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("cast names: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("cast names: %w", err)
		}
		out[id] = name
	}
	return out, rows.Err()
}

// CollectionFacet is one collection a library can be filtered by, with how many
// items it holds — the number that tells a two-film pairing from a franchise.
type CollectionFacet struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Members int    `json:"members"`
}

/*
 * RatingThresholds are the "rated at least" steps the client offers.
 *
 * Whole and half points from 5, because below five a rating filter stops
 * separating anything — almost nothing in a curated library rates under it, so
 * the lower steps would all return the same grid and read as broken controls.
 */
var RatingThresholds = []float64{9, 8.5, 8, 7.5, 7, 6.5, 6, 5}

/*
 * like escapes LIKE's own wildcards.
 *
 * Without it a search for "%" matches every person in the library and a search
 * for "_" matches every two-letter difference, which is a search box that
 * answers questions nobody asked. The escape character has to be declared in
 * the SQL for SQLite to honour it, which is why the clause carries ESCAPE.
 */
func like(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(q)
}
