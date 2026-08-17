package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ------------------------------------------------------------------- locks

// LockedFields returns the field names locked on an item.
func (s *Store) LockedFields(ctx context.Context, itemID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT field FROM item_lock WHERE item_id = ? ORDER BY field`, itemID)
	if err != nil {
		return nil, fmt.Errorf("locked fields: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, fmt.Errorf("locked fields: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// LockField marks a field as user-owned. Idempotent.
func (s *Store) LockField(ctx context.Context, itemID int64, field string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO item_lock (item_id, field) VALUES (?, ?)`, itemID, field)
	if err != nil {
		return fmt.Errorf("lock field %q: %w", field, err)
	}
	return nil
}

// UnlockField releases a lock so the field resumes updating.
func (s *Store) UnlockField(ctx context.Context, itemID int64, field string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM item_lock WHERE item_id = ? AND field = ?`, itemID, field)
	if err != nil {
		return fmt.Errorf("unlock field %q: %w", field, err)
	}
	return nil
}

// ------------------------------------------------------------------ metadata

// ItemMetadata is the writable metadata for one item. Nil fields are left
// unchanged, so callers pass only what the merge engine resolved.
type ItemMetadata struct {
	Title         *string
	SortTitle     *string
	Year          *int
	Overview      *string
	Rating        *float64
	ContentRating *string
	ReleasedAt    *int64
	DurationMS    *int64
	Series        *string
	Season        *int
	Episode       *int

	Provider   *string
	ExternalID *string
	MatchState *string
	MatchScore *float64
	IMDbID     *string
}

// UpdateItemMetadata writes resolved metadata and stamps metadata_updated_at,
// which is what removes the item from the enrichment queue.
func (s *Store) UpdateItemMetadata(ctx context.Context, itemID int64, m ItemMetadata) error {
	set := []string{"metadata_updated_at = ?"}
	args := []any{time.Now().Unix()}

	add := func(col string, v any, isNil bool) {
		if isNil {
			return
		}
		set = append(set, col+" = ?")
		args = append(args, v)
	}
	add("title", derefStr(m.Title), m.Title == nil)
	add("sort_title", derefStr(m.SortTitle), m.SortTitle == nil)
	add("year", derefInt(m.Year), m.Year == nil)
	add("overview", derefStr(m.Overview), m.Overview == nil)
	add("rating", derefFloat(m.Rating), m.Rating == nil)
	add("content_rating", derefStr(m.ContentRating), m.ContentRating == nil)
	add("released_at", derefInt64(m.ReleasedAt), m.ReleasedAt == nil)
	add("duration_ms", derefInt64(m.DurationMS), m.DurationMS == nil)
	add("series", derefStr(m.Series), m.Series == nil)
	add("season", derefInt(m.Season), m.Season == nil)
	add("episode", derefInt(m.Episode), m.Episode == nil)
	add("provider", derefStr(m.Provider), m.Provider == nil)
	add("external_id", derefStr(m.ExternalID), m.ExternalID == nil)
	add("match_state", derefStr(m.MatchState), m.MatchState == nil)
	add("match_score", derefFloat(m.MatchScore), m.MatchScore == nil)
	add("imdb_id", derefStr(m.IMDbID), m.IMDbID == nil)

	q := `UPDATE media_item SET ` + join(set, ", ") + `, updated_at = ? WHERE id = ?`
	args = append(args, time.Now().Unix(), itemID)

	if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("update item metadata: %w", err)
	}
	return nil
}

// SetMatch records a confirmed identity. A locked match is never re-scored or
// re-searched: rescans reconcile files, they do not re-litigate identity.
func (s *Store) SetMatch(ctx context.Context, itemID int64, provider, externalID, state string, score float64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE media_item
		SET provider = ?, external_id = ?, match_state = ?, match_score = ?, updated_at = ?
		WHERE id = ?`,
		provider, externalID, state, score, time.Now().Unix(), itemID)
	if err != nil {
		return fmt.Errorf("set match: %w", err)
	}
	return nil
}

// -------------------------------------------------------------- external ratings

// ItemRating is one third-party score for an item (ADR 0019). Score is
// normalized to 0–10 so it sorts and compares uniformly; Display keeps the
// source-native form ("92%", "74") so the UI renders each in its own scale.
type ItemRating struct {
	Source    string  `json:"source"`
	Score     float64 `json:"score"`
	Display   string  `json:"display"`
	Votes     int     `json:"votes,omitempty"`
	UpdatedAt int64   `json:"-"`
}

