package store

import (
	"context"
	"fmt"
	"time"
)

// MediaStream is one track of a probed file.
type MediaStream struct {
	Index   int    `json:"index"`
	Kind    string `json:"kind"`
	Codec   string `json:"codec"`
	Profile string `json:"profile,omitempty"`
	PixFmt  string `json:"pix_fmt,omitempty"`
	// Colour, for telling HDR from SDR (ADR 0033). Empty means not probed
	// since revision 19, which is not the same as SDR — IsHDR answers false
	// for both, so an unprobed library reads exactly as it did.
	ColorTransfer  string `json:"color_transfer,omitempty"`
	ColorPrimaries string `json:"color_primaries,omitempty"`
	ColorSpace     string `json:"color_space,omitempty"`
	Language       string `json:"language,omitempty"`
	Title          string `json:"title,omitempty"`
	Default        bool   `json:"default"`
	Forced         bool   `json:"forced"`

	Width    int   `json:"width,omitempty"`
	Height   int   `json:"height,omitempty"`
	Channels int   `json:"channels,omitempty"`
	BitRate  int64 `json:"bit_rate,omitempty"`
}

// ProbeResult is what SaveProbe persists.
type ProbeResult struct {
	DurationMS    int64
	Container     string
	VideoCodec    string
	VideoProfile  string
	Width         int
	Height        int
	VideoBitRate  int64
	FrameRate     float64
	AudioCodec    string
	AudioChannels int
	Streams       []MediaStream
}

// SaveProbe stores probe output and stamps the item as probed.
//
// The summary columns and the stream rows are written in one transaction: a
// half-applied probe would leave an item claiming a codec its stream list
// contradicts, which is worse than an unprobed item.
func (s *Store) SaveProbe(ctx context.Context, itemID int64, r ProbeResult) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save probe: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		UPDATE media_item SET
			probed_at = ?, duration_ms = ?, video_codec = ?, video_profile = ?,
			width = ?, height = ?, video_bitrate = ?, audio_codec = ?, audio_channels = ?,
			video_frame_rate = ?
		WHERE id = ?`,
		time.Now().Unix(), nullZero64(r.DurationMS), nullEmpty(r.VideoCodec), nullEmpty(r.VideoProfile),
		nullZero(r.Width), nullZero(r.Height), nullZero64(r.VideoBitRate),
		nullEmpty(r.AudioCodec), nullZero(r.AudioChannels), nullZeroF(r.FrameRate), itemID)
	if err != nil {
		return fmt.Errorf("save probe: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM media_stream WHERE item_id = ?`, itemID); err != nil {
		return fmt.Errorf("save probe: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO media_stream
			(item_id, idx, kind, codec, profile, pix_fmt, language, title, is_default, forced,
			 width, height, channels, bit_rate,
			 color_transfer, color_primaries, color_space)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("save probe: %w", err)
	}
	defer stmt.Close()

	for _, st := range r.Streams {
		if _, err := stmt.ExecContext(ctx, itemID, st.Index, st.Kind, st.Codec,
			nullEmpty(st.Profile), nullEmpty(st.PixFmt), nullEmpty(st.Language), nullEmpty(st.Title),
			boolInt(st.Default), boolInt(st.Forced),
			nullZero(st.Width), nullZero(st.Height), nullZero(st.Channels),
			nullZero64(st.BitRate),
			nullEmpty(st.ColorTransfer), nullEmpty(st.ColorPrimaries),
			nullEmpty(st.ColorSpace)); err != nil {
			return fmt.Errorf("save probe stream %d: %w", st.Index, err)
		}
	}
	return tx.Commit()
}

