package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

/*
 * The profile's kinds (profile-2-plan §4d, §4e, step 5).
 *
 * History is filtered by a group a person thinks in (Video, Music, Books,
 * Comics), not by the media kinds underneath, and both lists (each title once,
 * and every time) must mean the same thing by each group. So the mapping lives
 * here, once.
 *
 * Games are not a group. A retro game leaves no play record at all (§9c), only
 * its saves, so it has nothing in either history to filter; the Overview shows
 * its last save instead.
 */

// HistoryGroup is a filter on the profile's history.
type HistoryGroup string

const (
	GroupAll    HistoryGroup = ""
	GroupVideo  HistoryGroup = "video"
	GroupMusic  HistoryGroup = "music"
	GroupBooks  HistoryGroup = "books"
	GroupComics HistoryGroup = "comics"
)

// Valid reports whether the group is one the history knows.
func (g HistoryGroup) Valid() bool {
	return g == GroupAll || g.kinds() != nil
}

// kinds is the media kinds in a group; nil for All.
func (g HistoryGroup) kinds() []string {
	switch g {
	case GroupVideo:
		return []string{"movie", "episode", "video"}
	case GroupMusic:
		return []string{"track"}
	case GroupBooks:
		// A book is played on its own clock (ADR 0116); its parts carry rows
		// from before that, and both are the same listening.
		return []string{"book", "book_part"}
	case GroupComics:
		return []string{"comic"}
	}
	return nil
}

// kindClause is " AND col IN (?, …)" for a group, or nothing for All.
func (g HistoryGroup) kindClause(col string) (string, []any) {
	ks := g.kinds()
	if ks == nil {
		return "", nil
	}
	args := make([]any, len(ks))
	for i, k := range ks {
		args[i] = k
	}
	return " AND " + col + " IN (" + strings.TrimSuffix(strings.Repeat("?, ", len(ks)), ", ") + ")", args
}

// ProfileKinds is what the Overview shows of the kinds that arrived after it
// did: books, comics and games.
type ProfileKinds struct {
	Books  KindShelf   `json:"books"`
	Comics KindShelf   `json:"comics"`
	Games  []SavedGame `json:"games"`
}

// KindShelf is one kind's two short lists, newest first.
type KindShelf struct {
	InProgress []KindEntry `json:"in_progress"`
	Finished   []KindEntry `json:"finished"`
}

// KindEntry is a book or a comic, when it was last touched, and for a comic in
// progress the page it is on.
type KindEntry struct {
	Item      Item        `json:"item"`
	TouchedAt int64       `json:"touched_at"`
	Page      *ComicPlace `json:"page,omitempty"`
}

// SavedGame is a retro game and its newest save on the server. There is no
// play time to report (profile-2-plan §8), so this is the whole record.
type SavedGame struct {
	Item    Item  `json:"item"`
	SavedAt int64 `json:"saved_at"`
}

/*
 * ProfileKindsFor lists up to `per` of each.
 *
 * In progress is the resume console's own answer (continueOnTimed for books,
 * resumeComics, resumeGames) with no Continue window, so the profile and the
 * home page cannot disagree about what is unfinished; the profile simply does
 * not forget it after a month. Finished is the finished rows, newest first.
 *
 * Account limits (ADR 0084) are the caller's job, as for the console.
 */
func (s *Store) ProfileKindsFor(ctx context.Context, userID string, per int) (ProfileKinds, error) {
	if per <= 0 || per > 50 {
		per = 6
	}
	out := ProfileKinds{
		Books:  KindShelf{InProgress: []KindEntry{}, Finished: []KindEntry{}},
		Comics: KindShelf{InProgress: []KindEntry{}, Finished: []KindEntry{}},
		Games:  []SavedGame{},
	}

	books, err := s.continueOnTimed(ctx, userID, ShelfBooks, per, 0)
	if err != nil {
		return out, err
	}
	for _, t := range books {
		out.Books.InProgress = append(out.Books.InProgress, KindEntry{Item: t.Item, TouchedAt: t.Touched})
	}
	if out.Books.Finished, err = s.finishedOfKind(ctx, userID, "book", per); err != nil {
		return out, err
	}

	comics, err := s.resumeComics(ctx, userID, per, 0)
	if err != nil {
		return out, err
	}
	for _, c := range comics {
		out.Comics.InProgress = append(out.Comics.InProgress, KindEntry{Item: c.Item, TouchedAt: c.TouchedAt, Page: c.Page})
	}
	if out.Comics.Finished, err = s.finishedOfKind(ctx, userID, "comic", per); err != nil {
		return out, err
	}

	games, err := s.resumeGames(ctx, userID, per, 0)
	if err != nil {
		return out, err
	}
	for _, g := range games {
		out.Games = append(out.Games, SavedGame{Item: g.Item, SavedAt: g.TouchedAt})
	}
	return out, nil
}

// finishedOfKind is the titles of one kind this account has finished, newest
// first. Missing files are left out: this is a shelf to open things from, not
// the history.
func (s *Store) finishedOfKind(ctx context.Context, userID, kind string, limit int) ([]KindEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ps.item_id, ps.updated_at
		FROM playback_state ps
		JOIN media_item mi ON mi.id = ps.item_id AND mi.missing = 0 AND mi.kind = ?
		WHERE ps.user_id = ? AND ps.watched = 1
		ORDER BY ps.updated_at DESC, ps.item_id DESC
		LIMIT ?`, kind, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("finished %s: %w", kind, err)
	}
	type row struct{ id, at int64 }
	var found []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.at); err != nil {
			rows.Close()
			return nil, fmt.Errorf("finished %s: %w", kind, err)
		}
		found = append(found, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	out := []KindEntry{}
	for _, r := range found {
		it, err := s.GetItem(ctx, r.id, userID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("finished %s: %w", kind, err)
		}
		out = append(out, KindEntry{Item: *it, TouchedAt: r.at})
	}
	return out, nil
}