// SaveRatings upserts an item's scores, one row per source. A source already
// present is replaced (a refresh re-fetches it); sources not in the set are left
// untouched, so two rating sources writing independently do not clobber each
// other.
func (s *Store) SaveRatings(ctx context.Context, itemID int64, ratings []ItemRating) error {
	if len(ratings) == 0 {
		return nil
	}
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save ratings: %w", err)
	}
	defer tx.Rollback()
	for _, r := range ratings {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO item_rating (item_id, source, score, display, votes, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(item_id, source) DO UPDATE SET
				score = excluded.score, display = excluded.display,
				votes = excluded.votes, updated_at = excluded.updated_at`,
			itemID, r.Source, r.Score, r.Display, r.Votes, now); err != nil {
			return fmt.Errorf("save ratings: %w", err)
		}
	}
	return tx.Commit()
}

// ItemRatings returns an item's third-party scores, highest normalized score
// first so the UI leads with the strongest.
func (s *Store) ItemRatings(ctx context.Context, itemID int64) ([]ItemRating, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT source, score, display, votes, updated_at
		FROM item_rating WHERE item_id = ?
		ORDER BY score DESC, source`, itemID)
	if err != nil {
		return nil, fmt.Errorf("item ratings: %w", err)
	}
	defer rows.Close()
	out := []ItemRating{}
	for rows.Next() {
		var r ItemRating
		if err := rows.Scan(&r.Source, &r.Score, &r.Display, &r.Votes, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("item ratings: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// enrichableKinds excludes the rows no provider can ever match.
//
// Music is built from the file's own tags and LANcast ships no music provider
// (ADR 0024): a track, album or artist will never be identified by TMDB, and
// asking is not merely wasted work — it is *blocking* work. The queue is
// ordered oldest-first, so a music library puts thousands of permanently
// unmatchable rows at its head. Measured on the real library: 1,592 tracks, 394
// albums and 206 artists ahead of everything else.
//
// A filter rather than a stamp on the rows themselves. Marking music as
// "enrichment has run" would be a lie about what happened, and the day a music
// provider exists this one line changes and those rows become eligible with no
// migration to undo the lie.
//
// This is also what makes the remaining count mean something. With music in the
// queue it read 2,198 forever — a number that never falls, indistinguishable
// from a stuck backlog, for work that was never going to happen.
//
// Pictures are the same case and were missed when this list was written. A
// photo is its filename verbatim (ADR 0028) and `meta.Caps.Supports` answers
// false for both photo and gallery, so no provider will ever match one. On the
// real library that left **4,238 photos** permanently pending, which is why the
// activity readout said "of 5,492" no matter which library was being scanned:
// the total was dominated by rows that could never move, and adding a film to
// it changed the figure by less than a tenth of a percent.
//
// The lesson the music entry already paid for, repeated: the test is not "did
// we forget a kind" but "can a provider ever answer for this kind".
const enrichableKinds = `kind NOT IN ('track', 'album', 'artist', 'photo', 'gallery')`

// PendingEnrichment returns items awaiting metadata. The queue is a query
// rather than a table, which makes it restart-safe by construction.
func (s *Store) PendingEnrichment(ctx context.Context, limit int) ([]Item, error) {
	return s.PendingEnrichmentFrom(ctx, limit, 0)
}

// PendingEnrichmentFrom is PendingEnrichment with a starting offset, so a caller
// can look past a run of items it could not stamp.
//
// The queue is a query, not a cursor: rows that get enriched leave it, and rows
// nothing can enrich stay at the front forever. Without an offset, a worker that
// stops at the first unproductive batch never sees anything behind that batch —
// which is how a music backlog no provider handles stranded every film added
// after it.
func (s *Store) PendingEnrichmentFrom(ctx context.Context, limit, offset int) ([]Item, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM media_item
		WHERE metadata_updated_at IS NULL AND missing = 0 AND match_state != 'locked'
		  AND `+enrichableKinds+`
		ORDER BY added_at LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("pending enrichment: %w", err)
	}
	defer rows.Close()

	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("pending enrichment: %w", err)
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// PendingCount is how many items still await enrichment.
//
// The worker's batch length is not this number — reporting the batch size as
// "remaining" tells the user 50 when there are 3000, and keeps saying 25 after
// the work is finished.
func (s *Store) PendingCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM media_item
		WHERE metadata_updated_at IS NULL AND missing = 0 AND match_state != 'locked'
		  AND `+enrichableKinds).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("pending count: %w", err)
	}
	return n, nil
}

// ClearMetadataStamp requeues items for enrichment. Locked fields still survive
// the refresh — this only schedules the work.
func (s *Store) ClearMetadataStamp(ctx context.Context, libraryID int64, itemID int64) error {
	var err error
	switch {
	case itemID != 0:
		_, err = s.db.ExecContext(ctx,
			`UPDATE media_item SET metadata_updated_at = NULL WHERE id = ?`, itemID)
	default:
		_, err = s.db.ExecContext(ctx,
			`UPDATE media_item SET metadata_updated_at = NULL WHERE library_id = ?`, libraryID)
	}
	if err != nil {
		return fmt.Errorf("clear metadata stamp: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------- re-parsing

// ReparseTarget is one row a re-parse may rewrite, carrying everything the
// filename heuristics need: the file, the root it was found under, and the
// owning library's kind.
type ReparseTarget struct {
	ItemID  int64
	Path    string
	Root    string
	LibKind string
}

// Guess is what re-running the filename heuristics produced for a row. It is
// deliberately only the *identity* fields — a guess has no opinion about
// overview, rating or artwork, and must not be able to clear them.
type Guess struct {
	Title     string
	SortTitle string
	Year      int
	Series    string
	Season    int
	Episode   int
}

/*
 * ReparseTargets lists the items a re-parse is allowed to touch.
 *
 * Only 'review' and 'unmatched' rows qualify, and that restriction is the
 * safety of the whole operation rather than a performance shortcut. A
 * 'matched' row's title came from a provider, which is better evidence than
 * any filename; rewriting it with a guess would trade a thousand correct
 * titles for a chance at a hundred uncertain ones.
 *
 * 'locked' and 'local' fall outside the same clause, for the reasons they
 * always do: a locked identity is never re-litigated, and a local one is what
 * the user already said this is (CLAUDE.md, ADR 0008).
 */
func (s *Store) ReparseTargets(ctx context.Context, libraryID int64, force bool) ([]ReparseTarget, error) {
	args := []any{}
	where := ` WHERE i.missing = 0 AND i.match_state IN ('review','unmatched')`

	// A row that has already been re-parsed is not offered again, and this is
	// what makes re-running the action free.
	//
	// Without it a re-parse cannot tell a row it has never seen from one it
	// re-parsed a minute ago, because enrichment writes the provider's answer
	// back over the guess for anything that stays uncertain — so the stored
	// title disagrees with the filename either way. Every run then rewrote the
	// same rows and asked the provider the same question again. On a real
	// library that was 32 rows flipping back and forth on every press.
	//
	// force is the escape hatch for the case the stamp cannot see: the parser
	// itself improving, where rows re-parsed under the old heuristics deserve
	// another pass.
	if !force {
		where += ` AND i.reparsed_at IS NULL`
	}
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
		return nil, fmt.Errorf("reparse targets: %w", err)
	}
	defer rows.Close()

	out := []ReparseTarget{}
	for rows.Next() {
		var t ReparseTarget
		if err := rows.Scan(&t.ItemID, &t.Path, &t.Root, &t.LibKind); err != nil {
			return nil, fmt.Errorf("reparse targets: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

/*
 * ApplyGuess writes re-parsed identity fields over a row and requeues it for
 * enrichment, returning whether anything actually changed.
 *
 * Locked fields are skipped individually, not as a group: an item whose title
 * a person corrected still has its year re-parsed, because the lock says "I
 * own the title", not "stop looking at this file".
 *
 * A row that already agrees with its filename is left alone entirely — no
 * write, no requeue. Re-parsing is meant to be safe to run twice, and a
 * version that requeued the whole library every time would turn a repair into
 * a provider-quota event.
 */
func (s *Store) ApplyGuess(ctx context.Context, itemID int64, g Guess) (bool, error) {
	locked, err := s.LockedFields(ctx, itemID)
	if err != nil {
		return false, err
	}
	isLocked := func(f string) bool {
		for _, l := range locked {
			if l == f {
				return true
			}
		}
		return false
	}

	var cur Guess
	var year, season, episode sql.NullInt64
	var series sql.NullString
	err = s.db.QueryRowContext(ctx,
		`SELECT title, year, series, season, episode FROM media_item WHERE id = ?`, itemID).
		Scan(&cur.Title, &year, &series, &season, &episode)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("apply guess: %w", err)
	}
	cur.Year, cur.Series = int(year.Int64), series.String
	cur.Season, cur.Episode = int(season.Int64), int(episode.Int64)

	set := []string{}
	args := []any{}
	add := func(col string, val any) {
		set = append(set, col+" = ?")
		args = append(args, val)
	}

	// An empty guess is not an answer. The parser returning nothing for a field
	// means it could not tell, which is never a reason to erase what is there.
	if g.Title != "" && g.Title != cur.Title && !isLocked("title") {
		add("title", g.Title)
		add("sort_title", g.SortTitle)
	}
	if g.Year != 0 && g.Year != cur.Year && !isLocked("year") {
		add("year", g.Year)
	}
	if g.Series != "" && g.Series != cur.Series && !isLocked("series") {
		add("series", g.Series)
	}
	if g.Season != 0 && g.Season != cur.Season && !isLocked("season") {
		add("season", g.Season)
	}
	if g.Episode != 0 && g.Episode != cur.Episode && !isLocked("episode") {
		add("episode", g.Episode)
	}
	now := time.Now().Unix()

	// Stamped whether or not anything changed. A row the parser already agrees
	// with has been re-parsed just as truly as one that moved, and leaving it
	// unstamped would offer it again on every run for ever.
	changed := len(set) > 0
	if changed {
		// Clearing the metadata stamp is what puts the row back in the
		// enrichment queue, so the corrected guess is actually searched against
		// a provider. Without it this writes a better question and never asks
		// it.
		set = append(set, "metadata_updated_at = NULL")
	}
	set = append(set, "reparsed_at = ?", "updated_at = ?")
	args = append(args, now, now, itemID)

	if _, err := s.db.ExecContext(ctx,
		`UPDATE media_item SET `+strings.Join(set, ", ")+` WHERE id = ?`, args...); err != nil {
		return false, fmt.Errorf("apply guess: %w", err)
	}
	return changed, nil
}

/*
 * notReviewable excludes seasons from the review queue.
 *
 * A season has no identity of its own. Its name is "Season 1" — a position
 * within a show, not the name of a work — so searching a provider for it can
 * only ever fail, and it fails at 0% on every season in the library. A real TV
 * library reported 55 of them, each offering a Fix button leading to a search
 * that cannot succeed and a human decision that does not exist.
 *
 * `meta.Caps.Supports` routes KindSeason to the show providers, which is why
 * these reach a provider at all rather than being skipped as unsupported. That
 * routing is right for *fetching* a known season, and wrong for searching one
 * by name — but the queue is where the cost lands on a person, so this is where
 * it is stopped.
 *
 * Shows are deliberately still listed. A show's title is a real title, a wrong
 * match on one is worth correcting, and a person can do it — which is the test
 * for belonging here.
 */
const notReviewable = `kind != 'season'`

// ReviewQueue returns items whose identity is uncertain. Applying a
// low-confidence match is a good default, but it is recorded as uncertain
// rather than presented as fact.
//
// Only items that have actually been through enrichment qualify.
// match_state defaults to 'unmatched', so without the metadata_updated_at
// check every freshly scanned item would appear here — reporting "no match
// found" for titles nothing has looked at yet. That is the same
// not-attempted versus no-answer distinction the enrichment worker makes.
func (s *Store) ReviewQueue(ctx context.Context, libraryID int64, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 100
	}
	args := []any{}
	where := ` WHERE match_state IN ('review','unmatched') AND missing = 0
		AND metadata_updated_at IS NOT NULL AND ` + notReviewable
	if libraryID != 0 {
		where += ` AND library_id = ?`
		args = append(args, libraryID)
	}
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM media_item`+where+` ORDER BY match_score DESC, sort_title LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("review queue: %w", err)
	}
	defer rows.Close()

	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("review queue: %w", err)
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// -------------------------------------------------------------------- genres

// ReplaceGenres sets an item's genres, creating genre rows as needed.
func (s *Store) ReplaceGenres(ctx context.Context, itemID int64, names []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("replace genres: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM item_genre WHERE item_id = ?`, itemID); err != nil {
		return fmt.Errorf("replace genres: %w", err)
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO genre (name) VALUES (?)`, name); err != nil {
			return fmt.Errorf("replace genres: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO item_genre (item_id, genre_id)
			VALUES (?, (SELECT id FROM genre WHERE name = ?))`, itemID, name); err != nil {
			return fmt.Errorf("replace genres: %w", err)
		}
	}
	return tx.Commit()
}

// Genres returns an item's genre names.
func (s *Store) Genres(ctx context.Context, itemID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.name FROM genre g
		JOIN item_genre ig ON ig.genre_id = g.id
		WHERE ig.item_id = ? ORDER BY g.name`, itemID)
	if err != nil {
		return nil, fmt.Errorf("genres: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("genres: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------------- credits

// ReplaceCredits sets an item's cast and crew.
func (s *Store) ReplaceCredits(ctx context.Context, itemID int64, provider string, credits []Credit) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("replace credits: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM credit WHERE item_id = ?`, itemID); err != nil {
		return fmt.Errorf("replace credits: %w", err)
	}
	for i, c := range credits {
		if c.Name == "" {
			continue
		}
		// People are keyed by name when the provider gives no id; good enough
		// until a provider that supplies stable person ids is added.
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO person (provider, external_id, name) VALUES (?, ?, ?)`,
			provider, c.Name, c.Name); err != nil {
			return fmt.Errorf("replace credits: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO credit (item_id, person_id, role, character, ord)
			VALUES (?, (SELECT id FROM person WHERE provider = ? AND external_id = ?), ?, ?, ?)`,
			itemID, provider, c.Name, c.Role, c.Character, i); err != nil {
			return fmt.Errorf("replace credits: %w", err)
		}
	}
	return tx.Commit()
}

