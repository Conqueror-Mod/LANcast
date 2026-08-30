package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

/*
 * Backfilling the edition marker onto rows that predate it.
 *
 * `edition` arrived at schema revision 29 and is written by UpsertItem — and
 * the scanner only upserts a file whose size or mtime changed. Every row older
 * than the column therefore reads NULL for ever, because nothing about those
 * files will move again. Measured against the reporting library: 1,229 movie
 * rows, and `SELECT COUNT(*) FROM media_item WHERE edition IS NOT NULL` was
 * **zero**. The marker shipped, is correct, and was inert on every library that
 * existed before it (ADR 0049).
 *
 * Re-parse does not rescue it. `Guess` carries title, year, series, season and
 * episode and not edition, and ReparseTargets only offers 'review' and
 * 'unmatched' rows — while the case that motivated this is 'locked'.
 *
 * # NULL keeps meaning "no marker"
 *
 * That is the existing contract: `Edition` is `*string` precisely so that a
 * film with no marker stores nothing rather than an empty string (ADR 0042),
 * and the scanner's own test asserts it.
 *
 * The cost is that this pass cannot tell "no marker" from "never looked", so it
 * re-reads every unmarked film on every scan instead of converging. That was
 * worth an alternative design until it was priced: the candidate rows come back
 * in one indexed query, `media.Parse` is pure string work, and a film with no
 * marker is *not written* — so the repeated cost is a single SELECT and a few
 * milliseconds of parsing, and no write amplification at all. Inventing a
 * third state to avoid it would change a documented API contract to save that.
 */

// EditionTarget is one row whose edition column is empty and whose filename has
// therefore never been credited with a marker.
type EditionTarget struct {
	ItemID  int64
	Path    string
	Root    string
	LibKind string
}

/*
 * EditionBackfillTargets lists rows that carry no edition marker.
 *
 * Movies only. An edition is a claim about which cut of a film a file holds,
 * and the parser's vocabulary is built for film naming; an episode named
 * "S01E02 EE" is far more likely to be a release tag than an extended edition.
 * Episodes therefore never appear here, which costs nothing because the
 * predicate simply does not match them.
 *
 * Unlike ReparseTargets this deliberately does **not** filter on match_state.
 * That exclusion protects *titles* — a provider title is better evidence than a
 * filename — and an edition marker is not a title. No provider knows which cut
 * a particular file holds; only the filename records it. So a 'matched' row and
 * a 'locked' one are both allowed here, and per-field locks remain the thing
 * that stops a person's own edit being overwritten.
 */
func (s *Store) EditionBackfillTargets(ctx context.Context, libraryID int64) ([]EditionTarget, error) {
	args := []any{}
	where := ` WHERE i.edition IS NULL AND i.missing = 0 AND i.kind = 'movie'`
	if libraryID != 0 {
		where += ` AND i.library_id = ?`
		args = append(args, libraryID)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.path, COALESCE(r.path, ''), l.kind
		  FROM media_item i
		  JOIN library l ON l.id = i.library_id
		  LEFT JOIN library_root r ON r.id = i.root_id`+where, args...)
	if err != nil {
		return nil, fmt.Errorf("edition backfill targets: %w", err)
	}
	defer rows.Close()

	out := []EditionTarget{}
	for rows.Next() {
		var t EditionTarget
		if err := rows.Scan(&t.ItemID, &t.Path, &t.Root, &t.LibKind); err != nil {
			return nil, fmt.Errorf("edition backfill targets: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

/*
 * SetEdition records which cut of a film this file holds.
 *
 * Only ever called with a marker the parser actually found. An empty marker is
 * rejected rather than written, because NULL already means "no marker" and
 * writing an empty string would put a second spelling of that into the column
 * for the API to disagree with itself over.
 *
 * A locked `edition` is skipped: a person who edited this field owns it
 * (CLAUDE.md), and a filename guess is exactly what the lock exists to keep
 * out.
 *
 * Nothing else about the row is touched. In particular metadata_updated_at is
 * left alone, because ApplyGuess clears it to requeue enrichment and that is
 * precisely wrong here — an edition marker is not a better question to ask a
 * provider, it is an answer no provider has. Requeueing to ask again would turn
 * a repair into a provider-quota event.
 */
func (s *Store) SetEdition(ctx context.Context, itemID int64, edition string) (bool, error) {
	if edition == "" {
		return false, nil
	}

	locked, err := s.LockedFields(ctx, itemID)
	if err != nil {
		return false, err
	}
	for _, f := range locked {
		if f == "edition" {
			return false, nil
		}
	}

	var cur sql.NullString
	err = s.db.QueryRowContext(ctx,
		`SELECT edition FROM media_item WHERE id = ?`, itemID).Scan(&cur)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("set edition: %w", err)
	}
	if cur.Valid && cur.String == edition {
		return false, nil
	}

	if _, err := s.db.ExecContext(ctx,
		`UPDATE media_item SET edition = ?, updated_at = ? WHERE id = ?`,
		edition, time.Now().Unix(), itemID); err != nil {
		return false, fmt.Errorf("set edition: %w", err)
	}
	return true, nil
}
