package store

import (
	"context"
	"errors"
	"fmt"
)

/*
 * The resume console (ADR 0118): one list of everything this account is in the
 * middle of, across kinds, for the home page's Bridge.
 *
 * Each kind is asked for on its own and merged afterwards. One shared window is
 * the fault the Continue shelves already met twice — an evening of music filled
 * it and the films fell off (#1025) — so nothing here takes a share of a list
 * another kind can flood.
 *
 * This file only finds candidates. The caller orders them, applies the
 * account's limits and the per-kind cap, because those are rules about what
 * the console shows rather than about what is in progress.
 */

// Resume card kinds. A card is one thing you would resume, not one file.
const (
	ResumeVideo = "video" // a film, or a collection's next film
	ResumeShow  = "show"  // a series; the card names the next episode
	ResumeMusic = "music" // an album, resumed at a track
	ResumeBook  = "book"  // an audiobook
	ResumeComic = "comic" // a comic, at a page
	ResumeGame  = "game"  // a retro game with a save on the server
)

// ResumeCard is one entry on the console.
type ResumeCard struct {
	Kind      string `json:"kind"`
	TouchedAt int64  `json:"touched_at"`
	// Item is what the card pictures and opens: the film, the show (with
	// next_episode), the album, the book, the comic or the game.
	Item Item `json:"item"`
	// Track is set on a music card: the track to resume and where it sits.
	Track *ResumeTrack `json:"track,omitempty"`
	// Page is set on a comic card, with the same zero-based page
	// PUT /api/items/{id}/place takes.
	Page *ComicPlace `json:"page,omitempty"`
}

// ResumeTrack is the track a music card resumes.
type ResumeTrack struct {
	Item   Item `json:"item"`
	Number int  `json:"number,omitempty"`
	Of     int  `json:"of,omitempty"`
}

// ResumeCandidates lists up to perKind cards of each kind, newest first within
// a kind and in no particular order across kinds. sinceUnix is the Continue
// window (0 for none), the same rule every Continue shelf applies.
func (s *Store) ResumeCandidates(ctx context.Context, userID string, perKind int, sinceUnix int64) ([]ResumeCard, error) {
	if perKind <= 0 || perKind > 100 {
		perKind = 20
	}
	var out []ResumeCard

	// Films, shows and collections' next films: the Continue Watching query,
	// unchanged, so the console and a show's Continue button cannot disagree.
	// Shows and videos share this one query, so it is asked for enough of
	// both.
	watching, err := s.continueOnTimed(ctx, userID, ShelfWatching, perKind*2, sinceUnix)
	if err != nil {
		return nil, err
	}
	shows, videos := 0, 0
	for _, t := range watching {
		kind, n := ResumeVideo, &videos
		if t.Item.Kind == "show" {
			kind, n = ResumeShow, &shows
		}
		if *n < perKind {
			*n++
			out = append(out, ResumeCard{Kind: kind, TouchedAt: t.Touched, Item: t.Item})
		}
	}

	books, err := s.continueOnTimed(ctx, userID, ShelfBooks, perKind, sinceUnix)
	if err != nil {
		return nil, err
	}
	for _, t := range books {
		out = append(out, ResumeCard{Kind: ResumeBook, TouchedAt: t.Touched, Item: t.Item})
	}

	music, err := s.resumeAlbums(ctx, userID, perKind, sinceUnix)
	if err != nil {
		return nil, err
	}
	out = append(out, music...)

	comics, err := s.resumeComics(ctx, userID, perKind, sinceUnix)
	if err != nil {
		return nil, err
	}
	out = append(out, comics...)

	games, err := s.resumeGames(ctx, userID, perKind, sinceUnix)
	if err != nil {
		return nil, err
	}
	out = append(out, games...)
	return out, nil
}

/*
 * resumeAlbums is the music shelf collapsed to albums.
 *
 * Continue Listening lists tracks, and a console of tracks is the album you
 * are halfway through appearing as three cards. So a track becomes its album,
 * the newest track wins, and the card says which track and of how many. A
 * track with no album stands as itself.
 */
