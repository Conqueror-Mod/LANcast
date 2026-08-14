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
	// _txlock=immediate takes the write lock when a transaction begins rather
	// than when it first writes.
	//
	// Without it, a transaction starts as a reader, and any read it performs —
	// including the schema read a PREPARE does — fixes a snapshot. If another
	// connection commits before the transaction's first write, SQLite cannot
	// upgrade the stale snapshot and fails with SQLITE_BUSY_SNAPSHOT (517).
	// busy_timeout does not cover 517: it defers plain SQLITE_BUSY (5), while
	// 517 returns immediately, so the transaction does not wait and retry, it
	// dies. That is how a TV Shows scan aborted at 15 files with "database is
	// locked" while enrichment was writing beside it.
	//
	// Every transaction in this package writes, so taking the lock up front
	// costs no read concurrency — it converts an instant failure into the wait
	// busy_timeout was always meant to provide.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate", path)
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
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Path is the library's first location, kept so clients that predate
	// multi-root libraries keep working (ADR 0034). Superseded by Roots, which
	// is what anything new should read.
	Path      string        `json:"path"`
	Roots     []LibraryRoot `json:"roots,omitempty"`
	CreatedAt int64         `json:"created_at"`
	ScannedAt *int64        `json:"scanned_at"`
	ItemCount int           `json:"item_count"`
}

/*
 * firstRoot is the library's path as everything above this layer still
 * understands it (ADR 0034).
 *
 * A library's roots live in `library_root` from revision 18, and `Library.Path`
 * is the first of them by id — the one the library was created with. It is a
 * correlated subquery rather than a join so a library with several roots still
 * produces exactly one row per library, which is what every caller of
 * ListLibraries already assumes.
 *
 * This exists so the schema could move without thirty call sites moving on the
 * same day. It is a compatibility shim with a deliberately short life: the
 * handlers that resolve an item to a file must end up using the *item's* root,
 * not the library's first one, and until they do a multi-root library would
 * resolve its second root's files against its first root's path. That is why
 * nothing yet creates a second root.
 */
const firstRootPath = `(SELECT r.path FROM library_root r
	WHERE r.library_id = l.id ORDER BY r.id LIMIT 1)`

func (s *Store) CreateLibrary(ctx context.Context, name, kind, path string) (*Library, error) {
	now := time.Now().Unix()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("create library: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO library (name, kind, created_at) VALUES (?, ?, ?)`,
		name, kind, now)
	if err != nil {
		return nil, fmt.Errorf("create library: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("create library: %w", err)
	}

	// The library and its first root are one act, so they are one transaction.
	// A library with no root is not a degraded library, it is a row nothing can
	// scan, resolve or repoint — and the UNIQUE on path means the second
	// statement is the one that rejects a duplicate location, which has to
	// take the first statement down with it.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO library_root (library_id, path, created_at) VALUES (?, ?, ?)`,
		id, path, now); err != nil {
		return nil, fmt.Errorf("create library: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("create library: %w", err)
	}
	return &Library{ID: id, Name: name, Kind: kind, Path: path, CreatedAt: now}, nil
}

// topLevelPredicate selects the rows the browse grid shows as tiles: no
// parent, and a collection only when it groups at least two present members. A
// provider hands us a franchise even when the library holds a single film from
// it, and a collection of one is just a duplicate tile of that film
// (ADR 0010, ADR 0017).
//
// It is a shared constant because a library's item count and the grid must
// answer the same question. When they were written separately, the count
// included season and episode rows: a shows library with three series read
// "21 items" beside a grid holding three tiles. Any second copy of this rule
// drifts from the first the same way.
//
// References `media_item` unqualified, so a query using it must not alias the
// table.
const topLevelPredicate = `parent_id IS NULL
	AND (kind != 'collection' OR (
		SELECT COUNT(*) FROM item_collection ic
		JOIN media_item m2 ON m2.id = ic.item_id
		WHERE ic.collection_id = media_item.id AND m2.missing = 0
	) >= 2)`

// libraryItemCount counts what the grid would show for one library, as a
// correlated subquery against the `library l` row.
const libraryItemCount = `(SELECT COUNT(*) FROM media_item
	WHERE library_id = l.id AND missing = 0 AND ` + topLevelPredicate + `)`

func (s *Store) ListLibraries(ctx context.Context) ([]Library, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.id, l.name, l.kind, `+firstRootPath+`, l.created_at, l.scanned_at,
		       `+libraryItemCount+`
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list libraries: %w", err)
	}

	// One query for every library's locations, attached in memory. A per-library
	// lookup here would be a round trip each to render a page that is mostly
	// names.
	byLib, err := s.RootsByLibrary(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Roots = byLib[out[i].ID]
	}
	return out, nil
}

func (s *Store) GetLibrary(ctx context.Context, id int64) (*Library, error) {
	var l Library
	err := s.db.QueryRowContext(ctx, `
		SELECT l.id, l.name, l.kind, `+firstRootPath+`, l.created_at, l.scanned_at,
		       `+libraryItemCount+`
		FROM library l WHERE l.id = ?`, id).
		Scan(&l.ID, &l.Name, &l.Kind, &l.Path, &l.CreatedAt, &l.ScannedAt, &l.ItemCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get library: %w", err)
	}
	roots, err := s.ListRoots(ctx, l.ID)
	if err != nil {
		return nil, err
	}
	l.Roots = roots
	return &l, nil
}

