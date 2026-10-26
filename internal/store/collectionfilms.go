package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

/*
 * A collection's whole film list, owned or not (ADR 0093).
 *
 * The provider's statement of what a franchise holds, kept beside the
 * membership table rather than in it: item_collection links items that exist,
 * and most of the point of this list is the films that do not.
 */

// CollectionFilm is one row of a collection's film list, as stored.
type CollectionFilm struct {
	ExternalID string
	Title      string
	Year       int
	ReleasedAt int64
	Overview   string
	PosterHash string
}

/*
 * ReplaceCollectionFilms writes a collection's film list, whole.
 *
 * Replaced rather than merged, because the provider's list is the truth about
 * the franchise: a film TMDB removed from it should leave, and a newly
 * announced sequel should arrive. A poster already downloaded for a film is
 * kept across the replacement when the new row brings none, so a refresh never
 * costs a second download of the same image.
 */
func (s *Store) ReplaceCollectionFilms(ctx context.Context, collectionID int64, provider string, films []CollectionFilm) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("replace collection films: %w", err)
	}
	defer tx.Rollback()

	kept := map[string]string{}
	rows, err := tx.QueryContext(ctx, `
		SELECT external_id, poster_hash FROM collection_film
		 WHERE collection_id = ? AND provider = ? AND poster_hash IS NOT NULL`, collectionID, provider)
	if err != nil {
		return fmt.Errorf("replace collection films: read posters: %w", err)
	}
	for rows.Next() {
		var ext, hash string
		if err := rows.Scan(&ext, &hash); err != nil {
			rows.Close()
			return fmt.Errorf("replace collection films: read posters: %w", err)
		}
		kept[ext] = hash
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM collection_film WHERE collection_id = ? AND provider = ?`, collectionID, provider); err != nil {
		return fmt.Errorf("replace collection films: clear: %w", err)
	}
	for i, f := range films {
		hash := f.PosterHash
		if hash == "" {
			hash = kept[f.ExternalID]
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO collection_film
				(collection_id, provider, external_id, ord, title, year, released_at, overview, poster_hash)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			collectionID, provider, f.ExternalID, i, f.Title,
			zeroNull(int64(f.Year)), zeroNull(f.ReleasedAt), emptyNull(f.Overview), emptyNull(hash)); err != nil {
			return fmt.Errorf("replace collection films: insert %q: %w", f.ExternalID, err)
		}
	}
	return tx.Commit()
}

// SetCollectionFilmPoster records the poster downloaded for one listed film.
func (s *Store) SetCollectionFilmPoster(ctx context.Context, collectionID int64, provider, externalID, hash string) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE collection_film SET poster_hash = ?
		 WHERE collection_id = ? AND provider = ? AND external_id = ?`,
		hash, collectionID, provider, externalID); err != nil {
		return fmt.Errorf("set collection film poster: %w", err)
	}
	return nil
}

/*
 * CollectionFilmsNeedingPosters lists the listed films that are not in the
 * collection's library and have no poster yet: the only ones whose picture
 * nothing else supplies. An owned film is drawn with its own poster.
 */
