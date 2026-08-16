package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

/*
 * Live TV channels (schema 23).
 *
 * A channel is deliberately **not** a `media_item`. Every column on that table
 * describes a *work* — a title a provider could match, a duration, a file on
 * disk, a position you stopped at — and a channel has none of them. It is a
 * name, a logo and a URL whose contents are different every time you look.
 *
 * ADR 0002 chose one wide table for things that are works; this is the case
 * that is not one, and putting it there would mean six nullable columns, a new
 * `kind` every listing has to learn to exclude, and a row answering "how long is
 * it" with nothing.
 */

// ChannelSource is a channel list and where it came from.
type ChannelSource struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// URL is the playlist this source was imported from, kept so it can be
	// refreshed. A provider list changes weekly and re-typing the URL is not a
	// refresh workflow.
	URL          string `json:"url"`
	CreatedAt    int64  `json:"created_at"`
	RefreshedAt  *int64 `json:"refreshed_at"`
	ChannelCount int    `json:"channel_count"`
}

// Channel is one entry.
type Channel struct {
	ID       int64   `json:"id"`
	SourceID int64   `json:"source_id"`
	Name     string  `json:"name"`
	LogoURL  *string `json:"logo_url"`
	Group    *string `json:"group"`
	Position int     `json:"position"`
	// URL is never sent to clients. A provider playlist is frequently a
	// credentialed URL — a token in the path, or a username and password in the
	// query — and publishing it to every browser on the LAN would hand out the
	// subscription. Clients play through the server instead.
	URL string `json:"-"`
}

func (s *Store) CreateChannelSource(ctx context.Context, name, url string) (*ChannelSource, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO channel_source (name, url, created_at) VALUES (?, ?, ?)`,
		name, url, now)
	if err != nil {
		return nil, fmt.Errorf("create channel source: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("create channel source: %w", err)
	}
	return &ChannelSource{ID: id, Name: name, URL: url, CreatedAt: now}, nil
}

func (s *Store) ListChannelSources(ctx context.Context) ([]ChannelSource, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, url, created_at, refreshed_at, channel_count
		 FROM channel_source ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list channel sources: %w", err)
	}
	defer rows.Close()

	out := []ChannelSource{}
	for rows.Next() {
		var c ChannelSource
		if err := rows.Scan(&c.ID, &c.Name, &c.URL, &c.CreatedAt, &c.RefreshedAt, &c.ChannelCount); err != nil {
			return nil, fmt.Errorf("list channel sources: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetChannelSource(ctx context.Context, id int64) (*ChannelSource, error) {
	var c ChannelSource
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, url, created_at, refreshed_at, channel_count
		 FROM channel_source WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &c.URL, &c.CreatedAt, &c.RefreshedAt, &c.ChannelCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get channel source: %w", err)
	}
	return &c, nil
}

/*
 * ReplaceChannels swaps a source's channels for a freshly imported set, in one
 * transaction.
 *
 * Replace rather than merge, and that is the decision: a channel list is a
 * *snapshot* published by somebody else, not a collection somebody curates
 * here. Merging would mean deciding what identity a channel has across two
 * versions of a file that carries no stable id worth trusting — `tvg-id` is
 * optional and frequently absent — and the result of guessing wrong is a
 * duplicate of every channel on every refresh.
 *
 * The transaction matters because the alternative is a window where a refresh
 * has deleted the old channels and not yet written the new ones, and anybody
 * looking at the page in that moment sees an empty provider.
 */
func (s *Store) ReplaceChannels(ctx context.Context, sourceID int64, chans []Channel) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("replace channels: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM channel WHERE source_id = ?`, sourceID); err != nil {
		return fmt.Errorf("replace channels: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO channel (source_id, name, url, logo_url, group_name, position)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("replace channels: %w", err)
	}
	defer stmt.Close()

	for i, c := range chans {
		if _, err := stmt.ExecContext(ctx, sourceID, c.Name, c.URL,
			nullable(c.LogoURL), nullable(c.Group), i); err != nil {
			return fmt.Errorf("replace channels: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE channel_source SET channel_count = ?, refreshed_at = ? WHERE id = ?`,
		len(chans), time.Now().Unix(), sourceID); err != nil {
		return fmt.Errorf("replace channels: %w", err)
	}
	return tx.Commit()
}

// ListChannels returns a source's channels in the order the source listed them,
// or every channel when sourceID is zero. Source order is preserved because it
// is meaningful to whoever curated the list, and alphabetical is not an
// improvement on "the order the channels are on the remote control".
func (s *Store) ListChannels(ctx context.Context, sourceID int64) ([]Channel, error) {
	q := `SELECT id, source_id, name, url, logo_url, group_name, position FROM channel`
	args := []any{}
	if sourceID != 0 {
		q += ` WHERE source_id = ?`
		args = append(args, sourceID)
	}
	q += ` ORDER BY source_id, position, id`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer rows.Close()

	out := []Channel{}
	for rows.Next() {
		var c Channel
		if err := rows.Scan(&c.ID, &c.SourceID, &c.Name, &c.URL, &c.LogoURL, &c.Group, &c.Position); err != nil {
			return nil, fmt.Errorf("list channels: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetChannel(ctx context.Context, id int64) (*Channel, error) {
	var c Channel
	err := s.db.QueryRowContext(ctx,
		`SELECT id, source_id, name, url, logo_url, group_name, position
		 FROM channel WHERE id = ?`, id).
		Scan(&c.ID, &c.SourceID, &c.Name, &c.URL, &c.LogoURL, &c.Group, &c.Position)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get channel: %w", err)
	}
	return &c, nil
}

// DeleteChannelSource removes a source and, by cascade, its channels.
func (s *Store) DeleteChannelSource(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM channel_source WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete channel source: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func nullable(s *string) any {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}