func (s *Store) TouchLibraryScanned(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE library SET scanned_at = ? WHERE id = ?`, time.Now().Unix(), id)
	return err
}

// DeleteLibrary removes a library and, by ON DELETE CASCADE, its items and
// everything hanging off them (playback state, subtitles). It touches nothing
// on disk: LANcast only ever stored paths, so forgetting a library never
// destroys media. Returns ErrNotFound if there was no such library.
func (s *Store) DeleteLibrary(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM library WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete library: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
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

	Series *string `json:"series"`
	// Artist is a music track's own performer, which on a compilation differs
	// from the album artist carried by the container above it (ADR 0024).
	Artist  *string `json:"artist,omitempty"`
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

	// IMDbID is TMDB's external imdb id for the item, the key third-party rating
	// services join on (ADR 0019). Not exposed to clients — they read ratings
	// through the ratings array, not this id.
	IMDbID *string `json:"-"`

	// MetadataUpdatedAt is nil until enrichment has run. Clients need it to
	// distinguish "not looked at yet" from "looked and found nothing" —
	// match_state alone defaults to 'unmatched' and cannot express that.
	MetadataUpdatedAt *int64 `json:"metadata_updated_at"`

	// Probe results. Nil until the file has been inspected.
	ProbedAt     *int64  `json:"probed_at"`
	VideoCodec   *string `json:"video_codec"`
	VideoProfile *string `json:"video_profile"`
	Width        *int    `json:"width"`
	Height       *int    `json:"height"`
	// TakenAt is EXIF capture time for a photo (ADR 0028), null when the file
	// carries none — which is most of a wallpaper library. Distinct from
	// AddedAt: one is when the picture was made, the other when it reached this
	// disk.
	TakenAt       *int64   `json:"taken_at,omitempty"`
	VideoBitRate  *int64   `json:"video_bitrate"`
	FrameRate     *float64 `json:"frame_rate"`
	AudioCodec    *string  `json:"audio_codec"`
	AudioChannels *int     `json:"audio_channels"`

	// ChildCount is how many (present) items name this one as parent — nonzero
	// for a container (show, season, collection, multi-part work). It lets a
	// client tell a container from a leaf without a follow-up query, so a
	// movie-parent of parts is not offered a dead-end Play (ADR 0017).
	ChildCount int `json:"child_count,omitempty"`

	// Detail-only.
	Streams []MediaStream `json:"streams,omitempty"`

	// FileName is the base name of the file, detail-only. The full path stays
	// private — it would disclose the server's filesystem layout — but the name
	// alone is what identifies a title whose metadata is wrong, and without it a
	// mis-scanned file ("01 Magnetic Rose") cannot be told apart from its
	// siblings well enough to correct it.
	FileName string `json:"file_name,omitempty"`

	// Detail-only; nil on list responses.
	Genres       []string     `json:"genres,omitempty"`
	Credits      []Credit     `json:"credits,omitempty"`
	Artwork      *Artwork     `json:"artwork,omitempty"`
	LockedFields []string     `json:"locked_fields,omitempty"`
	Ratings      []ItemRating `json:"ratings,omitempty"`

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

	// Inherited marks a poster borrowed from a child rather than owned. Today
	// that is an artist wearing one of its albums' covers, because nothing on
	// disk is an artist photograph — the images sitting in an artist folder are
	// a media player's per-album cache, not a picture of the band.
	//
	// Reported rather than hidden so a client can treat it as the placeholder
	// it is, and so "why is this artist showing that album's sleeve" has an
	// answer. It is never stored: the fallback stops applying by itself the
	// moment a real artist image exists.
	Inherited bool `json:"inherited,omitempty"`
}

// Progress is a user's playback position for an item.
type Progress struct {
	PositionMS int64 `json:"position_ms"`
	Watched    bool  `json:"watched"`
}

// ScanFile is what the scanner knows about a file on disk.
type ScanFile struct {
	LibraryID int64
	// RootID is the location this file was walked under (ADR 0034).
	//
	// Not derivable from LibraryID once a library has more than one: it is the
	// scanner that knows which root it was walking, and it is the only thing
	// that knows. Every containment check downstream resolves against this, so
	// a file written without it is a file nothing can later serve.
	RootID    int64
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

	/*
	 * A caller that did not say which location this file came from.
	 *
	 * Resolved from the library, but only while that is unambiguous. A library
	 * with one root has exactly one answer and inferring it saves every caller
	 * that predates roots from having to care. A library with several has no
	 * answer worth guessing: picking the first would file the item under a
	 * location it is not in, and the containment check downstream would then
	 * resolve it against the wrong directory — quietly, and in the one place
	 * this project treats as a security boundary.
	 *
	 * So it refuses instead. This cannot rot into a silent mis-assignment the
	 * day a second root appears, because the day a second root appears it starts
	 * erroring loudly at whichever caller never learned to pass one.
	 */
	if f.RootID == 0 {
		roots, err := s.ListRoots(ctx, f.LibraryID)
		if err != nil {
			return 0, fmt.Errorf("upsert item %q: %w", f.Path, err)
		}
		switch len(roots) {
		case 0:
			return 0, fmt.Errorf("upsert item %q: library %d has no location", f.Path, f.LibraryID)
		case 1:
			f.RootID = roots[0].ID
		default:
			return 0, fmt.Errorf(
				"upsert item %q: library %d has %d locations and no root was given",
				f.Path, f.LibraryID, len(roots))
		}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO media_item
			(library_id, root_id, kind, path, title, sort_title, year, series, season, episode,
			 container, size_bytes, mtime, added_at, updated_at, missing)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(path) DO UPDATE SET
			-- root_id is refreshed too. A file can change roots without changing
			-- path only if the roots themselves moved, which RepointRoot does --
			-- but a row left pointing at a stale root is a row whose containment
			-- check resolves against the wrong directory, so it is cheaper to
			-- keep it current than to reason about when it cannot drift.
			root_id = excluded.root_id,
			kind = excluded.kind, title = excluded.title, sort_title = excluded.sort_title,
			year = excluded.year, series = excluded.series, season = excluded.season,
			episode = excluded.episode, container = excluded.container,
			size_bytes = excluded.size_bytes, mtime = excluded.mtime,
			updated_at = excluded.updated_at, missing = 0,
			-- The scanner only upserts files whose size or mtime changed, so
			-- reaching here means the bytes are different and any previous
			-- probe describes a file that no longer exists.
			probed_at = NULL`,
		f.LibraryID, f.RootID, f.Kind, f.Path, f.Title, f.SortTitle, f.Year, f.Series, f.Season, f.Episode,
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
	Kind      string
	SizeBytes *int64
	MTime     *int64
	Missing   bool
}