func (s *Store) CollectionFilmsNeedingPosters(ctx context.Context, collectionID int64, provider string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT cf.external_id FROM collection_film cf
		  JOIN media_item c ON c.id = cf.collection_id
		 WHERE cf.collection_id = ? AND cf.provider = ? AND cf.poster_hash IS NULL
		   AND NOT EXISTS (
			SELECT 1 FROM media_item m
			 WHERE m.library_id = c.library_id AND m.kind = 'movie' AND m.missing = 0
			   AND m.provider = cf.provider AND m.external_id = cf.external_id)
		 ORDER BY cf.ord`, collectionID, provider)
	if err != nil {
		return nil, fmt.Errorf("collection films needing posters: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ext string
		if err := rows.Scan(&ext); err != nil {
			return nil, fmt.Errorf("collection films needing posters: %w", err)
		}
		out = append(out, ext)
	}
	return out, rows.Err()
}

// CollectionNeedingFilms is a provider collection with no film list yet.
type CollectionNeedingFilms struct {
	ID         int64
	Provider   string
	ExternalID string
}

/*
 * CollectionsNeedingFilms lists provider collections that have no film list:
 * the ones created before lists were stored. Keyword groupings are excluded —
 * a keyword is a tag with no list behind it — and so is anything that is not
 * a provider's collection at all.
 */
func (s *Store) CollectionsNeedingFilms(ctx context.Context, limit int) ([]CollectionNeedingFilms, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, provider, external_id FROM media_item c
		 WHERE kind = 'collection' AND provider IS NOT NULL AND external_id IS NOT NULL
		   AND external_id NOT LIKE 'keyword:%'
		   AND NOT EXISTS (SELECT 1 FROM collection_film cf WHERE cf.collection_id = c.id)
		 ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("collections needing films: %w", err)
	}
	defer rows.Close()
	var out []CollectionNeedingFilms
	for rows.Next() {
		var c CollectionNeedingFilms
		if err := rows.Scan(&c.ID, &c.Provider, &c.ExternalID); err != nil {
			return nil, fmt.Errorf("collections needing films: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListedFilm is one film of a collection's list as an account sees it.
type ListedFilm struct {
	ExternalID string `json:"external_id"`
	Title      string `json:"title"`
	Year       int    `json:"year,omitempty"`
	Overview   string `json:"overview,omitempty"`
	// Poster is an artwork hash: the owned film's own poster, else the one
	// downloaded for the listing. Empty when there is neither.
	Poster string `json:"poster,omitempty"`
	// ItemID is the film in this library, when it is there. Absent means
	// missing from the library.
	ItemID int64 `json:"item_id,omitempty"`
	// Upcoming is a film not released yet: undated, or dated in the future.
	Upcoming bool `json:"upcoming,omitempty"`
}

/*
 * CollectionFilmsFor is a collection's film list for one account, in release
 * order, each film marked with the library's copy where there is one.
 *
 * The account's limits apply, and they decide the shape:
 *
 *   - an owned film the account may not open is left out entirely, as every
 *     listing leaves it out;
 *   - an account with a rating ceiling is shown no missing films at all. A
 *     missing film has no certificate here to test against the ceiling, and
 *     naming one would be naming a title the household may be keeping from
 *     them — the restricted sequel is exactly the film most likely to be
 *     missing from a child's view.
 *
 * Restricted is the ceiling only: a hidden library is decided by the
 * collection itself, which the caller has already opened.
 */
func (s *Store) CollectionFilmsFor(ctx context.Context, collectionID int64, userID string) ([]ListedFilm, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT cf.external_id, cf.title, cf.year, cf.released_at, cf.overview, cf.poster_hash,
		       (SELECT m.id FROM media_item m
		         WHERE m.library_id = c.library_id AND m.kind = 'movie' AND m.missing = 0
		           AND m.provider = cf.provider AND m.external_id = cf.external_id
		         ORDER BY m.id LIMIT 1)
		  FROM collection_film cf
		  JOIN media_item c ON c.id = cf.collection_id
		 WHERE cf.collection_id = ?
		 ORDER BY cf.ord`, collectionID)
	if err != nil {
		return nil, fmt.Errorf("collection films: %w", err)
	}
	defer rows.Close()

	now := time.Now().Unix()
	var out []ListedFilm
	var owned []Item
	for rows.Next() {
		var f ListedFilm
		var year, released, item sql.NullInt64
		var overview, poster sql.NullString
		if err := rows.Scan(&f.ExternalID, &f.Title, &year, &released, &overview, &poster, &item); err != nil {
			return nil, fmt.Errorf("collection films: %w", err)
		}
		f.Year = int(year.Int64)
		f.Overview = overview.String
		f.Poster = poster.String
		f.ItemID = item.Int64
		f.Upcoming = !released.Valid || released.Int64 == 0 || released.Int64 > now
		if f.ItemID != 0 {
			owned = append(owned, Item{ID: f.ItemID})
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	access, err := s.accessFor(ctx, userID)
	if err != nil {
		return nil, err
	}
	permitted, err := s.PermittedItems(ctx, userID, owned)
	if err != nil {
		return nil, err
	}
	allowed := make(map[int64]bool, len(permitted))
	for _, it := range permitted {
		allowed[it.ID] = true
	}
	// The owned films' own posters, which win over a listing's.
	if err := s.AttachArtwork(ctx, permitted); err != nil {
		return nil, err
	}
	posters := make(map[int64]string, len(permitted))
	for _, it := range permitted {
		if it.Artwork != nil && it.Artwork.Poster != "" {
			posters[it.ID] = it.Artwork.Poster
		}
	}

	kept := out[:0]
	for _, f := range out {
		if f.ItemID != 0 {
			if !allowed[f.ItemID] {
				continue
			}
			if p := posters[f.ItemID]; p != "" {
				f.Poster = p
			}
		} else if access.ceiling != "" {
			continue
		}
		kept = append(kept, f)
	}
	return kept, nil
}

// zeroNull and emptyNull store "unknown" as NULL rather than as 0 or "".
func zeroNull(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func emptyNull(v string) any {
	if v == "" {
		return nil
	}
	return v
}