func (s *Store) resumeAlbums(ctx context.Context, userID string, perKind int, sinceUnix int64) ([]ResumeCard, error) {
	// Over-fetched, because several tracks collapse to one album.
	tracks, err := s.continueOnTimed(ctx, userID, ShelfMusic, perKind*5+10, sinceUnix)
	if err != nil {
		return nil, err
	}
	var out []ResumeCard
	seen := map[int64]bool{}
	for _, t := range tracks {
		if len(out) == perKind {
			break
		}
		tr := t.Item
		if tr.Kind != "track" || tr.ParentID == nil {
			if !seen[tr.ID] {
				seen[tr.ID] = true
				out = append(out, ResumeCard{Kind: ResumeMusic, TouchedAt: t.Touched, Item: tr})
			}
			continue
		}
		albumID := *tr.ParentID
		if seen[albumID] {
			continue
		}
		seen[albumID] = true
		album, err := s.GetItem(ctx, albumID, userID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("resume album: %w", err)
		}
		var of int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM media_item WHERE parent_id = ? AND kind = 'track' AND missing = 0`,
			albumID).Scan(&of); err != nil {
			return nil, fmt.Errorf("resume album tracks: %w", err)
		}
		rt := &ResumeTrack{Item: tr, Of: of}
		if tr.Episode != nil {
			rt.Number = *tr.Episode
		}
		out = append(out, ResumeCard{Kind: ResumeMusic, TouchedAt: t.Touched, Item: *album, Track: rt})
	}
	return out, nil
}

// resumeComics is every comic opened and not finished. Finishing one marks it
// watched (putComicPlace), which is what takes it off.
func (s *Store) resumeComics(ctx context.Context, userID string, perKind int, sinceUnix int64) ([]ResumeCard, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT cp.item_id, cp.page, cp.pages, cp.updated_at
		FROM comic_place cp
		JOIN media_item mi ON mi.id = cp.item_id AND mi.missing = 0 AND mi.kind = 'comic'
		LEFT JOIN playback_state ps ON ps.item_id = cp.item_id AND ps.user_id = cp.user_id
		WHERE cp.user_id = ? AND COALESCE(ps.watched, 0) = 0
		  AND (? = 0 OR cp.updated_at >= ?)
		ORDER BY cp.updated_at DESC
		LIMIT ?`, userID, sinceUnix, sinceUnix, perKind)
	if err != nil {
		return nil, fmt.Errorf("resume comics: %w", err)
	}
	type place struct {
		id int64
		p  ComicPlace
	}
	var found []place
	for rows.Next() {
		var pl place
		if err := rows.Scan(&pl.id, &pl.p.Page, &pl.p.Pages, &pl.p.UpdatedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("resume comics: %w", err)
		}
		found = append(found, pl)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	var out []ResumeCard
	for _, pl := range found {
		it, err := s.GetItem(ctx, pl.id, userID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("resume comic: %w", err)
		}
		p := pl.p
		out = append(out, ResumeCard{Kind: ResumeComic, TouchedAt: p.UpdatedAt, Item: *it, Page: &p})
	}
	return out, nil
}

// resumeGames is every retro game this account has a save of on the server,
// by its newest save. A game has no "finished", so a save is the whole test.
func (s *Store) resumeGames(ctx context.Context, userID string, perKind int, sinceUnix int64) ([]ResumeCard, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT rs.item_id, MAX(rs.updated_at) AS touched
		FROM rom_save rs
		JOIN media_item mi ON mi.id = rs.item_id AND mi.missing = 0
		WHERE rs.user_id = ?
		GROUP BY rs.item_id
		HAVING ? = 0 OR touched >= ?
		ORDER BY touched DESC
		LIMIT ?`, userID, sinceUnix, sinceUnix, perKind)
	if err != nil {
		return nil, fmt.Errorf("resume games: %w", err)
	}
	type save struct{ id, at int64 }
	var found []save
	for rows.Next() {
		var sv save
		if err := rows.Scan(&sv.id, &sv.at); err != nil {
			rows.Close()
			return nil, fmt.Errorf("resume games: %w", err)
		}
		found = append(found, sv)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	var out []ResumeCard
	for _, sv := range found {
		it, err := s.GetItem(ctx, sv.id, userID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("resume game: %w", err)
		}
		out = append(out, ResumeCard{Kind: ResumeGame, TouchedAt: sv.at, Item: *it})
	}
	return out, nil
}
