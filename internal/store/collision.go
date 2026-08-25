package store

import (
	"context"
	"database/sql"
	"fmt"
)

func nullInt(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

/*
 * Two files claiming one work (ADR 0042).
 *
 * A shared provider id is **not** evidence of duplication. On the library this
 * was written against, thirteen pairs shared one, and they were five different
 * situations: seven redundant copies, three same-cut-different-encode, one
 * genuine second edition, one film split across two discs, and one outright
 * misfile — a 1989 film wearing a 2022 film's identity because of a stale
 * `.nfo`.
 *
 * Two of those thirteen are not duplicates at all. So this reports a
 * *collision* and calls it nothing else: LANcast surfaces the evidence and
 * takes no action. Never merge, never rank, never delete.
 *
 * What is deliberately absent is duration. `media_item.duration_ms` is
 * overwritten with the **provider's** runtime on match, so two rows matched to
 * one id always report identical durations whatever the files hold — including
 * the misfile, where one film is 126 minutes and the other 177. A tiebreak
 * built on that column would agree with itself and be wrong.
 *
 * What is left that is real: the path, `size_bytes`, and comparing the bytes.
 */

// CollisionMember is one file in a collision, with the evidence that is real.
type CollisionMember struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	// Path is shown here, unlike everywhere else in the API, because the whole
	// value of the report is being able to go and look at the two files. A
	// collision the viewer cannot locate on disk is a notification, not a
	// report. Admin-only for that reason.
	Path string `json:"path"`
	// Edition is the marker the filename claimed, or empty. Displayed, never
	// trusted: the motivating file called itself an alternate cut and was a
	// byte-for-byte copy.
	Edition   *string `json:"edition,omitempty"`
	SizeBytes *int64  `json:"size_bytes"`
	LibraryID int64   `json:"library_id"`
	Missing   bool    `json:"missing"`
}

// Collision is a set of rows claiming the same work.
type Collision struct {
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id"`
	/*
	 * Season and Episode complete the identity for an episode, and are absent
	 * for a film. An episode carries its *show's* external_id, so the pair
	 * alone does not identify a work -- see Collisions.
	 */
	Season  *int64            `json:"season,omitempty"`
	Episode *int64            `json:"episode,omitempty"`
	Members []CollisionMember `json:"members"`
	/*
	 * SameSize is the cheap half of "are these the same file", and it is
	 * reported separately from a byte comparison because it is free and always
	 * available. Equal sizes make a copy likely; different sizes rule one out
	 * outright, which is the more useful of the two answers and the one that
	 * needs no I/O.
	 */
	SameSize bool `json:"same_size"`
}

/*
 * Collisions lists every work claimed by more than one row.
 *
 * Keyed on (provider, external_id, season, episode), and the last two are not
 * decoration. **Every episode of a show carries the show's external_id** --
 * that is how an episode's provider identity works, the show id plus a
 * position -- so keying on the pair alone reports every multi-episode show in
 * the library as one enormous collision. On the library this was built
 * against that was 999 episode rows against 86 real film ones: the report
 * would have been 92% noise, burying the thing it exists to surface.
 *
 * A film has NULL season and episode, so it groups exactly as before. An
 * episode groups with the *same* episode of the same show, which is the real
 * condition -- two rips of S01E01 -- and was undetectable under the old key.
 *
 * Missing rows are
 * included on purpose: a file that has gone away is exactly the context that
 * explains why a second copy exists, and hiding it would turn a two-row
 * collision into a one-row mystery.
 *
 * Containers are excluded. A show and its season legitimately carry the same
 * provider id — that is the hierarchy working, not a collision — and reporting
 * it would bury the real ones under every show in the library.
 */
func (s *Store) Collisions(ctx context.Context, libraryID int64) ([]Collision, error) {
	/*
	 * The same predicate twice: once to find which works are claimed more than
	 * once, once to fetch the rows of those works. Written out rather than
	 * spliced together, because a filter that has to match in two places is
	 * exactly the thing worth being able to read.
	 */
	const claimed = `
		provider IS NOT NULL AND external_id IS NOT NULL AND path <> ''
		AND kind NOT IN ('show','season','collection','serial',
		                 'artist','album','gallery','playlist')`

	// COALESCE, because NULL never equals NULL: without it every film -- which
	// has no season or episode -- fails its own IN test and the report is
	// empty. The sentinel is -1 rather than 0, since disc 0 and track 0 are
	// real values for music.
	const workKey = `provider, external_id, COALESCE(season,-1), COALESCE(episode,-1)`

	args := []any{}
	scope := ""
	if libraryID > 0 {
		scope = " AND library_id = ?"
		args = append(args, libraryID, libraryID)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT provider, external_id, season, episode, id, title, path,
		       edition, size_bytes, library_id, missing
		FROM media_item
		WHERE `+claimed+scope+`
		  AND (`+workKey+`) IN (
			SELECT `+workKey+` FROM media_item
			WHERE `+claimed+scope+`
			GROUP BY `+workKey+`
			HAVING COUNT(*) > 1
		  )
		ORDER BY provider, external_id, COALESCE(season,-1),
		         COALESCE(episode,-1), size_bytes DESC, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("collisions: %w", err)
	}
	defer rows.Close()

	out := []Collision{}
	// The run key must match the SQL's grouping exactly, or two episodes of one
	// show fold into a single collision on the way out -- undoing in Go what
	// the query was careful about.
	// The run key must match the SQL's grouping exactly, or two episodes of one
	// show fold into a single collision on the way out -- undoing in Go what
	// the query was careful about. A comparable struct rather than a joined
	// string, so no separator has to be chosen and proven impossible.
	type workKeyT struct {
		provider, externalID string
		season, episode      int64
	}
	var last workKeyT
	for rows.Next() {
		var k workKeyT
		var season, episode sql.NullInt64
		var m CollisionMember
		if err := rows.Scan(&k.provider, &k.externalID, &season, &episode,
			&m.ID, &m.Title, &m.Path,
			&m.Edition, &m.SizeBytes, &m.LibraryID, &m.Missing); err != nil {
			return nil, fmt.Errorf("collisions: %w", err)
		}
		k.season, k.episode = season.Int64, episode.Int64

		if n := len(out); n > 0 && k == last {
			out[n-1].Members = append(out[n-1].Members, m)
			continue
		}
		last = k
		out = append(out, Collision{
			Provider: k.provider, ExternalID: k.externalID,
			Season: nullInt(season), Episode: nullInt(episode),
			Members: []CollisionMember{m},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("collisions: %w", err)
	}

	for i := range out {
		out[i].SameSize = sameSize(out[i].Members)
	}
	return out, nil
}

/*
 * sameSize reports whether every member states the same byte count.
 *
 * An unknown size is not a match. A row with no `size_bytes` has never been
 * probed or its file has gone; either way "these are the same size" is a claim
 * the data does not support, and reporting it would be the report inventing
 * evidence.
 */
func sameSize(members []CollisionMember) bool {
	if len(members) < 2 {
		return false
	}
	first := members[0].SizeBytes
	if first == nil {
		return false
	}
	for _, m := range members[1:] {
		if m.SizeBytes == nil || *m.SizeBytes != *first {
			return false
		}
	}
	return true
}