// Credits returns an item's cast and crew in billing order.
func (s *Store) Credits(ctx context.Context, itemID int64) ([]Credit, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.name, c.role, COALESCE(c.character, ''), c.ord
		FROM credit c JOIN person p ON p.id = c.person_id
		WHERE c.item_id = ? ORDER BY c.ord`, itemID)
	if err != nil {
		return nil, fmt.Errorf("credits: %w", err)
	}
	defer rows.Close()

	out := []Credit{}
	for rows.Next() {
		var c Credit
		if err := rows.Scan(&c.Name, &c.Role, &c.Character, &c.Order); err != nil {
			return nil, fmt.Errorf("credits: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------------- artwork

// PutArtwork records a stored image and links it to an item, returning the
// artwork row id. Content-addressed, so re-storing the same bytes is a no-op.
func (s *Store) PutArtwork(ctx context.Context, itemID int64, hash, kind, sourceURL string, w, h int, size int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("put artwork: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO artwork (hash, kind, source_url, width, height, bytes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(hash) DO NOTHING`,
		hash, kind, sourceURL, w, h, size, time.Now().Unix()); err != nil {
		return fmt.Errorf("put artwork: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO item_artwork (item_id, artwork_id, kind, selected)
		VALUES (?, (SELECT id FROM artwork WHERE hash = ?), ?, 1)`,
		itemID, hash, kind); err != nil {
		return fmt.Errorf("put artwork: %w", err)
	}
	return tx.Commit()
}

// ItemArtwork returns the selected image hashes for an item.
func (s *Store) ItemArtwork(ctx context.Context, itemID int64) (*Artwork, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ia.kind, a.hash FROM item_artwork ia
		JOIN artwork a ON a.id = ia.artwork_id
		WHERE ia.item_id = ? AND ia.selected = 1`, itemID)
	if err != nil {
		return nil, fmt.Errorf("item artwork: %w", err)
	}
	defer rows.Close()

	var art Artwork
	var any bool
	for rows.Next() {
		var kind, hash string
		if err := rows.Scan(&kind, &hash); err != nil {
			return nil, fmt.Errorf("item artwork: %w", err)
		}
		any = true
		switch kind {
		case "poster":
			art.Poster = hash
		case "fanart":
			art.Fanart = hash
		case "thumb":
			art.Thumb = hash
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !any {
		return nil, nil
	}
	return &art, nil
}

// AttachArtwork fills in Artwork for a page of items in one query.
//
// The grid renders from the list endpoint, so without this every poster is
// downloaded, stored, and then never shown. Doing it per item would be one
// query per tile, which is exactly the N+1 the batch shape exists to avoid.
func (s *Store) AttachArtwork(ctx context.Context, items []Item) error {
	if len(items) == 0 {
		return nil
	}

	placeholders := make([]byte, 0, len(items)*2)
	args := make([]any, 0, len(items))
	for i := range items {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, items[i].ID)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT ia.item_id, ia.kind, a.hash
		FROM item_artwork ia
		JOIN artwork a ON a.id = ia.artwork_id
		WHERE ia.selected = 1 AND ia.item_id IN (`+string(placeholders)+`)`, args...)
	if err != nil {
		return fmt.Errorf("attach artwork: %w", err)
	}
	defer rows.Close()

	byID := map[int64]*Artwork{}
	for rows.Next() {
		var id int64
		var kind, hash string
		if err := rows.Scan(&id, &kind, &hash); err != nil {
			return fmt.Errorf("attach artwork: %w", err)
		}
		art, ok := byID[id]
		if !ok {
			art = &Artwork{}
			byID[id] = art
		}
		switch kind {
		case "poster":
			art.Poster = hash
		case "fanart":
			art.Fanart = hash
		case "thumb":
			art.Thumb = hash
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range items {
		if art, ok := byID[items[i].ID]; ok {
			items[i].Artwork = art
		}
	}

	// Containers that own no image borrow one from a child. Done here rather
	// than by the caller so every grid gets it — a tile with a poster in one
	// list and none in another is worse than consistent blankness.
	if err := s.inheritArtistPosters(ctx, items); err != nil {
		return err
	}
	return s.inheritGalleryPosters(ctx, items)
}

// ArtworkExists reports whether a hash is already stored, so a download can be
// skipped.
func (s *Store) ArtworkExists(ctx context.Context, hash string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artwork WHERE hash = ?`, hash).Scan(&n)
	return n > 0, err
}

// ------------------------------------------------------------ provider cache

// CachedResponse returns a cached provider payload newer than maxAge.
func (s *Store) CachedResponse(ctx context.Context, provider, key string, maxAge time.Duration) ([]byte, bool, error) {
	var payload []byte
	var fetched int64
	err := s.db.QueryRowContext(ctx,
		`SELECT payload, fetched_at FROM provider_cache WHERE provider = ? AND key = ?`,
		provider, key).Scan(&payload, &fetched)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cached response: %w", err)
	}
	if maxAge > 0 && time.Since(time.Unix(fetched, 0)) > maxAge {
		return nil, false, nil
	}
	return payload, true, nil
}

