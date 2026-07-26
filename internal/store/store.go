// Package store owns all SQL. Nothing outside this package writes queries —
// callers use the typed methods below. See CLAUDE.md.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("not found")

// Store is a handle on the LANcast database.
type Store struct{ db *sql.DB }

// Open connects to the database at path and applies the schema. Schema
// application is idempotent, so this is safe on every start.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// modernc.org/sqlite serializes writes internally; a small pool avoids
	// spurious contention without starving concurrent reads.
	db.SetMaxOpenConns(4)

	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// ---------------------------------------------------------------- libraries

// Library is a configured root directory.
type Library struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	CreatedAt int64  `json:"created_at"`
	ScannedAt *int64 `json:"scanned_at"`
	ItemCount int    `json:"item_count"`
}

func (s *Store) CreateLibrary(ctx context.Context, name, kind, path string) (*Library, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO library (name, kind, path, created_at) VALUES (?, ?, ?, ?)`,
		name, kind, path, now)
	if err != nil {
		return nil, fmt.Errorf("create library: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("create library: %w", err)
	}
	return &Library{ID: id, Name: name, Kind: kind, Path: path, CreatedAt: now}, nil
}

func (s *Store) ListLibraries(ctx context.Context) ([]Library, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.id, l.name, l.kind, l.path, l.created_at, l.scanned_at,
		       (SELECT COUNT(*) FROM media_item m WHERE m.library_id = l.id AND m.missing = 0)
		FROM library l ORDER BY l.name`)
	if err != nil {
		return nil, fmt.Errorf("list libraries: %w", err)
	}
	defer rows.Close()

	out := []Library{}
	for rows.Next() {
		var l Library
		if err := rows.Scan(&l.ID, &l.Name, &l.Kind, &l.Path, &l.CreatedAt, &l.ScannedAt, &l.ItemCount); err != nil {
			return nil, fmt.Errorf("list libraries: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) GetLibrary(ctx context.Context, id int64) (*Library, error) {
	var l Library
	err := s.db.QueryRowContext(ctx, `
		SELECT l.id, l.name, l.kind, l.path, l.created_at, l.scanned_at,
		       (SELECT COUNT(*) FROM media_item m WHERE m.library_id = l.id AND m.missing = 0)
		FROM library l WHERE l.id = ?`, id).
		Scan(&l.ID, &l.Name, &l.Kind, &l.Path, &l.CreatedAt, &l.ScannedAt, &l.ItemCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get library: %w", err)
	}
	return &l, nil
}

func (s *Store) TouchLibraryScanned(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE library SET scanned_at = ? WHERE id = ?`, time.Now().Unix(), id)
	return err
}

// -------------------------------------------------------------------- items

// Item is one row of media_item. Path is never serialized: clients have no use
// for server filesystem paths, and withholding them keeps the layout private
// when the server is reachable beyond the LAN.
type Item struct {
	ID        int64  `json:"id"`
	LibraryID int64  `json:"library_id"`
	Kind      string `json:"kind"`
	Path      string `json:"-"`

	Title     string `json:"title"`
	SortTitle string `json:"-"`
	Year      *int   `json:"year"`

	Series  *string `json:"series"`
	Season  *int    `json:"season"`
	Episode *int    `json:"episode"`

	Container  *string `json:"container"`
	SizeBytes  *int64  `json:"size_bytes"`
	MTime      *int64  `json:"-"`
	DurationMS *int64  `json:"duration_ms"`

	AddedAt int64 `json:"added_at"`
	Missing bool  `json:"missing"`

	// M2 metadata.
	ParentID      *int64   `json:"parent_id"`
	Overview      *string  `json:"overview"`
	Rating        *float64 `json:"rating"`
	ContentRating *string  `json:"content_rating"`
	ReleasedAt    *int64   `json:"released_at"`
	Provider      *string  `json:"provider"`
	ExternalID    *string  `json:"external_id"`
	MatchState    string   `json:"match_state"`
	MatchScore    *float64 `json:"match_score"`

	// MetadataUpdatedAt is nil until enrichment has run. Clients need it to
	// distinguish "not looked at yet" from "looked and found nothing" —
	// match_state alone defaults to 'unmatched' and cannot express that.
	MetadataUpdatedAt *int64 `json:"metadata_updated_at"`

	// Probe results. Nil until the file has been inspected.
	ProbedAt      *int64   `json:"probed_at"`
	VideoCodec    *string  `json:"video_codec"`
	VideoProfile  *string  `json:"video_profile"`
	Width         *int     `json:"width"`
	Height        *int     `json:"height"`
	VideoBitRate  *int64   `json:"video_bitrate"`
	FrameRate     *float64 `json:"frame_rate"`
	AudioCodec    *string  `json:"audio_codec"`
	AudioChannels *int     `json:"audio_channels"`

	// Detail-only.
	Streams []MediaStream `json:"streams,omitempty"`

	// Detail-only; nil on list responses.
	Genres       []string `json:"genres,omitempty"`
	Credits      []Credit `json:"credits,omitempty"`
	Artwork      *Artwork `json:"artwork,omitempty"`
	LockedFields []string `json:"locked_fields,omitempty"`

	Progress *Progress `json:"progress,omitempty"`
}

// Credit is one person's involvement in an item.
type Credit struct {
	Name      string `json:"name"`
	Role      string `json:"role"`
	Character string `json:"character,omitempty"`
	Order     int    `json:"-"`
}

// Artwork holds the content-addressed hashes for an item's images.
type Artwork struct {
	Poster string `json:"poster,omitempty"`
	Fanart string `json:"fanart,omitempty"`
	Thumb  string `json:"thumb,omitempty"`
}

// Progress is a user's playback position for an item.
type Progress struct {
	PositionMS int64 `json:"position_ms"`
	Watched    bool  `json:"watched"`
}

// ScanFile is what the scanner knows about a file on disk.
type ScanFile struct {
	LibraryID int64
	Path      string
	Kind      string
	Title     string
	SortTitle string
	Year      *int
	Series    *string
	Season    *int
	Episode   *int
	Container string
	SizeBytes int64
	MTime     int64
}

// UpsertItem inserts or updates an item keyed on its unique path. It returns
// the row id, so callers that need to attach related records — subtitles, for
// one — do not have to re-query the whole library to find it.
func (s *Store) UpsertItem(ctx context.Context, f ScanFile) (int64, error) {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO media_item
			(library_id, kind, path, title, sort_title, year, series, season, episode,
			 container, size_bytes, mtime, added_at, updated_at, missing)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(path) DO UPDATE SET
			kind = excluded.kind, title = excluded.title, sort_title = excluded.sort_title,
			year = excluded.year, series = excluded.series, season = excluded.season,
			episode = excluded.episode, container = excluded.container,
			size_bytes = excluded.size_bytes, mtime = excluded.mtime,
			updated_at = excluded.updated_at, missing = 0,
			-- The scanner only upserts files whose size or mtime changed, so
			-- reaching here means the bytes are different and any previous
			-- probe describes a file that no longer exists.
			probed_at = NULL`,
		f.LibraryID, f.Kind, f.Path, f.Title, f.SortTitle, f.Year, f.Series, f.Season, f.Episode,
		f.Container, f.SizeBytes, f.MTime, now, now)
	if err != nil {
		return 0, fmt.Errorf("upsert item %q: %w", f.Path, err)
	}

	// LastInsertId is unreliable after an upsert that took the update branch,
	// so the id is read back by the unique key that identifies the row.
	var id int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT id FROM media_item WHERE path = ?`, f.Path).Scan(&id); err != nil {
		return 0, fmt.Errorf("upsert item %q: read id: %w", f.Path, err)
	}
	return id, nil
}

// FileState is the cheap change-detection tuple for one known file.
//
// Missing is part of it deliberately: a file that reappears byte-identical
// (remounted drive, reconnected share, restored backup) still needs an upsert
// to clear the flag, so size and mtime alone are not enough to decide a skip.
type FileState struct {
	ID        int64
	SizeBytes *int64
	MTime     *int64
	Missing   bool
}

// KnownFiles returns every non-directory item in a library keyed by path, so
// the scanner can skip unchanged files without re-parsing them.
func (s *Store) KnownFiles(ctx context.Context, libraryID int64) (map[string]FileState, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, path, size_bytes, mtime, missing FROM media_item WHERE library_id = ?`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("known files: %w", err)
	}
	defer rows.Close()

	out := map[string]FileState{}
	for rows.Next() {
		var path string
		var missing int
		var st FileState
		if err := rows.Scan(&st.ID, &path, &st.SizeBytes, &st.MTime, &missing); err != nil {
			return nil, fmt.Errorf("known files: %w", err)
		}
		st.Missing = missing != 0
		out[path] = st
	}
	return out, rows.Err()
}

