package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ExternalSubtitle is a subtitle file on disk belonging to an item.
type ExternalSubtitle struct {
	ID       int64  `json:"id"`
	ItemID   int64  `json:"item_id"`
	Path     string `json:"-"` // never serialized, like media paths
	Language string `json:"language,omitempty"`
	Title    string `json:"title,omitempty"`
	Forced   bool   `json:"forced"`
	Format   string `json:"format"`
	Source   string `json:"source"`
}

// ReplaceSidecarSubtitles refreshes the sidecar files known for an item.
//
// Only rows with source 'sidecar' are replaced: a downloaded subtitle is not
// something a rescan should delete because it happens not to be on disk beside
// the video under the expected name.
func (s *Store) ReplaceSidecarSubtitles(ctx context.Context, itemID int64, subs []ExternalSubtitle) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("replace sidecars: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM external_subtitle WHERE item_id = ? AND source = 'sidecar'`, itemID); err != nil {
		return fmt.Errorf("replace sidecars: %w", err)
	}

	now := time.Now().Unix()
	for _, sub := range subs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO external_subtitle (item_id, path, language, title, forced, format, source, added_at)
			VALUES (?, ?, ?, ?, ?, ?, 'sidecar', ?)
			ON CONFLICT(item_id, path) DO UPDATE SET
				language = excluded.language, title = excluded.title,
				forced = excluded.forced, format = excluded.format`,
			itemID, sub.Path, nullEmpty(sub.Language), nullEmpty(sub.Title),
			boolInt(sub.Forced), sub.Format, now); err != nil {
			return fmt.Errorf("replace sidecars: %w", err)
		}
	}
	return tx.Commit()
}

// AddSubtitle records a subtitle that was downloaded rather than found.
func (s *Store) AddSubtitle(ctx context.Context, sub ExternalSubtitle) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO external_subtitle (item_id, path, language, title, forced, format, source, added_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(item_id, path) DO UPDATE SET
			language = excluded.language, title = excluded.title, forced = excluded.forced`,
		sub.ItemID, sub.Path, nullEmpty(sub.Language), nullEmpty(sub.Title),
		boolInt(sub.Forced), sub.Format, sub.Source, time.Now().Unix())
	if err != nil {
		return 0, fmt.Errorf("add subtitle: %w", err)
	}
	return res.LastInsertId()
}

// DeleteExternalSubtitle removes one external subtitle row, scoped to its item
// so a crafted id cannot delete another item's track. It does not touch files
// on disk: whether the backing file is the server's to remove is the caller's
// decision, since a sidecar lives in the user's library and a download does not.
func (s *Store) DeleteExternalSubtitle(ctx context.Context, itemID, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM external_subtitle WHERE id = ? AND item_id = ?`, id, itemID)
	if err != nil {
		return fmt.Errorf("delete subtitle: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ExternalSubtitles returns an item's subtitle files.
func (s *Store) ExternalSubtitles(ctx context.Context, itemID int64) ([]ExternalSubtitle, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, item_id, path, COALESCE(language,''), COALESCE(title,''), forced, format, source
		FROM external_subtitle WHERE item_id = ?
		ORDER BY forced, language, id`, itemID)
	if err != nil {
		return nil, fmt.Errorf("external subtitles: %w", err)
	}
	defer rows.Close()

	out := []ExternalSubtitle{}
	for rows.Next() {
		var sub ExternalSubtitle
		var forced int
		if err := rows.Scan(&sub.ID, &sub.ItemID, &sub.Path, &sub.Language,
			&sub.Title, &forced, &sub.Format, &sub.Source); err != nil {
			return nil, fmt.Errorf("external subtitles: %w", err)
		}
		sub.Forced = forced != 0
		out = append(out, sub)
	}
	return out, rows.Err()
}

// ExternalSubtitle looks up one subtitle by id, scoped to its item so a
// crafted id cannot read a file belonging to something else.
func (s *Store) ExternalSubtitle(ctx context.Context, itemID, id int64) (*ExternalSubtitle, error) {
	var sub ExternalSubtitle
	var forced int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, item_id, path, COALESCE(language,''), COALESCE(title,''), forced, format, source
		FROM external_subtitle WHERE id = ? AND item_id = ?`, id, itemID).
		Scan(&sub.ID, &sub.ItemID, &sub.Path, &sub.Language, &sub.Title,
			&forced, &sub.Format, &sub.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("external subtitle: %w", err)
	}
	sub.Forced = forced != 0
	return &sub, nil
}
