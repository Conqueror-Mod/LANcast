package store

import (
	"context"
	"database/sql"
	"fmt"
)

/*
 * A track's loudness, as its tagger measured it (ADR 0108).
 *
 * The server stores and reports ReplayGain; it does not apply it. Playing a
 * track quieter is the player's business, per device, and a file served with
 * its own bytes is the only kind the music deck plays gaplessly.
 */
type Loudness struct {
	// TrackGain and AlbumGain are in dB, relative to ReplayGain 2's reference
	// (-18 LUFS); usually negative for anything mastered since the 1990s.
	TrackGain *float64 `json:"track_gain_db,omitempty"`
	AlbumGain *float64 `json:"album_gain_db,omitempty"`
	// TrackPeak and AlbumPeak are the loudest sample, 1 at full scale.
	TrackPeak *float64 `json:"track_peak,omitempty"`
	AlbumPeak *float64 `json:"album_peak,omitempty"`
}

func (l Loudness) empty() bool {
	return l.TrackGain == nil && l.AlbumGain == nil && l.TrackPeak == nil && l.AlbumPeak == nil
}

// ItemLoudness is a track's stored ReplayGain, or nil when it has none.
func (s *Store) ItemLoudness(ctx context.Context, itemID int64) (*Loudness, error) {
	var tg, tp, ag, ap sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `SELECT rg_track_gain, rg_track_peak, rg_album_gain, rg_album_peak
		FROM media_item WHERE id = ?`, itemID).Scan(&tg, &tp, &ag, &ap)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("item loudness: %w", err)
	}
	f := func(v sql.NullFloat64) *float64 {
		if !v.Valid {
			return nil
		}
		x := v.Float64
		return &x
	}
	l := Loudness{TrackGain: f(tg), TrackPeak: f(tp), AlbumGain: f(ag), AlbumPeak: f(ap)}
	if l.empty() {
		return nil, nil
	}
	return &l, nil
}

// UnkeyedTracks counts a library's present tracks with no stored grouping
// key: tracks the tag pass has to read even when no file changed, such as
// every track after a migration drops the keys.
func (s *Store) UnkeyedTracks(ctx context.Context, libraryID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_item
		WHERE library_id = ? AND kind = 'track' AND missing = 0 AND group_dir IS NULL`,
		libraryID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("unkeyed tracks: %w", err)
	}
	return n, nil
}
