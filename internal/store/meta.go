package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ------------------------------------------------------------------- locks

// LockedFields returns the field names locked on an item.
func (s *Store) LockedFields(ctx context.Context, itemID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT field FROM item_lock WHERE item_id = ? ORDER BY field`, itemID)
	if err != nil {
		return nil, fmt.Errorf("locked fields: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, fmt.Errorf("locked fields: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// LockField marks a field as user-owned. Idempotent.
func (s *Store) LockField(ctx context.Context, itemID int64, field string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO item_lock (item_id, field) VALUES (?, ?)`, itemID, field)
	if err != nil {
		return fmt.Errorf("lock field %q: %w", field, err)
	}
	return nil
}

// UnlockField releases a lock so the field resumes updating.
func (s *Store) UnlockField(ctx context.Context, itemID int64, field string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM item_lock WHERE item_id = ? AND field = ?`, itemID, field)
	if err != nil {
		return fmt.Errorf("unlock field %q: %w", field, err)
	}
	return nil
}

// ------------------------------------------------------------------ metadata

// ItemMetadata is the writable metadata for one item. Nil fields are left
// unchanged, so callers pass only what the merge engine resolved.
type ItemMetadata struct {
	Title         *string
	SortTitle     *string
	Year          *int
	Overview      *string
	Rating        *float64
	ContentRating *string
	ReleasedAt    *int64
	DurationMS    *int64
	Series        *string
	Season        *int
	Episode       *int

	Provider   *string
	ExternalID *string
	MatchState *string
	MatchScore *float64
	IMDbID     *string
}

// UpdateItemMetadata writes resolved metadata and stamps metadata_updated_at,
// which is what removes the item from the enrichment queue.
func (s *Store) UpdateItemMetadata(ctx context.Context, itemID int64, m ItemMetadata) error {
	set := []string{"metadata_updated_at = ?"}
	args := []any{time.Now().Unix()}

	add := func(col string, v any, isNil bool) {
		if isNil {
			return
		}
		set = append(set, col+" = ?")
		args = append(args, v)
	}
	add("title", derefStr(m.Title), m.Title == nil)
	add("sort_title", derefStr(m.SortTitle), m.SortTitle == nil)
	add("year", derefInt(m.Year), m.Year == nil)
	add("overview", derefStr(m.Overview), m.Overview == nil)
	add("rating", derefFloat(m.Rating), m.Rating == nil)
	add("content_rating", derefStr(m.ContentRating), m.ContentRating == nil)
	add("released_at", derefInt64(m.ReleasedAt), m.ReleasedAt == nil)
	add("duration_ms", derefInt64(m.DurationMS), m.DurationMS == nil)
	add("series", derefStr(m.Series), m.Series == nil)
	add("season", derefInt(m.Season), m.Season == nil)
	add("episode", derefInt(m.Episode), m.Episode == nil)
	add("provider", derefStr(m.Provider), m.Provider == nil)
	add("external_id", derefStr(m.ExternalID), m.ExternalID == nil)
	add("match_state", derefStr(m.MatchState), m.MatchState == nil)
	add("match_score", derefFloat(m.MatchScore), m.MatchScore == nil)
	add("imdb_id", derefStr(m.IMDbID), m.IMDbID == nil)

	q := `UPDATE media_item SET ` + join(set, ", ") + `, updated_at = ? WHERE id = ?`
	args = append(args, time.Now().Unix(), itemID)

	if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("update item metadata: %w", err)
	}
	return nil
}

// SetMatch records a confirmed identity. A locked match is never re-scored or
// re-searched: rescans reconcile files, they do not re-litigate identity.
func (s *Store) SetMatch(ctx context.Context, itemID int64, provider, externalID, state string, score float64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE media_item
		SET provider = ?, external_id = ?, match_state = ?, match_score = ?, updated_at = ?
		WHERE id = ?`,
		provider, externalID, state, score, time.Now().Unix(), itemID)
	if err != nil {
		return fmt.Errorf("set match: %w", err)
	}
	return nil
}

// -------------------------------------------------------------- external ratings

