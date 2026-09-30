package store

import (
	"context"
	"database/sql"
	"fmt"
)

/*
 * The watch history: one row per finished viewing (ADR 0074).
 *
 * playback_state answers "where was I" and keeps one row per item, so it can
 * only ever say when something was *last* played. This log is the other half:
 * every time a film or an episode is finished, a row, kept until the account
 * clears it.
 *
 * Private to one account like everything keyed by user (ADR 0006). Nothing in
 * here takes an account from anywhere but its caller's session, one layer up.
 */

// Viewing is one finished viewing, with what was watched copied onto it so the
// row still reads after the item is deleted.
type Viewing struct {
	ID         int64 `json:"id"`
	FinishedAt int64 `json:"finished_at"`
	// Estimated marks a row seeded from the old state table when the log began:
	// its date is the last time the item's state was written, the best the old
	// table can give, and it may be later than the viewing was.
	Estimated bool `json:"estimated"`
	// ItemID is null once the item has been deleted from the library; the rest
	// of the row still says what it was.
	ItemID     *int64  `json:"item_id"`
	Kind       string  `json:"kind"`
	Title      string  `json:"title"`
	Year       *int    `json:"year"`
	Series     *string `json:"series"`
	Season     *int    `json:"season"`
	Episode    *int    `json:"episode"`
	IMDbID     *string `json:"imdb_id"`
	ShowYear   *int    `json:"show_year"`
	ShowIMDbID *string `json:"show_imdb_id"`
}

const viewingCols = `id, finished_at, estimated, item_id, kind, title, year,
	series, season, episode, imdb_id, show_year, show_imdb_id`

func scanViewing(sc interface{ Scan(...any) error }) (Viewing, error) {
	var v Viewing
	var est int
	err := sc.Scan(&v.ID, &v.FinishedAt, &est, &v.ItemID, &v.Kind, &v.Title, &v.Year,
		&v.Series, &v.Season, &v.Episode, &v.IMDbID, &v.ShowYear, &v.ShowIMDbID)
	v.Estimated = est != 0
	return v, err
}

/*
 * logViewing records that an item was just finished.
 *
 * Called from SaveProgress on the same edge that moves watch_count, inside its
 * transaction, so the tally and the log can never disagree about what counted
 * as a viewing. Films and episodes only: the INSERT selects nothing for any
 * other kind, so music and photographs are skipped without a branch here.
 */
func logViewing(ctx context.Context, tx *sql.Tx, userID string, itemID, at int64) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO viewing (user_id, item_id, finished_at, kind, title, year,
		                     series, season, episode, imdb_id, show_year, show_imdb_id)
		SELECT ?, mi.id, ?, mi.kind, mi.title, mi.year,
		       mi.series, mi.season, mi.episode, mi.imdb_id, sh.year, sh.imdb_id
		FROM media_item mi
		LEFT JOIN media_item sh ON sh.id = (`+showOf+`)
		WHERE mi.id = ? AND mi.kind IN ('movie', 'episode')`,
		userID, at, itemID)
	if err != nil {
		return fmt.Errorf("log viewing: %w", err)
	}
	return nil
}

// Viewings returns one page of the account's history, newest first, and how
// many rows it holds in all. Ties on the second are broken on id, so paging
// never shows a row twice or skips one.
func (s *Store) Viewings(ctx context.Context, userID string, limit, offset int) ([]Viewing, int, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM viewing WHERE user_id = ?`, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("viewings: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+viewingCols+` FROM viewing
		WHERE user_id = ? ORDER BY finished_at DESC, id DESC LIMIT ? OFFSET ?`,
		userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("viewings: %w", err)
	}
	defer rows.Close()
	out := []Viewing{}
	for rows.Next() {
		v, err := scanViewing(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("viewings: %w", err)
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

// AllViewings returns the whole history, oldest first, for an export. A
// finished-viewings log is small — years of heavy watching is a few thousand
// rows — so it is read whole rather than streamed.
func (s *Store) AllViewings(ctx context.Context, userID string) ([]Viewing, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+viewingCols+` FROM viewing
		WHERE user_id = ? ORDER BY finished_at, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("all viewings: %w", err)
	}
	defer rows.Close()
	out := []Viewing{}
	for rows.Next() {
		v, err := scanViewing(rows)
		if err != nil {
			return nil, fmt.Errorf("all viewings: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