// MarkMissing flags items as absent from disk. Rows are never deleted — an
// unmounted drive must not destroy library data, watch history, or user edits.
func (s *Store) MarkMissing(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mark missing: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `UPDATE media_item SET missing = 1, updated_at = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("mark missing: %w", err)
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, now, id); err != nil {
			return fmt.Errorf("mark missing %d: %w", id, err)
		}
	}
	return tx.Commit()
}

// ItemFilter narrows a listing. Zero values mean "no restriction".
type ItemFilter struct {
	LibraryID int64
	Kind      string
	Query     string
	Sort      string // title | year | added
	Limit     int
	Offset    int
}

const itemCols = `id, library_id, kind, path, title, sort_title, year, series, season, episode,
	container, size_bytes, mtime, duration_ms, added_at, missing,
	parent_id, overview, rating, content_rating, released_at, provider, external_id,
	match_state, match_score, metadata_updated_at,
	probed_at, video_codec, video_profile, width, height, video_bitrate,
	audio_codec, audio_channels, video_frame_rate`

// itemColsMI is itemCols qualified with the media_item alias "mi", for queries
// that join another table carrying same-named columns (duration_ms, watched).
var itemColsMI = qualifyCols(itemCols, "mi")

func qualifyCols(cols, alias string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

func scanItem(sc interface{ Scan(...any) error }) (*Item, error) {
	var it Item
	var missing int
	err := sc.Scan(&it.ID, &it.LibraryID, &it.Kind, &it.Path, &it.Title, &it.SortTitle,
		&it.Year, &it.Series, &it.Season, &it.Episode, &it.Container, &it.SizeBytes,
		&it.MTime, &it.DurationMS, &it.AddedAt, &missing,
		&it.ParentID, &it.Overview, &it.Rating, &it.ContentRating, &it.ReleasedAt,
		&it.Provider, &it.ExternalID, &it.MatchState, &it.MatchScore, &it.MetadataUpdatedAt,
		&it.ProbedAt, &it.VideoCodec, &it.VideoProfile, &it.Width, &it.Height,
		&it.VideoBitRate, &it.AudioCodec, &it.AudioChannels, &it.FrameRate)
	if err != nil {
		return nil, err
	}
	it.Missing = missing != 0
	return &it, nil
}

// ListItems returns a page of items plus the total matching the filter.
func (s *Store) ListItems(ctx context.Context, f ItemFilter) ([]Item, int, error) {
	where := ` WHERE 1=1`
	args := []any{}
	if f.LibraryID != 0 {
		where += ` AND library_id = ?`
		args = append(args, f.LibraryID)
	}
	if f.Kind != "" {
		where += ` AND kind = ?`
		args = append(args, f.Kind)
	}
	if f.Query != "" {
		where += ` AND (title LIKE ? OR series LIKE ?)`
		q := "%" + f.Query + "%"
		args = append(args, q, q)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_item`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count items: %w", err)
	}

	order := ` ORDER BY sort_title, season, episode`
	switch f.Sort {
	case "year":
		order = ` ORDER BY year DESC, sort_title`
	case "added":
		order = ` ORDER BY added_at DESC, sort_title`
	}

	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args = append(args, limit, f.Offset)

	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM media_item`+where+order+` LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list items: %w", err)
	}
	defer rows.Close()

	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("list items: %w", err)
		}
		out = append(out, *it)
	}
	return out, total, rows.Err()
}

