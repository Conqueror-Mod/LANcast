package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

/*
 * Writing a MusicBrainz match onto an album or an artist (ADR 0115).
 *
 * Not UpdateItemMetadata, and that is the point. The shared path merges a
 * provider over what an item holds, which is right for a film named after its
 * file and wrong here: an album is named by its tags, and the tags win (ADR
 * 0114, 0115). So these methods add an identity beside the tags and fill only
 * what is empty, in the SQL itself, where no caller can forget the rule.
 */

// AlbumMatch is what a match adds to an album.
type AlbumMatch struct {
	Provider   string
	ExternalID string
	State      string // matched | review
	Score      float64
	// ReleaseType is Album, EP, Single, Compilation, Live… (meta.MusicInfo).
	ReleaseType string
	// ArtistID is the first artist the credit names: what the album's artist
	// is identified by (ArtistCreditVotes).
	ArtistID string
	// Year and Genres fill only an album that has none.
	Year   int
	Genres []string
	// Confirmed is a person's choice in Fix match: it is applied even over a
	// locked match, and locks the identity (State is ignored). Fields stay
	// gap-only, as for every match: the tags still win.
	Confirmed bool
}

// ApplyAlbumMatch writes a match onto an album. A locked album is left as it
// is unless the match is Confirmed; a locked year or genres field likewise.
func (s *Store) ApplyAlbumMatch(ctx context.Context, itemID int64, m AlbumMatch) error {
	locked, err := s.LockedFields(ctx, itemID)
	if err != nil {
		return fmt.Errorf("apply album match: %w", err)
	}
	isLocked := map[string]bool{}
	for _, f := range locked {
		isLocked[f] = true
	}
	year := 0
	if !isLocked["year"] {
		year = m.Year
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE media_item SET
			provider = ?, external_id = ?, match_state = ?, match_score = ?,
			release_type = NULLIF(?, ''), artist_external_id = NULLIF(?, ''),
			year = CASE WHEN (year IS NULL OR year = 0) AND ? > 0 THEN ? ELSE year END,
			metadata_updated_at = ?
		WHERE id = ? AND kind = 'album' AND (match_state != 'locked' OR ?)`,
		m.Provider, m.ExternalID, confirmedState(m.State, m.Confirmed), m.Score,
		m.ReleaseType, m.ArtistID, year, year,
		time.Now().Unix(), itemID, m.Confirmed)
	if err != nil {
		return fmt.Errorf("apply album match: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	return s.fillGenres(ctx, itemID, m.Genres, isLocked["genres"])
}

func confirmedState(state string, confirmed bool) string {
	if confirmed {
		return "locked"
	}
	return state
}

/*
 * RequeueArtistOf puts an album's artist back in the queue, after a person
 * changed what the album is. The artist is voted from its albums' credits, so
 * a corrected album may change the count. A locked artist is left alone.
 */
func (s *Store) RequeueArtistOf(ctx context.Context, albumID int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE media_item SET metadata_updated_at = NULL
		WHERE kind = 'artist' AND match_state != 'locked'
		  AND id = (SELECT parent_id FROM media_item WHERE id = ? AND kind = 'album')`,
		albumID)
	if err != nil {
		return fmt.Errorf("requeue artist: %w", err)
	}
	return nil
}

// fillGenres gives an item genres only when it has none and they are not
// locked. Tag genres are the owner's statement and are never replaced.
func (s *Store) fillGenres(ctx context.Context, itemID int64, genres []string, locked bool) error {
	if len(genres) == 0 || locked {
		return nil
	}
	var have int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM item_genre WHERE item_id = ?`, itemID).Scan(&have); err != nil {
		return fmt.Errorf("fill genres: %w", err)
	}
	if have > 0 {
		return nil
	}
	return s.replaceTagGenres(ctx, itemID, genres)
}

/*
 * MarkMusicUnmatched records that an album or artist was looked for and not
 * found, so it leaves the queue. Only for a real answer: a provider that was
 * busy or unreachable gave none, and the item must stay pending (ADR 0115).
 */
func (s *Store) MarkMusicUnmatched(ctx context.Context, itemID int64, score float64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE media_item SET match_state = 'unmatched', match_score = ?,
			metadata_updated_at = ?
		WHERE id = ? AND kind IN ('album', 'artist') AND match_state != 'locked'`,
		score, time.Now().Unix(), itemID)
	if err != nil {
		return fmt.Errorf("mark music unmatched: %w", err)
	}
	return nil
}

/*
 * ArtistCreditVotes is how an artist's albums identify it (ADR 0115).
 *
 * An artist is never searched by name: "Nirvana" is several artists. Its
 * albums are matched instead, each naming an artist in its credit, and the
 * artist is the one most of them name. Votes come from albums matched with
 * confidence, or fixed by a person (locked with an id); a review-band album
 * is a guess and does not vote. Pending is the albums not yet looked at, which
 * is what tells "no answer yet" from "no answer".
 */
func (s *Store) ArtistCreditVotes(ctx context.Context, artistID int64) (votes map[string]int, pending int, err error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT artist_external_id, match_state, metadata_updated_at
		FROM media_item
		WHERE parent_id = ? AND kind = 'album' AND missing = 0`, artistID)
	if err != nil {
		return nil, 0, fmt.Errorf("artist credit votes: %w", err)
	}
	defer rows.Close()
	votes = map[string]int{}
	for rows.Next() {
		var id sql.NullString
		var state string
		var stamped sql.NullInt64
		if err := rows.Scan(&id, &state, &stamped); err != nil {
			return nil, 0, fmt.Errorf("artist credit votes: %w", err)
		}
		switch {
		case (state == "matched" || state == "locked") && id.Valid && id.String != "":
			votes[id.String]++
		case !stamped.Valid && state != "locked":
			pending++
		}
	}
	return votes, pending, rows.Err()
}

// ArtistMatch is what a match adds to an artist.
type ArtistMatch struct {
	Provider   string
	ExternalID string
	State      string
	Score      float64
	Genres     []string
	// Confirmed is a person's choice, as for AlbumMatch.
	Confirmed bool
}

// ApplyArtistMatch writes an identity onto an artist, and genres only if it
// has none. Its name stays the one its tags give it.
func (s *Store) ApplyArtistMatch(ctx context.Context, itemID int64, m ArtistMatch) error {
	locked, err := s.LockedFields(ctx, itemID)
	if err != nil {
		return fmt.Errorf("apply artist match: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE media_item SET provider = ?, external_id = ?, match_state = ?,
			match_score = ?, metadata_updated_at = ?
		WHERE id = ? AND kind = 'artist' AND (match_state != 'locked' OR ?)`,
		m.Provider, m.ExternalID, confirmedState(m.State, m.Confirmed), m.Score,
		time.Now().Unix(), itemID, m.Confirmed)
	if err != nil {
		return fmt.Errorf("apply artist match: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	genresLocked := false
	for _, f := range locked {
		if f == "genres" {
			genresLocked = true
		}
	}
	return s.fillGenres(ctx, itemID, m.Genres, genresLocked)
}