// CacheResponse stores a raw provider payload.
func (s *Store) CacheResponse(ctx context.Context, provider, key string, payload []byte) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_cache (provider, key, payload, fetched_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(provider, key) DO UPDATE SET payload = excluded.payload, fetched_at = excluded.fetched_at`,
		provider, key, payload, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("cache response: %w", err)
	}
	return nil
}

// ------------------------------------------------------------------- helpers

// LoadDetail attaches the detail-only fields to an item.
func (s *Store) LoadDetail(ctx context.Context, it *Item) error {
	var err error
	if it.Genres, err = s.Genres(ctx, it.ID); err != nil {
		return err
	}
	if it.Credits, err = s.Credits(ctx, it.ID); err != nil {
		return err
	}
	if it.Artwork, err = s.ItemArtwork(ctx, it.ID); err != nil {
		return err
	}
	// Same fallback the grid gets. An artist whose tile has a poster and whose
	// detail page has none reads as a bug in whichever one the user saw second.
	if it.Kind == "artist" && (it.Artwork == nil || it.Artwork.Poster == "") {
		one := []Item{*it}
		if err := s.inheritArtistPosters(ctx, one); err != nil {
			return err
		}
		it.Artwork = one[0].Artwork
	}
	if it.LockedFields, err = s.LockedFields(ctx, it.ID); err != nil {
		return err
	}
	if it.Ratings, err = s.ItemRatings(ctx, it.ID); err != nil {
		return err
	}
	// The name only — never the directory (see Item.FileName), and only for rows
	// that are actually files.
	//
	// A container's `path` is not a path. Artists and albums are keyed by a
	// synthetic string (`<library>::artist=ABBA`), and so is a season with no
	// "Season N" folder (`<show dir>::season=2`) — that is how ADR 0010 and
	// ADR 0024 give a fileless row a stable identity. Running filepath.Base over
	// one produced a "file name" of `TEST MUSIC LIBRARY::artist=ABBA` on the
	// artist page, and — because Base splits on the separator — a bare `DC` for
	// AC/DC, which looks like nothing so much as corrupted metadata.
	//
	// It also quietly defeated the rule above. `path` is deliberately never
	// serialized so the server's filesystem layout stays private, and this was
	// putting a fragment of it on screen through the back door.
	//
	// Container is the file's extension and the scanner sets it only for real
	// files: ADR 0010 made it nullable precisely so directory rows could exist
	// without one. It is therefore the honest test for "does this row have a
	// file", rather than a list of container kinds that would need updating
	// every time a new one is added.
	if it.Path != "" && it.Container != nil && *it.Container != "" {
		it.FileName = filepath.Base(it.Path)
	}
	return nil
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
func derefFloat(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func join(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}