// KnownFiles returns every file-backed item in a library keyed by path, so the
// scanner can skip unchanged files without re-parsing them.
//
// It excludes rows that are not files on disk — shows, seasons, and collections
// have a null container and a directory or synthetic path (ADR 0010, ADR 0017).
// Including them would be a data-loss bug: the walk only ever "sees" video
// files, so every container row would be absent from the seen set and marked
// missing on the next scan.
func (s *Store) KnownFiles(ctx context.Context, libraryID int64) (map[string]FileState, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, kind, path, size_bytes, mtime, missing FROM media_item
		 WHERE library_id = ? AND container IS NOT NULL`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("known files: %w", err)
	}
	defer rows.Close()

	out := map[string]FileState{}
	for rows.Next() {
		var path string
		var missing int
		var st FileState
		if err := rows.Scan(&st.ID, &st.Kind, &path, &st.SizeBytes, &st.MTime, &missing); err != nil {
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

// bucketInitial maps a first character to the bucket the A–Z rail shows it in:
// itself when it is a Latin letter, "#" for everything else.
//
// Everything else really is everything else — a number, a bracket, a Cyrillic
// or Japanese title. Transliterating would be a second normalizer with an
// opinion about scripts, and the one rule this project has about normalizers is
// that a second one disagrees with the first.
func bucketInitial(c string) string {
	if c == "" {
		return "#"
	}
	r := []rune(strings.ToUpper(c))[0]
	if r >= 'A' && r <= 'Z' {
		return string(r)
	}
	return "#"
}

// ItemFilter narrows a listing. Zero values mean "no restriction".
type ItemFilter struct {
	LibraryID int64
	Kind      string
	Query     string
	Sort      string // title | year | added | rating | track

	// Facet filters. Each is OR within itself and AND across facets — the Plex
	// semantics: pick two genres to widen, add a decade to narrow. Empty slices
	// mean no restriction on that facet.
	Genres         []string // exact genre names
	Decades        []int    // e.g. 1990 restricts to 1990–1999
	ContentRatings []string // exact content-rating labels (PG, R, TV-MA…)

	// Unwatched restricts to items the user has not finished. It is keyed by
	// UserID, which must be set when Unwatched is true. Watched state lives on the
	// leaf's own playback row, so this filters movies and episodes; a container
	// (a show) carries no watched flag and is unaffected.
	Unwatched bool
	UserID    string

	// Initial restricts to items whose sort_title starts with this letter, or
	// with anything that is not a Latin letter when it is "#". The A–Z rail.
	//
	// A filter rather than a scroll offset: the grid pages in as you scroll, so
	// "jump to S" cannot mean "scroll to a row that is not loaded". Asking the
	// server for the S items is the same gesture with an answer that exists.
	Initial string

	// ExcludeKind drops one kind from a listing.
	//
	// For the browse grid, where collections were mixed in among the films they
	// group: a franchise tile beside its own members, sorted by a title nobody
	// chose, in a grid whose job is "what have I got". They are a different
	// question — "what belongs together" — and they get their own page. Empty
	// means no exclusion, which is every other caller.
	ExcludeKind string

	// TopLevel restricts the listing to rows with no parent — the browse-grid
	// default. Children (seasons, episodes, parts, chapters) have a parent_id
	// and belong under it, never loose in the grid; this is the guard ADR 0010
	// and ADR 0017 require so a container's pieces do not leak in as if they
	// were features. Ignored when ParentID is set.
	TopLevel bool

	// ParentID, when non-nil, returns exactly the children of that item.
	// Overrides TopLevel.
	//
	// Ordering is whatever Sort says, and the default is *not* hierarchy order
	// in general: it works for episodes only because they share their series'
	// sort title and tie. An album's tracks need Sort "track".
	ParentID *int64

	Limit  int
	Offset int
}

const itemCols = `id, library_id, kind, path, title, sort_title, year, series, season, episode,
	container, size_bytes, mtime, duration_ms, added_at, missing,
	parent_id, overview, rating, content_rating, released_at, provider, external_id,
	match_state, match_score, metadata_updated_at,
	probed_at, video_codec, video_profile, width, height, video_bitrate,
	audio_codec, audio_channels, video_frame_rate, imdb_id, artist, taken_at`

// itemColsMI is itemCols qualified with the media_item alias "mi", for queries
// that join another table carrying same-named columns (duration_ms, watched).
var itemColsMI = qualifyCols(itemCols, "mi")

// placeholders returns "?, ?, …" with n slots, for an IN (…) clause whose
// values are appended to the args slice in the same order.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?, ", n-1) + "?"
}

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
		&it.VideoBitRate, &it.AudioCodec, &it.AudioChannels, &it.FrameRate, &it.IMDbID,
		&it.Artist, &it.TakenAt)
	if err != nil {
		return nil, err
	}
	it.Missing = missing != 0
	return &it, nil
}

// scanItems drains rows selecting the item columns (in either the bare or the
// "mi"-qualified order, which scan identically) into a slice.
func scanItems(rows *sql.Rows) ([]Item, error) {
	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
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
	switch {
	case f.ParentID != nil:
		where += ` AND parent_id = ?`
		args = append(args, *f.ParentID)
	case f.TopLevel:
		where += ` AND ` + topLevelPredicate
	}
	if f.Query != "" {
		where += ` AND (title LIKE ? OR series LIKE ?)`
		q := "%" + f.Query + "%"
		args = append(args, q, q)
	}
	if f.ExcludeKind != "" {
		where += ` AND kind != ?`
		args = append(args, f.ExcludeKind)
	}
	if f.Initial != "" {
		if f.Initial == "#" {
			// Everything that does not begin with a Latin letter. GLOB is
			// case-sensitive in SQLite, which is what makes the range test mean
			// what it says here.
			where += ` AND UPPER(SUBSTR(sort_title, 1, 1)) NOT GLOB '[A-Z]'`
		} else {
			where += ` AND UPPER(SUBSTR(sort_title, 1, 1)) = ?`
			args = append(args, strings.ToUpper(f.Initial))
		}
	}
	if len(f.Genres) > 0 {
		// EXISTS rather than a join, so a multi-genre item is not duplicated in
		// the result or double-counted in the total. IN over the chosen names is
		// the OR-within-facet part.
		where += ` AND EXISTS (
			SELECT 1 FROM item_genre ig JOIN genre g ON g.id = ig.genre_id
			WHERE ig.item_id = media_item.id AND g.name IN (` + placeholders(len(f.Genres)) + `))`
		for _, g := range f.Genres {
			args = append(args, g)
		}
	}
	if len(f.Decades) > 0 {
		parts := make([]string, len(f.Decades))
		for i, d := range f.Decades {
			parts[i] = `(year >= ? AND year <= ?)`
			args = append(args, d, d+9)
		}
		where += ` AND (` + strings.Join(parts, " OR ") + `)`
	}
	if len(f.ContentRatings) > 0 {
		where += ` AND content_rating IN (` + placeholders(len(f.ContentRatings)) + `)`
		for _, cr := range f.ContentRatings {
			args = append(args, cr)
		}
	}
	if f.Unwatched {
		// Not finished for this user: no playback row with the watched flag set.
		// An in-progress-but-unfinished item (watched = 0) counts as unwatched,
		// matching the continue-watching semantics.
		where += ` AND NOT EXISTS (
			SELECT 1 FROM playback_state ps
			WHERE ps.item_id = media_item.id AND ps.user_id = ? AND ps.watched = 1)`
		args = append(args, f.UserID)
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
	case "rating":
		// Highest rated first; unrated rows sink to the bottom rather than
		// sorting as if they were zero-rated.
		order = ` ORDER BY rating IS NULL, rating DESC, sort_title`
	case "taken":
		// When the picture was made, newest first, falling back to when the file
		// reached this disk. A photo library sorted purely by taken_at would put
		// every EXIF-less wallpaper in one undifferentiated block; COALESCE
		// keeps them in a sensible place among the dated ones instead (ADR 0028).
		order = ` ORDER BY COALESCE(taken_at, mtime, added_at) DESC, sort_title`
	case "track":
		// Disc, then track number — an album in the order the record plays.
		//
		// This has to be asked for explicitly, because the default cannot
		// deliver it. A track keeps its own title as its sort title (unlike an
		// episode, which inherits its series' and therefore ties with every
		// other episode, letting the default fall through to season/episode).
		// Tracks never tie, so under the default an album comes back in
		// alphabetical order. Making the default lead with season/episode would
		// fix music by interleaving every show's season 1 ahead of any
		// season 2 in a cross-show listing, so it stays a separate sort.
		order = ` ORDER BY season, episode, sort_title`
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

// Facets is the set of filter values present in a library — what a browse view
// offers in its filter controls. Only values actually on top-level, present
// items are returned, so a filter never yields an empty grid. HasWatched says
// whether any top-level item is watched by this user, so the client offers the
// unwatched-only toggle only when it would actually remove something.
type Facets struct {
	Genres         []string `json:"genres"`
	Decades        []int    `json:"decades"`
	ContentRatings []string `json:"content_ratings"`
	HasWatched     bool     `json:"has_watched"`
	// Initials are the first letters present among this library's top-level
	// items, for the A–Z rail. Uppercase letters, plus "#" for everything that
	// does not start with one — a number, a bracket, a non-Latin script.
	//
	// Returned rather than assumed, because a rail of twenty-six letters where
	// nineteen do nothing is a control that lies about what is there. Sorted,
	// with "#" first, which is where a list sorted by sort_title puts it.
	Initials []string `json:"initials"`
}

// LibraryFacets returns the filter values present among a library's top-level
// items, and whether this user has watched any of them.
func (s *Store) LibraryFacets(ctx context.Context, libraryID int64, userID string) (Facets, error) {
	f := Facets{Genres: []string{}, Decades: []int{}, ContentRatings: []string{}, Initials: []string{}}

	// The initials present, computed the same way InitialFilter selects on so
	// the rail can never offer a letter the filter then finds nothing for.
	irows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT UPPER(SUBSTR(sort_title, 1, 1)) AS initial
		FROM media_item
		WHERE library_id = ? AND missing = 0 AND `+topLevelPredicate+`
		  AND sort_title != ''
		ORDER BY initial`, libraryID)
	if err != nil {
		return f, fmt.Errorf("library facets (initials): %w", err)
	}
	defer irows.Close()
	seen := map[string]bool{}
	for irows.Next() {
		var c string
		if err := irows.Scan(&c); err != nil {
			return f, fmt.Errorf("library facets (initials): %w", err)
		}
		key := bucketInitial(c)
		if !seen[key] {
			seen[key] = true
		}
	}
	if err := irows.Err(); err != nil {
		return f, err
	}
	if seen["#"] {
		f.Initials = append(f.Initials, "#")
	}
	for c := 'A'; c <= 'Z'; c++ {
		if seen[string(c)] {
			f.Initials = append(f.Initials, string(c))
		}
	}

	grows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT g.name FROM genre g
		JOIN item_genre ig ON ig.genre_id = g.id
		JOIN media_item m ON m.id = ig.item_id
		WHERE m.library_id = ? AND m.parent_id IS NULL AND m.missing = 0
		ORDER BY g.name`, libraryID)
	if err != nil {
		return f, fmt.Errorf("library facets (genres): %w", err)
	}
	defer grows.Close()
	for grows.Next() {
		var name string
		if err := grows.Scan(&name); err != nil {
			return f, fmt.Errorf("library facets (genres): %w", err)
		}
		f.Genres = append(f.Genres, name)
	}
	if err := grows.Err(); err != nil {
		return f, err
	}

	drows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT (year / 10) * 10 AS decade FROM media_item
		WHERE library_id = ? AND parent_id IS NULL AND missing = 0 AND year IS NOT NULL
		ORDER BY decade DESC`, libraryID)
	if err != nil {
		return f, fmt.Errorf("library facets (decades): %w", err)
	}
	defer drows.Close()
	for drows.Next() {
		var d int
		if err := drows.Scan(&d); err != nil {
			return f, fmt.Errorf("library facets (decades): %w", err)
		}
		f.Decades = append(f.Decades, d)
	}
	if err := drows.Err(); err != nil {
		return f, err
	}

	crows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT content_rating FROM media_item
		WHERE library_id = ? AND parent_id IS NULL AND missing = 0
			AND content_rating IS NOT NULL AND content_rating != ''
		ORDER BY content_rating`, libraryID)
	if err != nil {
		return f, fmt.Errorf("library facets (content ratings): %w", err)
	}
	defer crows.Close()
	for crows.Next() {
		var cr string
		if err := crows.Scan(&cr); err != nil {
			return f, fmt.Errorf("library facets (content ratings): %w", err)
		}
		f.ContentRatings = append(f.ContentRatings, cr)
	}
	if err := crows.Err(); err != nil {
		return f, err
	}

	// Whether the unwatched-only toggle is worth offering: true when this user
	// has finished at least one top-level item, so filtering them out removes
	// something rather than being a silent no-op.
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM media_item m
			JOIN playback_state ps ON ps.item_id = m.id
			WHERE m.library_id = ? AND m.parent_id IS NULL AND m.missing = 0
				AND ps.user_id = ? AND ps.watched = 1)`,
		libraryID, userID).Scan(&f.HasWatched); err != nil {
		return f, fmt.Errorf("library facets (has watched): %w", err)
	}
	return f, nil
}