// ContinueWatching returns a user's in-progress items, most recently played
// first: everything they have started but not finished. "Started" means a saved
// position past zero; "not finished" means the watched flag is unset, so an item
// played to the end drops off the shelf rather than inviting a replay.
func (s *Store) ContinueWatching(ctx context.Context, userID string, limit int) ([]Item, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColsMI+`
		FROM media_item mi
		JOIN playback_state ps ON ps.item_id = mi.id AND ps.user_id = ?
		WHERE ps.position_ms > 0 AND ps.watched = 0 AND mi.missing = 0
		ORDER BY ps.updated_at DESC
		LIMIT ?`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("continue watching: %w", err)
	}
	defer rows.Close()

	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("continue watching: %w", err)
		}
		out = append(out, *it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reuse the one progress-attach path rather than widening the scan.
	if err := s.AttachProgress(ctx, out, userID); err != nil {
		return nil, err
	}
	return out, nil
}

// GetItem returns one item with the given user's playback progress attached.
func (s *Store) GetItem(ctx context.Context, id int64, userID string) (*Item, error) {
	it, err := scanItem(s.db.QueryRowContext(ctx, `SELECT `+itemCols+` FROM media_item WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}

	var p Progress
	var watched int
	err = s.db.QueryRowContext(ctx,
		`SELECT position_ms, watched FROM playback_state WHERE item_id = ? AND user_id = ?`,
		id, userID).Scan(&p.PositionMS, &watched)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// No progress yet; leave it nil.
	case err != nil:
		return nil, fmt.Errorf("get item progress: %w", err)
	default:
		p.Watched = watched != 0
		it.Progress = &p
	}
	return it, nil
}

// AttachProgress fills in Progress for a page of items in one query, so a grid
// render is two queries rather than one per tile.
func (s *Store) AttachProgress(ctx context.Context, items []Item, userID string) error {
	if len(items) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT item_id, position_ms, watched FROM playback_state WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("attach progress: %w", err)
	}
	defer rows.Close()

	byID := map[int64]Progress{}
	for rows.Next() {
		var id int64
		var p Progress
		var watched int
		if err := rows.Scan(&id, &p.PositionMS, &watched); err != nil {
			return fmt.Errorf("attach progress: %w", err)
		}
		p.Watched = watched != 0
		byID[id] = p
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range items {
		if p, ok := byID[items[i].ID]; ok {
			pp := p
			items[i].Progress = &pp
		}
	}
	return nil
}

// SaveProgress records a playback position for a user.
func (s *Store) SaveProgress(ctx context.Context, itemID int64, userID string, positionMS int64, watched bool) error {
	w := 0
	if watched {
		w = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO playback_state (item_id, user_id, position_ms, watched, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(item_id, user_id) DO UPDATE SET
			position_ms = excluded.position_ms,
			watched = excluded.watched,
			updated_at = excluded.updated_at`,
		itemID, userID, positionMS, w, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("save progress: %w", err)
	}
	return nil
}
