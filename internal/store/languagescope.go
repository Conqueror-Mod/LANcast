package store

import (
	"context"
	"fmt"
)

/*
 * Re-asking about every TMDB-identified title, for a new metadata language
 * (ADR 0122).
 *
 * The `settled` scope's door, wider. That scope fetches each row from its own
 * recorded provider id and applies the result under the row's locks, without
 * searching, scoring or moving a match state — and it covers locked rows only,
 * because an ordinary refresh already reaches matched ones. An ordinary
 * refresh searches and scores, though, and a change of language is not news
 * about anybody's identity. It is news about every identified title's text,
 * so this scope takes both matched and locked rows through the by-id door.
 *
 * `review` is left out on purpose: a row in review carries the id of a
 * candidate nobody has confirmed, and fetching by it would fill the row with a
 * suggestion's text as though it had been chosen.
 *
 * TMDB only, because TMDB is the provider whose answers depend on the
 * language. MusicBrainz names an album the way the record does, whatever
 * language anybody asks in.
 */

const languageProvider = "tmdb"

func (s *Store) languageWhere() string {
	return `
	WHERE library_id = ? AND missing = 0
	  AND match_state IN ('matched', 'locked')
	  AND provider = '` + languageProvider + `'
	  AND external_id IS NOT NULL AND TRIM(external_id) <> ''
	  AND ` + s.enrichableKinds()
}

// LanguageCount prices a language refresh of one library without performing it.
func (s *Store) LanguageCount(ctx context.Context, libraryID int64) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM media_item`+s.languageWhere(), libraryID).Scan(&n); err != nil {
		return 0, fmt.Errorf("language count: %w", err)
	}
	return n, nil
}

// LanguageItems lists the rows a language refresh will fetch again, by id.
func (s *Store) LanguageItems(ctx context.Context, libraryID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM media_item`+s.languageWhere()+` ORDER BY id`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("language items: %w", err)
	}
	defer rows.Close()

	var out []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("language items: %w", err)
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}