// ItemRating is one third-party score for an item (ADR 0019). Score is
// normalized to 0–10 so it sorts and compares uniformly; Display keeps the
// source-native form ("92%", "74") so the UI renders each in its own scale.
type ItemRating struct {
	Source    string  `json:"source"`
	Score     float64 `json:"score"`
	Display   string  `json:"display"`
	Votes     int     `json:"votes,omitempty"`
	UpdatedAt int64   `json:"-"`
}

// SaveRatings upserts an item's scores, one row per source. A source already
// present is replaced (a refresh re-fetches it); sources not in the set are left
// untouched, so two rating sources writing independently do not clobber each
// other.
func (s *Store) SaveRatings(ctx context.Context, itemID int64, ratings []ItemRating) error {
	if len(ratings) == 0 {
		return nil
	}
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save ratings: %w", err)
	}
	defer tx.Rollback()
	for _, r := range ratings {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO item_rating (item_id, source, score, display, votes, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(item_id, source) DO UPDATE SET
				score = excluded.score, display = excluded.display,
				votes = excluded.votes, updated_at = excluded.updated_at`,
			itemID, r.Source, r.Score, r.Display, r.Votes, now); err != nil {
			return fmt.Errorf("save ratings: %w", err)
		}
	}
	return tx.Commit()
}

// ItemRatings returns an item's third-party scores, highest normalized score
// first so the UI leads with the strongest.
func (s *Store) ItemRatings(ctx context.Context, itemID int64) ([]ItemRating, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT source, score, display, votes, updated_at
		FROM item_rating WHERE item_id = ?
		ORDER BY score DESC, source`, itemID)
	if err != nil {
		return nil, fmt.Errorf("item ratings: %w", err)
	}
	defer rows.Close()
	out := []ItemRating{}
	for rows.Next() {
		var r ItemRating
		if err := rows.Scan(&r.Source, &r.Score, &r.Display, &r.Votes, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("item ratings: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PendingEnrichment returns items awaiting metadata. The queue is a query
// rather than a table, which makes it restart-safe by construction.
func (s *Store) PendingEnrichment(ctx context.Context, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM media_item
		WHERE metadata_updated_at IS NULL AND missing = 0 AND match_state != 'locked'
		ORDER BY added_at LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("pending enrichment: %w", err)
	}
	defer rows.Close()

	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("pending enrichment: %w", err)
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// PendingCount is how many items still await enrichment.
//
// The worker's batch length is not this number — reporting the batch size as
// "remaining" tells the user 50 when there are 3000, and keeps saying 25 after
// the work is finished.
func (s *Store) PendingCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM media_item
		WHERE metadata_updated_at IS NULL AND missing = 0 AND match_state != 'locked'`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("pending count: %w", err)
	}
	return n, nil
}

// ClearMetadataStamp requeues items for enrichment. Locked fields still survive
// the refresh — this only schedules the work.
func (s *Store) ClearMetadataStamp(ctx context.Context, libraryID int64, itemID int64) error {
	var err error
	switch {
	case itemID != 0:
		_, err = s.db.ExecContext(ctx,
			`UPDATE media_item SET metadata_updated_at = NULL WHERE id = ?`, itemID)
	default:
		_, err = s.db.ExecContext(ctx,
			`UPDATE media_item SET metadata_updated_at = NULL WHERE library_id = ?`, libraryID)
	}
	if err != nil {
		return fmt.Errorf("clear metadata stamp: %w", err)
	}
	return nil
}

// ReviewQueue returns items whose identity is uncertain. Applying a
// low-confidence match is a good default, but it is recorded as uncertain
// rather than presented as fact.
//
// Only items that have actually been through enrichment qualify.
// match_state defaults to 'unmatched', so without the metadata_updated_at
// check every freshly scanned item would appear here — reporting "no match
// found" for titles nothing has looked at yet. That is the same
// not-attempted versus no-answer distinction the enrichment worker makes.
func (s *Store) ReviewQueue(ctx context.Context, libraryID int64, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 100
	}
	args := []any{}
	where := ` WHERE match_state IN ('review','unmatched') AND missing = 0
		AND metadata_updated_at IS NOT NULL`
	if libraryID != 0 {
		where += ` AND library_id = ?`
		args = append(args, libraryID)
	}
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM media_item`+where+` ORDER BY match_score DESC, sort_title LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("review queue: %w", err)
	}
	defer rows.Close()

	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("review queue: %w", err)
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// -------------------------------------------------------------------- genres

// ReplaceGenres sets an item's genres, creating genre rows as needed.
func (s *Store) ReplaceGenres(ctx context.Context, itemID int64, names []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("replace genres: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM item_genre WHERE item_id = ?`, itemID); err != nil {
		return fmt.Errorf("replace genres: %w", err)
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO genre (name) VALUES (?)`, name); err != nil {
			return fmt.Errorf("replace genres: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO item_genre (item_id, genre_id)
			VALUES (?, (SELECT id FROM genre WHERE name = ?))`, itemID, name); err != nil {
			return fmt.Errorf("replace genres: %w", err)
		}
	}
	return tx.Commit()
}

// Genres returns an item's genre names.
func (s *Store) Genres(ctx context.Context, itemID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.name FROM genre g
		JOIN item_genre ig ON ig.genre_id = g.id
		WHERE ig.item_id = ? ORDER BY g.name`, itemID)
	if err != nil {
		return nil, fmt.Errorf("genres: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("genres: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------------- credits

// ReplaceCredits sets an item's cast and crew.
func (s *Store) ReplaceCredits(ctx context.Context, itemID int64, provider string, credits []Credit) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("replace credits: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM credit WHERE item_id = ?`, itemID); err != nil {
		return fmt.Errorf("replace credits: %w", err)
	}
	for i, c := range credits {
		if c.Name == "" {
			continue
		}
		// People are keyed by name when the provider gives no id; good enough
		// until a provider that supplies stable person ids is added.
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO person (provider, external_id, name) VALUES (?, ?, ?)`,
			provider, c.Name, c.Name); err != nil {
			return fmt.Errorf("replace credits: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO credit (item_id, person_id, role, character, ord)
			VALUES (?, (SELECT id FROM person WHERE provider = ? AND external_id = ?), ?, ?, ?)`,
			itemID, provider, c.Name, c.Role, c.Character, i); err != nil {
			return fmt.Errorf("replace credits: %w", err)
		}
	}
	return tx.Commit()
}