// SetParent records that childID is contained by parentID — a season under a
// show, a part or chapter under a work (ADR 0010, ADR 0017). Passing a nil
// parent detaches the child, so a re-parent is one call. The child drops out of
// the top-level browse grid the moment this is set, because ListItems' default
// excludes rows with a parent.
func (s *Store) SetParent(ctx context.Context, childID int64, parentID *int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE media_item SET parent_id = ?, updated_at = ? WHERE id = ?`,
		parentID, time.Now().Unix(), childID)
	if err != nil {
		return fmt.Errorf("set parent of %d: %w", childID, err)
	}
	return nil
}

// Children returns the items contained by parentID, in hierarchy order. It is
// the same query ListItems runs for a ParentID filter, exposed directly for
// callers assembling a detail page (a show's episodes, a work's parts).
func (s *Store) Children(ctx context.Context, parentID int64) ([]Item, error) {
	items, _, err := s.ListItems(ctx, ItemFilter{ParentID: &parentID, Limit: 500})
	return items, err
}

// EnsureShow find-or-creates the show media_item for a series directory,
// returning its id and whether it was just created. Identity is the show
// directory path, which is UNIQUE and is also where tvshow.nfo is written
// (ADR 0010). Unlike a collection, a show is a real metadata subject: it is
// left pending (metadata_updated_at null) so the enricher fetches its poster,
// overview, and cast like any other item.
//
// sortTitle must be normalized by the caller through internal/media.
func (s *Store) EnsureShow(ctx context.Context, libraryID int64, path, title, sortTitle string) (int64, bool, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO media_item
			(library_id, kind, path, title, sort_title, series, added_at, updated_at, missing)
		VALUES (?, 'show', ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(path) DO NOTHING`,
		libraryID, path, title, sortTitle, title, now, now)
	if err != nil {
		return 0, false, fmt.Errorf("ensure show %q: %w", path, err)
	}
	created := false
	if n, err := res.RowsAffected(); err == nil {
		created = n == 1
	}
	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM media_item WHERE path = ?`, path).Scan(&id); err != nil {
		return 0, false, fmt.Errorf("ensure show %q: read id: %w", path, err)
	}
	return id, created, nil
}

// EnsureSeason find-or-creates a season under a show. path is the season
// directory when one exists, else a synthetic identity the caller derives from
// the show and season number, so it stays UNIQUE either way. A season is
// stamped resolved at birth: its identity comes from the show, and the provider
// season endpoint (its own poster, overview) is deferred depth — enriching it
// today would only re-fetch the show it already hangs off.
//
// sortTitle must be normalized by the caller through internal/media.
func (s *Store) EnsureSeason(ctx context.Context, libraryID, showID int64, seasonNum int, path, title, sortTitle string) (int64, bool, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO media_item
			(library_id, kind, path, title, sort_title, season, parent_id,
			 match_state, match_score, metadata_updated_at, added_at, updated_at, missing)
		VALUES (?, 'season', ?, ?, ?, ?, ?, 'matched', 1, ?, ?, ?, 0)
		ON CONFLICT(path) DO NOTHING`,
		libraryID, path, title, sortTitle, seasonNum, showID, now, now, now)
	if err != nil {
		return 0, false, fmt.Errorf("ensure season %q: %w", path, err)
	}
	created := false
	if n, err := res.RowsAffected(); err == nil {
		created = n == 1
	}
	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM media_item WHERE path = ?`, path).Scan(&id); err != nil {
		return 0, false, fmt.Errorf("ensure season %q: read id: %w", path, err)
	}
	return id, created, nil
}

// EnsureCollection finds or creates the collection media_item for a provider's
// grouping (TMDB's belongs_to_collection), returning its id and whether it was
// just created — so a caller downloads collection artwork once, not once per
// member. Identity is the synthetic, deterministic path, which the UNIQUE
// constraint makes idempotent under the concurrent enrichment workers.
//
// The row is stamped metadata_updated_at and match_state 'matched' at birth: a
// collection is resolved the moment it is created (its identity came straight
// from the provider), so it must never enter the enrichment queue, which has no
// provider that can search for kind 'collection' anyway.
//
// The synthetic path is not a filesystem path and never resolves to one — the
// containment checks that guard file-serving handlers reject it, which is
// correct: a collection has no bytes to stream.
//
// sortTitle must already be normalized by the caller through the single
// normalizer in internal/media — store owns no title-normalization opinion.
func (s *Store) EnsureCollection(ctx context.Context, libraryID int64, provider, externalID, name, sortTitle string) (int64, bool, error) {
	path := fmt.Sprintf("lancast:collection:%s:%s", provider, externalID)
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO media_item
			(library_id, kind, path, title, sort_title, provider, external_id,
			 match_state, match_score, metadata_updated_at, added_at, updated_at, missing)
		VALUES (?, 'collection', ?, ?, ?, ?, ?, 'matched', 1, ?, ?, ?, 0)
		ON CONFLICT(path) DO NOTHING`,
		libraryID, path, name, sortTitle, provider, externalID, now, now, now)
	if err != nil {
		return 0, false, fmt.Errorf("ensure collection %q: %w", externalID, err)
	}
	created := false
	if n, err := res.RowsAffected(); err == nil {
		created = n == 1
	}
	var id int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT id FROM media_item WHERE path = ?`, path).Scan(&id); err != nil {
		return 0, false, fmt.Errorf("ensure collection %q: read id: %w", externalID, err)
	}
	return id, created, nil
}

// LibraryEpisodes returns every episode row in a library, for the scanner's
// hierarchy reconciliation. It returns all of them, not only the unparented
// ones, so a series that was re-organised on disk re-parents correctly rather
// than keeping a stale link.
func (s *Store) LibraryEpisodes(ctx context.Context, libraryID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM media_item WHERE library_id = ? AND kind = 'episode'`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("library episodes: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// LibraryMovieFiles returns the file-backed movie rows in a library — the
// candidates the scanner re-parses for multi-part grouping. It excludes the
// synthetic parent-work rows (which have a null container) so a work is never
// mistaken for one of its own parts.
func (s *Store) LibraryMovieFiles(ctx context.Context, libraryID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM media_item
		 WHERE library_id = ? AND kind = 'movie' AND container IS NOT NULL`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("library movie files: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// LibraryPhotoFiles returns every photo *file* in a picture library (ADR 0028),
// unpaged, for the gallery reconciler.
//
// container IS NOT NULL is what distinguishes a file from a directory row, the
// same test LibraryMovieFiles uses. Unpaged because reconciliation has to see
// the whole library at once — a gallery derived from half the photos in a
// folder is a gallery that loses members on the next scan.
func (s *Store) LibraryPhotoFiles(ctx context.Context, libraryID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM media_item
		 WHERE library_id = ? AND kind = 'photo' AND container IS NOT NULL
		 ORDER BY path`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("library photo files: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// EnsureWork find-or-creates the parent media_item for a multi-part film — the
// work "Baahubali" over its two parts (ADR 0017). Per that ADR the parent is a
// 'movie', a container with no file of its own; its parts carry the files. It is
// left pending so the enricher gives the work its own poster and overview.
//
// Identity is a synthetic path derived from the library and the normalized work
// title, so the same work groups idempotently across rescans. sortTitle must be
// normalized by the caller through internal/media.
func (s *Store) EnsureWork(ctx context.Context, libraryID int64, workKey, title, sortTitle string) (int64, bool, error) {
	return s.ensureContainer(ctx, libraryID, "movie",
		fmt.Sprintf("lancast:work:%d:%s", libraryID, workKey), title, sortTitle)
}

// EnsureSerial find-or-creates the parent for a chaptered serial or miniseries —
// a closed, finite story played through as a whole (ADR 0017). Unlike a
// multi-part film's 'movie' parent, its kind is 'serial', which is what tells a
// client "play the whole thing" rather than "pick a film".
func (s *Store) EnsureSerial(ctx context.Context, libraryID int64, workKey, title, sortTitle string) (int64, bool, error) {
	return s.ensureContainer(ctx, libraryID, "serial",
		fmt.Sprintf("lancast:serial:%d:%s", libraryID, workKey), title, sortTitle)
}

// ensureContainer is the shared body behind EnsureWork and EnsureSerial: a
// no-file parent row on a synthetic, deterministic path, left pending so the
// enricher gives it its own artwork. The distinct path schemes keep a work and a
// serial that happen to share a title from colliding on the UNIQUE path.
func (s *Store) ensureContainer(ctx context.Context, libraryID int64, kind, path, title, sortTitle string) (int64, bool, error) {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO media_item
			(library_id, kind, path, title, sort_title, added_at, updated_at, missing)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(path) DO NOTHING`,
		libraryID, kind, path, title, sortTitle, now, now)
	if err != nil {
		return 0, false, fmt.Errorf("ensure %s %q: %w", kind, path, err)
	}
	created := false
	if n, err := res.RowsAffected(); err == nil {
		created = n == 1
	}
	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM media_item WHERE path = ?`, path).Scan(&id); err != nil {
		return 0, false, fmt.Errorf("ensure %s %q: read id: %w", kind, path, err)
	}
	return id, created, nil
}

// PromoteToChild turns a movie file into an ordered child of a container — a
// 'part' of a multi-part work, a 'chapter' of a serial — under parentID, with
// its order in the episode column (the ordering column ADR 0017 reuses). The
// file's own title is left as scanned ("Baahubali Part 1"), which already reads
// well and preserves any manual rename. Idempotent across rescans.
func (s *Store) PromoteToChild(ctx context.Context, itemID, parentID int64, kind string, order int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE media_item
		SET kind = ?, parent_id = ?, episode = ?, updated_at = ?
		WHERE id = ?`,
		kind, parentID, order, time.Now().Unix(), itemID)
	if err != nil {
		return fmt.Errorf("promote item %d to %s: %w", itemID, kind, err)
	}
	return nil
}

// RemovalTarget is one row a delete or ignore touches: its id, its file path
// (empty for a container row that is not a file), and whether it is a file.
type RemovalTarget struct {
	ID     int64
	Path   string
	IsFile bool
}

// ItemSubtree returns an item and every descendant under it via parent_id — a
// show with its seasons and episodes, a work with its parts. It is what a
// container-level delete or ignore operates on. Collection membership is not
// containment, so a collection's members are not included: removing a collection
// removes the grouping, never the films.
func (s *Store) ItemSubtree(ctx context.Context, id int64) ([]RemovalTarget, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH RECURSIVE tree(id) AS (
			SELECT ?
			UNION ALL
			SELECT m.id FROM media_item m JOIN tree t ON m.parent_id = t.id
		)
		SELECT m.id, m.path, m.container IS NOT NULL
		FROM media_item m JOIN tree ON m.id = tree.id`, id)
	if err != nil {
		return nil, fmt.Errorf("item subtree: %w", err)
	}
	defer rows.Close()
	var out []RemovalTarget
	for rows.Next() {
		var t RemovalTarget
		if err := rows.Scan(&t.ID, &t.Path, &t.IsFile); err != nil {
			return nil, fmt.Errorf("item subtree: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteItems removes rows by id. Everything hanging off them — locks, artwork
// links, credits, playback state, subtitles, streams, collection membership —
// goes with them via ON DELETE CASCADE. This never touches disk.
func (s *Store) DeleteItems(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i] = "?"
		args[i] = id
	}
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM media_item WHERE id IN (`+strings.Join(ph, ",")+`)`, args...); err != nil {
		return fmt.Errorf("delete items: %w", err)
	}
	return nil
}

// IgnorePaths records paths the scanner must skip, so a removed-but-not-deleted
// title is never re-added on the next scan.
func (s *Store) IgnorePaths(ctx context.Context, libraryID int64, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ignore paths: %w", err)
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO ignored_path (library_id, path, added_at) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("ignore paths: %w", err)
	}
	defer stmt.Close()
	now := time.Now().Unix()
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, libraryID, p, now); err != nil {
			return fmt.Errorf("ignore paths: %w", err)
		}
	}
	return tx.Commit()
}

// IgnoredPaths returns the ignore list for a library, for the scanner to skip.
func (s *Store) IgnoredPaths(ctx context.Context, libraryID int64) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT path FROM ignored_path WHERE library_id = ?`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("ignored paths: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("ignored paths: %w", err)
		}
		out[p] = true
	}
	return out, rows.Err()
}

// PruneEmptyContainers deletes container rows that no longer hold anything — a
// show with no episodes, a work whose parts became episodes, a collection with
// no members. Containers have a null container column (they are not files), so
// this can never touch a real movie or episode. It runs after reconciliation,
// once children have been re-parented, so a container is pruned only when it is
// genuinely orphaned rather than mid-rebuild.
//
// Playlists are exempt, for two reasons that both had to be true (ADR 0030).
// Their members live in playlist_entry, not in parent_id or item_collection, so
// every one of them looked empty here and was deleted at the end of the same
// scan that imported it. And an empty playlist is a legitimate object anyway —
// the importer creates one deliberately when none of its lines resolve, so that
// a person can see it exists and find out why it is empty, rather than watching
// their .m3u vanish in silence. "Empty" is not "orphaned" for this kind.
func (s *Store) PruneEmptyContainers(ctx context.Context, libraryID int64) (int, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM media_item
		WHERE library_id = ? AND container IS NULL
		  AND kind != 'playlist'
		  AND NOT EXISTS (SELECT 1 FROM media_item c WHERE c.parent_id = media_item.id)
		  AND NOT EXISTS (SELECT 1 FROM item_collection ic WHERE ic.collection_id = media_item.id)`,
		libraryID)
	if err != nil {
		return 0, fmt.Errorf("prune empty containers: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// AttachChildCounts fills ChildCount for each item, so a caller can tell a
// container from a leaf without a query per row. It counts both kinds of
// containment: parent_id children (a show's seasons, a work's parts) and
// collection membership (a collection's films), which live in a join table
// rather than parent_id. Missing children are not counted — a container whose
// files are all offline reads as empty, not as a phantom.
func (s *Store) AttachChildCounts(ctx context.Context, items []Item) error {
	if len(items) == 0 {
		return nil
	}
	byID := make(map[int64]*Item, len(items))
	ph := make([]string, len(items))
	args := make([]any, len(items))
	for i := range items {
		byID[items[i].ID] = &items[i]
		ph[i] = "?"
		args[i] = items[i].ID
	}
	in := strings.Join(ph, ",")

	// parent_id children.
	rows, err := s.db.QueryContext(ctx, `
		SELECT parent_id, COUNT(*) FROM media_item
		WHERE parent_id IN (`+in+`) AND missing = 0
		GROUP BY parent_id`, args...)
	if err != nil {
		return fmt.Errorf("attach child counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var parent int64
		var n int
		if err := rows.Scan(&parent, &n); err != nil {
			return fmt.Errorf("attach child counts: %w", err)
		}
		if it := byID[parent]; it != nil {
			it.ChildCount = n
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Collection members (join table, not parent_id).
	crows, err := s.db.QueryContext(ctx, `
		SELECT ic.collection_id, COUNT(*)
		FROM item_collection ic
		JOIN media_item m ON m.id = ic.item_id
		WHERE ic.collection_id IN (`+in+`) AND m.missing = 0
		GROUP BY ic.collection_id`, args...)
	if err != nil {
		return fmt.Errorf("attach collection counts: %w", err)
	}
	defer crows.Close()
	for crows.Next() {
		var coll int64
		var n int
		if err := crows.Scan(&coll, &n); err != nil {
			return fmt.Errorf("attach collection counts: %w", err)
		}
		if it := byID[coll]; it != nil {
			it.ChildCount = n
		}
	}
	if err := crows.Err(); err != nil {
		return err
	}

	// Playlist entries (ADR 0030), the third kind of containment and the only
	// one that counts repeats. A playlist held zero here until the playlists
	// page needed to say how long a list was, and "0 tracks" under a playlist
	// with eleven in it is worse than saying nothing — it is a grid asserting
	// something false about every tile.
	//
	// COUNT(*) over the entries, not over distinct items: a set that opens and
	// closes with the same song is twelve entries, and the detail page beneath
	// this tile will show twelve rows.
	//
	// Missing files are counted, unlike the two queries above. A playlist entry
	// whose file is temporarily gone — an unmounted drive — is still an entry;
	// scanning marks missing rather than deleting for exactly this reason, and a
	// playlist that silently shortens itself when a drive is unplugged is the
	// failure that rule exists to prevent.
	prows, err := s.db.QueryContext(ctx, `
		SELECT playlist_id, COUNT(*)
		FROM playlist_entry
		WHERE playlist_id IN (`+in+`)
		GROUP BY playlist_id`, args...)
	if err != nil {
		return fmt.Errorf("attach playlist counts: %w", err)
	}
	defer prows.Close()
	for prows.Next() {
		var pl int64
		var n int
		if err := prows.Scan(&pl, &n); err != nil {
			return fmt.Errorf("attach playlist counts: %w", err)
		}
		if it := byID[pl]; it != nil {
			it.ChildCount = n
		}
	}
	return prows.Err()
}

// AddToCollection links an item into a collection (ADR 0017). Membership is
// many-to-many and deliberately not parent_id: a member — an "Anne" film, a
// franchise entry — stays a top-level, independently browsable item that may
// belong to several collections. ord positions it within the collection. Re-
// adding updates the order rather than erroring, so ingestion is idempotent.
func (s *Store) AddToCollection(ctx context.Context, itemID, collectionID int64, ord int) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO item_collection (item_id, collection_id, ord)
		VALUES (?, ?, ?)
		ON CONFLICT(item_id, collection_id) DO UPDATE SET ord = excluded.ord`,
		itemID, collectionID, ord)
	if err != nil {
		return fmt.Errorf("add item %d to collection %d: %w", itemID, collectionID, err)
	}
	return nil
}

// RemoveFromCollection unlinks an item without deleting either row.
func (s *Store) RemoveFromCollection(ctx context.Context, itemID, collectionID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM item_collection WHERE item_id = ? AND collection_id = ?`,
		itemID, collectionID)
	if err != nil {
		return fmt.Errorf("remove item %d from collection %d: %w", itemID, collectionID, err)
	}
	return nil
}

// CollectionMembers returns a collection's members in their curated order.
func (s *Store) CollectionMembers(ctx context.Context, collectionID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColsMI+`
		FROM media_item mi
		JOIN item_collection ic ON ic.item_id = mi.id
		WHERE ic.collection_id = ?
		ORDER BY ic.ord, mi.sort_title`, collectionID)
	if err != nil {
		return nil, fmt.Errorf("collection members: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// CollectionsOf returns the collections an item belongs to.
func (s *Store) CollectionsOf(ctx context.Context, itemID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColsMI+`
		FROM media_item mi
		JOIN item_collection ic ON ic.collection_id = mi.id
		WHERE ic.item_id = ?
		ORDER BY mi.sort_title`, itemID)
	if err != nil {
		return nil, fmt.Errorf("collections of item: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// ContinueWatching returns a user's in-progress items, most recently played
// first: everything they have started but not finished. "Started" means a saved
// position past zero; "not finished" means the watched flag is unset, so an item
// played to the end drops off the shelf rather than inviting a replay.
// sinceUnix drops anything untouched before that time, or 0 for no cutoff — the
// configured Continue Watching window (config.Settings.ContinueWeeks). Filtered
// in SQL rather than after the fact so the limit applies to what survives: a
// shelf of 40 that is 39 abandoned documentaries and one real row is the bug
// this window exists to prevent, and trimming afterwards reproduces it exactly.
func (s *Store) ContinueWatching(ctx context.Context, userID string, limit int, sinceUnix int64) ([]Item, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColsMI+`
		FROM media_item mi
		JOIN playback_state ps ON ps.item_id = mi.id AND ps.user_id = ?
		WHERE ps.position_ms > 0 AND ps.watched = 0 AND mi.missing = 0
		  AND (? = 0 OR ps.updated_at >= ?)
		ORDER BY ps.updated_at DESC
		LIMIT ?`, userID, sinceUnix, sinceUnix, limit)
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

// LibraryTracks returns a music library's track rows.
func (s *Store) LibraryTracks(ctx context.Context, libraryID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM media_item WHERE library_id = ? AND kind = 'track' AND missing = 0`,
		libraryID)
	if err != nil {
		return nil, fmt.Errorf("library tracks: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// TrackTags is what an embedded tag set contributes to a track row.
//
// SortTitle is supplied by the caller rather than derived here: title
// normalization has exactly one implementation, `media.SortTitle`, and a second
// one inside the store would be the bug factory CLAUDE.md warns about.
type TrackTags struct {
	Title     string
	SortTitle string
	Artist    string
	Album     string
	Disc      int
	Track     int
	Year      int
}

// ApplyTrackTags writes tag-derived metadata onto a track.
//
// Locked fields are left alone, which is the same promise every other write
// path makes (ADR 0008): editing a field pins it, and re-reading the file must
// not undo that. A rescan reconciles files; it does not re-litigate identity.
//
// Empty values do not overwrite. A tagger that filled in a title but left the
// album blank should not erase an album that a folder name supplied.
func (s *Store) ApplyTrackTags(ctx context.Context, itemID int64, t TrackTags) error {
	lockedList, err := s.LockedFields(ctx, itemID)
	if err != nil {
		return err
	}
	locked := make(map[string]bool, len(lockedList))
	for _, f := range lockedList {
		locked[f] = true
	}

	set := []string{}
	args := []any{}
	add := func(field string, value any, skip bool) {
		if skip || locked[field] {
			return
		}
		set = append(set, field+" = ?")
		args = append(args, value)
	}

	add("title", t.Title, t.Title == "")
	// sort_title follows title's lock, not one of its own — they are one field
	// as far as an operator is concerned.
	add("sort_title", t.SortTitle, t.Title == "" || locked["title"])
	add("artist", t.Artist, t.Artist == "")
	add("series", t.Album, t.Album == "")
	add("season", t.Disc, t.Disc == 0)
	add("episode", t.Track, t.Track == 0)
	add("year", t.Year, t.Year == 0)

	if len(set) == 0 {
		return nil
	}
	set = append(set, "updated_at = ?")
	args = append(args, time.Now().Unix(), itemID)

	_, err = s.db.ExecContext(ctx,
		`UPDATE media_item SET `+strings.Join(set, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return fmt.Errorf("apply track tags: %w", err)
	}
	return nil
}

// EnsureDerivedContainer find-or-creates a container assembled from its own
// children rather than from a provider — an artist, an album, or a gallery.
//
// Music containers have no file, so their identity is a synthetic path — the
// same device seasons already use when a show has no "Season N" folder. The
// path is what makes this idempotent across rescans, and it is scoped by the
// parent so two artists can both have a "Greatest Hits" without colliding.
//
// Marked matched with a full score for the same reason shows and seasons are:
// a container assembled from its children's tags is not waiting to be
// identified, and leaving it unmatched would park it in the review queue
// forever.
// The kinds this may create. A closed set, because the guard's whole job is to
// turn a typo'd or caller-invented kind into an error here rather than a row
// nothing queries. Pictures joined it in ADR 0028: a gallery is assembled from
// the folder its photos sit in, exactly as an album is assembled from its
// tracks' tags.
// A playlist is here for the same reason the others are: it is a container row
// the scanner invents, keyed by a path, with no file of its own. For one
// imported from an .m3u that path is the .m3u itself, which is what makes a
// re-import update the playlist rather than accumulate a second one.
var derivedContainerKinds = map[string]bool{
	"artist": true, "album": true, "gallery": true, "playlist": true,
}

func (s *Store) EnsureDerivedContainer(ctx context.Context, libraryID int64, kind, path, title, sortTitle string, parentID *int64) (int64, error) {
	if !derivedContainerKinds[kind] {
		return 0, fmt.Errorf("ensure derived container: unexpected kind %q", kind)
	}
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO media_item
			(library_id, kind, path, title, sort_title, parent_id,
			 match_state, match_score, metadata_updated_at, added_at, updated_at, missing)
		VALUES (?, ?, ?, ?, ?, ?, 'matched', 1, ?, ?, ?, 0)
		ON CONFLICT(path) DO NOTHING`,
		libraryID, kind, path, title, sortTitle, parentID, now, now, now)
	if err != nil {
		return 0, fmt.Errorf("ensure %s %q: %w", kind, path, err)
	}

	var id int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT id FROM media_item WHERE path = ?`, path).Scan(&id); err != nil {
		return 0, fmt.Errorf("ensure %s %q: read id: %w", kind, path, err)
	}
	return id, nil
}

// FillAlbumMetadata gives album rows the two facts their tracks already carry:
// who made the record, and when.
//
// EnsureDerivedContainer creates an album with a title and nothing else, because
// at creation time it knows only the grouping key. The result is an album detail
// page showing a cover and a title over an empty space, a Year sort with no year
// to sort by, and a track list that cannot tell whether a performer differs from
// the album artist — because it has no album artist to compare against. Every
// one of those reads as missing metadata; the metadata was there the whole time,
// one row down.
//
// Derived rather than stored at creation, and re-derived on every scan, so an
// album that gains a correctly tagged track picks it up without a special case.
//
//   - artist is the album artist, which is the parent artist row's title. That
//     row was created *from* the album-artist tag (ADR 0024), so this is not a
//     guess — it is the same value, denormalized one level down.
//   - year is the earliest year among the tracks. A record has one year; its
//     files sometimes disagree by a year when a single was tagged with its own
//     release date, and the earliest is the closer answer for a sort.
//
// Locked fields are never overwritten (CLAUDE.md), so an operator who fixed
// either by hand keeps their answer.
func (s *Store) FillAlbumMetadata(ctx context.Context, libraryID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE media_item AS a
		SET artist = COALESCE(
		        (SELECT p.title FROM media_item p WHERE p.id = a.parent_id),
		        a.artist),
		    year = COALESCE(
		        (SELECT MIN(t.year) FROM media_item t
		          WHERE t.parent_id = a.id AND t.year IS NOT NULL AND t.year > 0),
		        a.year)
		WHERE a.library_id = ? AND a.kind = 'album'
		  AND NOT EXISTS (
		        SELECT 1 FROM item_lock l
		         WHERE l.item_id = a.id AND l.field IN ('artist', 'year'))`,
		libraryID)
	if err != nil {
		return 0, fmt.Errorf("fill album metadata: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// DeleteEmptyMusicContainers removes artist and album rows left with no
// children — an album whose files moved, an artist whose last album went.
//
// Scanning marks missing and never deletes *files*; these are not files. They
// are grouping rows LANcast invented, and an invented row with nothing under it
// is an empty shelf in the browse grid.
func (s *Store) DeleteEmptyMusicContainers(ctx context.Context, libraryID int64) (int64, error) {
	var total int64
	// Albums first, then artists: emptying an album can empty its artist, and
	// doing it in the other order would leave that artist behind for a scan.
	for _, kind := range []string{"album", "artist"} {
		res, err := s.db.ExecContext(ctx, `
			DELETE FROM media_item
			WHERE library_id = ? AND kind = ?
			  AND NOT EXISTS (SELECT 1 FROM media_item c WHERE c.parent_id = media_item.id)`,
			libraryID, kind)
		if err != nil {
			return total, fmt.Errorf("delete empty %ss: %w", kind, err)
		}
		n, _ := res.RowsAffected()
		total += n
	}
	return total, nil
}

// RenameLibrary changes a library's display name. Nothing else moves: the name
// is a label, and no row anywhere refers to it.
func (s *Store) RenameLibrary(ctx context.Context, id int64, name string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE library SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return fmt.Errorf("rename library: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RepointRoot moves one location to a new path, resolving it by id.
//
// The shape this wants to be from ADR 0034: repointing is per *location*, and a
// library with two of them has one move while the other stays where it is.
// RepointLibrary below is the older signature, kept until the handlers learn
// about roots, and it delegates here.
func (s *Store) RepointRoot(ctx context.Context, rootID int64, newRoot string) error {
	root, err := s.GetRoot(ctx, rootID)
	if err != nil {
		return err
	}
	// Overlap is checked for the same reason AddRoot checks it: a repoint can
	// land a root inside another one just as easily as adding it there could,
	// and the resulting ambiguity is identical.
	all, err := s.AllRoots(ctx)
	if err != nil {
		return err
	}
	for _, r := range all {
		if r.ID == rootID {
			continue
		}
		if rootsOverlap(r.Path, newRoot) {
			return fmt.Errorf("%w: %s", ErrRootOverlaps, r.Path)
		}
	}
	return s.repoint(ctx, root.LibraryID, root.Path, newRoot)
}

// RepointLibrary moves a library to a new root, carrying its contents with it.
//
// This is the drive-letter case, and it is the reason editing a path is worth
// having at all: the media moved from D: to E:, or a folder was renamed, and
// everything LANcast knows about it — matches, artwork, watch state, playlist
// membership — is keyed on rows whose `path` still names the old place. A
// library that could only be deleted and re-added would throw all of that away
// to record a fact about a drive letter.
//
// So every path under the old root is rewritten to the new one, in a single
// transaction, and *nothing else changes*: no row is deleted, no item is marked
// missing, no scan is triggered. A rescan afterwards reconciles files the same
// way it always does — this only tells it where to look.
//
// Deliberately not clever about case or separators. It is an exact prefix swap
// on the stored strings, because the stored strings are what every other query
// joins on; a normalizing rewrite would "fix" paths into a form the filesystem
// layer never produced and quietly orphan them.
//
// The ignore list moves too. Those are absolute paths to files somebody chose
// not to see, and a library that moved must not resurrect them.
func (s *Store) RepointLibrary(ctx context.Context, id int64, oldRoot, newRoot string) error {
	return s.repoint(ctx, id, oldRoot, newRoot)
}

// repoint is the prefix swap both entry points share. Splitting it keeps the
// reasoning above attached to one implementation rather than two that could
// drift — the failure mode this whole file is organised against.
func (s *Store) repoint(ctx context.Context, id int64, oldRoot, newRoot string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("repoint library: %w", err)
	}
	defer tx.Rollback()

	// A trailing separator on either side would double it in every rewritten
	// path, so both are normalized to "no trailing separator" and the separator
	// comes from the stored path itself.
	oldRoot = strings.TrimRight(oldRoot, `/\`)
	newRoot = strings.TrimRight(newRoot, `/\`)
	prefix := oldRoot + "%"

	if _, err := tx.ExecContext(ctx, `
		UPDATE media_item
		SET path = ? || SUBSTR(path, ?)
		WHERE library_id = ? AND path LIKE ?`,
		newRoot, len(oldRoot)+1, id, prefix); err != nil {
		return fmt.Errorf("repoint library: items: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE ignored_path
		SET path = ? || SUBSTR(path, ?)
		WHERE path LIKE ?`,
		newRoot, len(oldRoot)+1, prefix); err != nil {
		return fmt.Errorf("repoint library: ignored paths: %w", err)
	}
	// The root row, not the library row — the path lives in `library_root` from
	// revision 18. Matched on the old path rather than on "the first root",
	// because repointing is per *location*: a library with two roots has one of
	// them move, and picking by ordinal would rewrite whichever root happened to
	// be created first regardless of which one the caller meant.
	res, err := tx.ExecContext(ctx,
		`UPDATE library_root SET path = ? WHERE library_id = ? AND path = ?`,
		newRoot, id, oldRoot)
	if err != nil {
		return fmt.Errorf("repoint library: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repoint library: commit: %w", err)
	}
	return nil
}