// PendingProbe returns items that have never been probed, or whose file
// changed since they were.
func (s *Store) PendingProbe(ctx context.Context, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM media_item
		WHERE probed_at IS NULL AND missing = 0 AND path IS NOT NULL AND container IS NOT NULL
		ORDER BY added_at LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("pending probe: %w", err)
	}
	defer rows.Close()

	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("pending probe: %w", err)
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// PendingProbeCount is how many items still need probing.
func (s *Store) PendingProbeCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM media_item
		WHERE probed_at IS NULL AND missing = 0 AND container IS NOT NULL`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("pending probe count: %w", err)
	}
	return n, nil
}

// ClearProbe queues probed items to be probed again by clearing probed_at, and
// reports how many were queued.
//
// The stored probe rows are left alone: SaveProbe replaces an item's streams
// wholesale, so deleting them here would only widen the window in which an
// item has no codec information at all and every playback decision for it
// falls back to direct play. Missing items are skipped — re-probing a file
// that is not on disk just marks it failed.
//
// libraryID of 0 means every library.
func (s *Store) ClearProbe(ctx context.Context, libraryID int64) (int64, error) {
	q := `UPDATE media_item SET probed_at = NULL
	      WHERE probed_at IS NOT NULL AND missing = 0 AND path IS NOT NULL`
	args := []any{}
	if libraryID != 0 {
		q += ` AND library_id = ?`
		args = append(args, libraryID)
	}

	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("clear probe: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("clear probe: %w", err)
	}
	return n, nil
}

// ClearIncompleteProbe queues only the items whose stored probe is missing
// information a current build needs, and reports how many were queued.
//
// Today that means a video stream with no pix_fmt: it was added in schema
// revision 12, and it is what decides whether a 10-bit H.264 file direct-plays
// into a black rectangle. Everything probed before that revision has it empty
// and silently falls back to matching profile names.
//
// This is the targeted alternative to ClearProbe. Re-probing a large library
// is hours of ffprobe, and most of those items would learn nothing.
//
// Some files genuinely have no pix_fmt to report, so they match this query on
// every call and are re-probed each time. That is a bounded, manually
// triggered cost rather than a loop — nothing calls this on a timer — and the
// alternative is a "probe attempted" flag that would have to be invalidated by
// the next added field anyway.
func (s *Store) ClearIncompleteProbe(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE media_item SET probed_at = NULL
		WHERE probed_at IS NOT NULL AND missing = 0 AND path IS NOT NULL
		  AND EXISTS (
		      SELECT 1 FROM media_stream
		      WHERE media_stream.item_id = media_item.id
		        AND media_stream.kind = 'video'
		        AND (media_stream.pix_fmt IS NULL OR media_stream.pix_fmt = '')
		  )`)
	if err != nil {
		return 0, fmt.Errorf("clear incomplete probe: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("clear incomplete probe: %w", err)
	}
	return n, nil
}

// MarkProbeFailed stamps an item so a file ffprobe cannot read is not retried
// forever. Without this a single corrupt file blocks the queue every pass.
func (s *Store) MarkProbeFailed(ctx context.Context, itemID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE media_item SET probed_at = ? WHERE id = ?`, time.Now().Unix(), itemID)
	return err
}

// Streams returns an item's tracks in file order.
func (s *Store) Streams(ctx context.Context, itemID int64) ([]MediaStream, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT idx, kind, codec, COALESCE(profile,''), COALESCE(pix_fmt,''),
		       COALESCE(language,''), COALESCE(title,''),
		       is_default, forced, COALESCE(width,0), COALESCE(height,0),
		       COALESCE(channels,0), COALESCE(bit_rate,0),
		       COALESCE(color_transfer,''), COALESCE(color_primaries,''),
		       COALESCE(color_space,'')
		FROM media_stream WHERE item_id = ? ORDER BY idx`, itemID)
	if err != nil {
		return nil, fmt.Errorf("streams: %w", err)
	}
	defer rows.Close()

	out := []MediaStream{}
	for rows.Next() {
		var st MediaStream
		var def, forced int
		if err := rows.Scan(&st.Index, &st.Kind, &st.Codec, &st.Profile, &st.PixFmt, &st.Language,
			&st.Title, &def, &forced, &st.Width, &st.Height, &st.Channels, &st.BitRate,
			&st.ColorTransfer, &st.ColorPrimaries, &st.ColorSpace); err != nil {
			return nil, fmt.Errorf("streams: %w", err)
		}
		st.Default, st.Forced = def != 0, forced != 0
		out = append(out, st)
	}
	return out, rows.Err()
}

// Storing zero and empty as NULL keeps "unknown" distinct from "genuinely
// zero" — a file really can have a zero bitrate field, and a UI showing "0
// kbps" for an unprobed file is worse than showing nothing.
func nullEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullZero(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullZero64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullZeroF(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
