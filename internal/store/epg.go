package store

import (
	"context"
	"fmt"
	"time"
)

/*
 * The guide (schema 24).
 *
 * Three reads, and only three, because they are the three questions a guide is
 * ever asked: what is on now, what is on next, and what is on this channel
 * today. Everything the UI shows is one of those, and a general "query the
 * schedule" method would be a query builder for a table nobody queries freely.
 *
 * Listings are replaced wholesale rather than merged. A guide is a snapshot
 * published by somebody else — same argument as the channel list, and stronger
 * here: XMLTV programmes carry no id at all, so "the same programme, updated"
 * is not a thing this data can express. A refresh that merged would double
 * every entry.
 */

// Program is one listing.
type Program struct {
	ID          int64   `json:"id"`
	ChannelID   int64   `json:"channel_id"`
	StartAt     int64   `json:"start_at"`
	StopAt      int64   `json:"stop_at"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Category    *string `json:"category"`
	Season      *int    `json:"season"`
	Episode     *int    `json:"episode"`
	IconURL     *string `json:"icon_url"`
}

/*
 * ReplaceProgramsForSource swaps every listing belonging to a source's channels.
 *
 * Scoped to the source rather than to the whole table: two providers can each
 * publish a guide, and refreshing one must not blank the other. The delete is
 * expressed through `channel` because `epg_program` deliberately carries no
 * source column — a programme's source is its channel's, and a denormalised
 * copy is a second truth that can disagree.
 *
 * Programs whose channel_id is not a real channel are rejected by the foreign
 * key rather than filtered here; the importer resolves ids before calling.
 */
func (s *Store) ReplaceProgramsForSource(ctx context.Context, sourceID int64, progs []Program) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("replace programs: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM epg_program WHERE channel_id IN
		   (SELECT id FROM channel WHERE source_id = ?)`, sourceID); err != nil {
		return 0, fmt.Errorf("replace programs: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO epg_program
		  (channel_id, start_at, stop_at, title, description, category, season, episode, icon_url)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("replace programs: %w", err)
	}
	defer stmt.Close()

	for _, p := range progs {
		if _, err := stmt.ExecContext(ctx, p.ChannelID, p.StartAt, p.StopAt, p.Title,
			nullable(p.Description), nullable(p.Category),
			nullableInt(p.Season), nullableInt(p.Episode), nullable(p.IconURL)); err != nil {
			return 0, fmt.Errorf("replace programs: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE channel_source SET program_count = ?, epg_refreshed_at = ? WHERE id = ?`,
		len(progs), time.Now().Unix(), sourceID); err != nil {
		return 0, fmt.Errorf("replace programs: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("replace programs: %w", err)
	}
	return len(progs), nil
}

// ChannelTvgIDs maps a source's channels by their XMLTV id, for resolving a
// guide at import. Channels with no `tvg-id` are absent, which is what makes
// them unmatchable — see ADR 0031 on why a name match is refused.
//
// Lower-cased, because providers publish `bbcone.uk` in the playlist and
// `BBCOne.uk` in the guide often enough that a case-sensitive join loses whole
// channel lineups to a shift key.
func (s *Store) ChannelTvgIDs(ctx context.Context, sourceID int64) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT lower(tvg_id), id FROM channel
		 WHERE source_id = ? AND tvg_id IS NOT NULL AND tvg_id <> ''`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("channel tvg ids: %w", err)
	}
	defer rows.Close()

	out := map[string]int64{}
	for rows.Next() {
		var key string
		var id int64
		if err := rows.Scan(&key, &id); err != nil {
			return nil, fmt.Errorf("channel tvg ids: %w", err)
		}
		// First wins. A list that reuses one tvg-id across two channels ("BBC
		// One" and "BBC One HD" both tagged bbcone.uk) is common, and showing
		// the guide on the first is better than on neither.
		if _, dup := out[key]; !dup {
			out[key] = id
		}
	}
	return out, rows.Err()
}

/*
 * NowNext returns what is on and what follows, for every channel that has
 * listings, as of `at`.
 *
 * One query for the whole grid rather than one per channel. A Live TV page
 * showing six hundred tiles would otherwise issue six hundred round trips to
 * fill a strapline, and the page that does that is the page nobody opens twice.
 *
 * The window is bounded on `start_at` alone — six hours back for the "now" row,
 * which is longer than any programme this side of a test match, and forward to
 * find the "next". Bounding on `stop_at` instead would need an index this table
 * does not carry, to gain nothing.
 */
func (s *Store) NowNext(ctx context.Context, at time.Time) (map[int64][]Program, error) {
	t := at.Unix()
	const lookback = int64(6 * 60 * 60)

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, channel_id, start_at, stop_at, title, description, category, season, episode, icon_url
		FROM epg_program
		WHERE start_at BETWEEN ? AND ? AND stop_at > ?
		ORDER BY channel_id, start_at`,
		t-lookback, t+lookback, t)
	if err != nil {
		return nil, fmt.Errorf("now/next: %w", err)
	}
	defer rows.Close()

	out := map[int64][]Program{}
	for rows.Next() {
		p, err := scanProgram(rows)
		if err != nil {
			return nil, fmt.Errorf("now/next: %w", err)
		}
		// At most two per channel: the row covering `at` and the one after it.
		// Trimmed here rather than in SQL — a per-group limit in SQLite means a
		// window function over the whole table, and the window is small enough
		// that reading it and stopping is cheaper than making the planner do it.
		if len(out[p.ChannelID]) < 2 {
			out[p.ChannelID] = append(out[p.ChannelID], p)
		}
	}
	return out, rows.Err()
}

// ChannelSchedule returns one channel's listings that overlap [from, to).
// Overlap rather than containment: the programme that started before the window
// opened is the one somebody is watching, and a schedule that omits it begins
// with a hole.
func (s *Store) ChannelSchedule(ctx context.Context, channelID int64, from, to time.Time) ([]Program, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, channel_id, start_at, stop_at, title, description, category, season, episode, icon_url
		FROM epg_program
		WHERE channel_id = ? AND start_at < ? AND stop_at > ?
		ORDER BY start_at`,
		channelID, to.Unix(), from.Unix())
	if err != nil {
		return nil, fmt.Errorf("channel schedule: %w", err)
	}
	defer rows.Close()

	out := []Program{}
	for rows.Next() {
		p, err := scanProgram(rows)
		if err != nil {
			return nil, fmt.Errorf("channel schedule: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

/*
 * PruneExpiredPrograms deletes listings that finished before `before`.
 *
 * A guide is replaced on refresh, so this is not how listings normally go —
 * it is for the source nobody refreshes. Without it, a provider whose guide URL
 * lapses leaves last month's schedule in the database for ever, and the page
 * says a repeat of something is on now.
 */
func (s *Store) PruneExpiredPrograms(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM epg_program WHERE stop_at < ?`, before.Unix())
	if err != nil {
		return 0, fmt.Errorf("prune programs: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func scanProgram(sc interface{ Scan(...any) error }) (Program, error) {
	var p Program
	err := sc.Scan(&p.ID, &p.ChannelID, &p.StartAt, &p.StopAt, &p.Title,
		&p.Description, &p.Category, &p.Season, &p.Episode, &p.IconURL)
	return p, err
}

func nullableInt(n *int) any {
	if n == nil || *n == 0 {
		return nil
	}
	return *n
}