// Credits returns an item's cast and crew in billing order.
func (s *Store) Credits(ctx context.Context, itemID int64) ([]Credit, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.name, c.role, COALESCE(c.character, ''), c.ord
		FROM credit c JOIN person p ON p.id = c.person_id
		WHERE c.item_id = ? ORDER BY c.ord`, itemID)
	if err != nil {
		return nil, fmt.Errorf("credits: %w", err)
	}
	defer rows.Close()

	out := []Credit{}
	for rows.Next() {
		var c Credit
		if err := rows.Scan(&c.Name, &c.Role, &c.Character, &c.Order); err != nil {
			return nil, fmt.Errorf("credits: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------------- artwork

// PutArtwork records a stored image and links it to an item, returning the
// artwork row id. Content-addressed, so re-storing the same bytes is a no-op.
func (s *Store) PutArtwork(ctx context.Context, itemID int64, hash, kind, sourceURL string, w, h int, size int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("put artwork: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artwork (hash, kind, source_url, width, height, bytes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(hash) DO NOTHING`,
		hash, kind, sourceURL, w, h, size, time.Now().Unix()); err != nil {
		return fmt.Errorf("put artwork: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO item_artwork (item_id, artwork_id, kind, selected)
		VALUES (?, (SELECT id FROM artwork WHERE hash = ?), ?, 1)`,
		itemID, hash, kind); err != nil {
		return fmt.Errorf("put artwork: %w", err)
	}
	return tx.Commit()
}

// ItemArtwork returns the selected image hashes for an item.
func (s *Store) ItemArtwork(ctx context.Context, itemID int64) (*Artwork, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ia.kind, a.hash FROM item_artwork ia
		JOIN artwork a ON a.id = ia.artwork_id
		WHERE ia.item_id = ? AND ia.selected = 1`, itemID)
	if err != nil {
		return nil, fmt.Errorf("item artwork: %w", err)
	}
	defer rows.Close()

	var art Artwork
	var any bool
	for rows.Next() {
		var kind, hash string
		if err := rows.Scan(&kind, &hash); err != nil {
			return nil, fmt.Errorf("item artwork: %w", err)
		}
		any = true
		switch kind {
		case "poster":
			art.Poster = hash
		case "fanart":
			art.Fanart = hash
		case "thumb":
			art.Thumb = hash
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !any {
		return nil, nil
	}
	return &art, nil
}

// AttachArtwork fills in Artwork for a page of items in one query.
//
// The grid renders from the list endpoint, so without this every poster is
// downloaded, stored, and then never shown. Doing it per item would be one
// query per tile, which is exactly the N+1 the batch shape exists to avoid.
func (s *Store) AttachArtwork(ctx context.Context, items []Item) error {
	if len(items) == 0 {
		return nil
	}

	placeholders := make([]byte, 0, len(items)*2)
	args := make([]any, 0, len(items))
	for i := range items {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, items[i].ID)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT ia.item_id, ia.kind, a.hash
		FROM item_artwork ia
		JOIN artwork a ON a.id = ia.artwork_id
		WHERE ia.selected = 1 AND ia.item_id IN (`+string(placeholders)+`)`, args...)
	if err != nil {
		return fmt.Errorf("attach artwork: %w", err)
	}
	defer rows.Close()

	byID := map[int64]*Artwork{}
	for rows.Next() {
		var id int64
		var kind, hash string
		if err := rows.Scan(&id, &kind, &hash); err != nil {
			return fmt.Errorf("attach artwork: %w", err)
		}
		art, ok := byID[id]
		if !ok {
			art = &Artwork{}
			byID[id] = art
		}
		switch kind {
		case "poster":
			art.Poster = hash
		case "fanart":
			art.Fanart = hash
		case "thumb":
			art.Thumb = hash
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range items {
		if art, ok := byID[items[i].ID]; ok {
			items[i].Artwork = art
		}
	}
	return nil
}

// ArtworkExists reports whether a hash is already stored, so a download can be
// skipped.
func (s *Store) ArtworkExists(ctx context.Context, hash string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artwork WHERE hash = ?`, hash).Scan(&n)
	return n > 0, err
}

// ------------------------------------------------------------ provider cache

// CachedResponse returns a cached provider payload newer than maxAge.
func (s *Store) CachedResponse(ctx context.Context, provider, key string, maxAge time.Duration) ([]byte, bool, error) {
	var payload []byte
	var fetched int64
	err := s.db.QueryRowContext(ctx,
		`SELECT payload, fetched_at FROM provider_cache WHERE provider = ? AND key = ?`,
		provider, key).Scan(&payload, &fetched)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cached response: %w", err)
	}
	if maxAge > 0 && time.Since(time.Unix(fetched, 0)) > maxAge {
		return nil, false, nil
	}
	return payload, true, nil
}

// CacheResponse stores a raw provider payload.
func (s *Store) CacheResponse(ctx context.Context, provider, key string, payload []byte) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_cache (provider, key, payload, fetched_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(provider, key) DO UPDATE SET payload = excluded.payload, fetched_at = excluded.fetched_at`,
		provider, key, payload, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("cache response: %w", err)
	}
	return nil
}

// ------------------------------------------------------------------- helpers

// LoadDetail attaches the detail-only fields to an item.
func (s *Store) LoadDetail(ctx context.Context, it *Item) error {
	var err error
	if it.Genres, err = s.Genres(ctx, it.ID); err != nil {
		return err
	}
	if it.Credits, err = s.Credits(ctx, it.ID); err != nil {
		return err
	}
	if it.Artwork, err = s.ItemArtwork(ctx, it.ID); err != nil {
		return err
	}
	if it.LockedFields, err = s.LockedFields(ctx, it.ID); err != nil {
		return err
	}
	return nil
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
func derefFloat(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func join(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}
