package store

import (
	"context"
	"fmt"
	"time"
)

/*
 * Photo places — a picture library grouped by the town each photograph was
 * taken in (ADR 0078).
 *
 * The Timeline's sibling: a folder grid answers "where did I put it", a
 * timeline "when was that", and this "where were we". Built on photo_location,
 * which the location pass fills only while the `photo_places` setting is on.
 *
 * What leaves this file is a town's name and a count. No function here returns
 * a coordinate to a caller that serves HTTP: nothing in the client needs one
 * without a map, and a field that is never selected cannot be leaked by a
 * handler that forgot to drop it.
 */

// PhotoPlace is one town photographs were filed under.
type PhotoPlace struct {
	// ID is the GeoNames id, stable across their exports.
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Region      string `json:"region,omitempty"`
	CountryCode string `json:"country_code"`
	Country     string `json:"country,omitempty"`
	Count       int    `json:"count"`
}

// PlaceSummary is a picture library's places and the photographs that are in
// none of them.
type PlaceSummary struct {
	Places []PhotoPlace `json:"places"`
	// Elsewhere counts photographs with a position and no town within reach of
	// it — at sea, on a mountain. They are a bucket of their own rather than
	// dropped, because a photo that vanished from a view it belongs in reads
	// as a bug.
	Elsewhere int `json:"elsewhere"`
	// Unlocated counts photographs that were read and carry no position:
	// most of any library, because only cameras with GPS write one.
	Unlocated int `json:"unlocated"`
	// Unread counts photographs the location pass has not reached yet.
	Unread int `json:"unread"`
}

// LatLon is a position in decimal degrees.
type LatLon struct {
	Lat, Lon float64
}

/*
 * PhotoPlaces counts a picture library's photographs by place, most
 * photographed first.
 *
 * **Marked folders are excluded**, exactly as the Timeline excludes them (ADR
 * 0051, amended): a cover cannot be lifted on this view, and a covered tile
 * under a town still says where the marked photographs were taken. Their rows
 * are deleted when the mark is applied in any case (see
 * DeleteLocationsUnderSensitive); this is the second line, which depends on
 * nothing.
 */
func (s *Store) PhotoPlaces(ctx context.Context, libraryID int64) (PlaceSummary, error) {
	out := PlaceSummary{Places: []PhotoPlace{}}
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.region, p.country_code, p.country, COUNT(*) AS n
		  FROM photo_location l
		  JOIN media_item mi ON mi.id = l.item_id
		  JOIN photo_place p ON p.id = l.place_id
		 WHERE mi.library_id = ? AND mi.kind = 'photo' AND mi.missing = 0
		   AND mi.sensitive_effective = 0
		 GROUP BY p.id
		 ORDER BY n DESC, p.name`, libraryID)
	if err != nil {
		return out, fmt.Errorf("photo places: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p PhotoPlace
		if err := rows.Scan(&p.ID, &p.Name, &p.Region, &p.CountryCode, &p.Country, &p.Count); err != nil {
			return out, fmt.Errorf("photo places: %w", err)
		}
		out.Places = append(out.Places, p)
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("photo places: %w", err)
	}

	err = s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(l.item_id IS NOT NULL AND l.lat IS NOT NULL AND l.place_id IS NULL), 0),
			COALESCE(SUM(l.item_id IS NOT NULL AND l.lat IS NULL), 0),
			COALESCE(SUM(l.item_id IS NULL), 0)
		  FROM media_item mi
		  LEFT JOIN photo_location l ON l.item_id = mi.id
		 WHERE mi.library_id = ? AND mi.kind = 'photo' AND mi.missing = 0
		   AND mi.sensitive_effective = 0`, libraryID).
		Scan(&out.Elsewhere, &out.Unlocated, &out.Unread)
	if err != nil {
		return out, fmt.Errorf("photo places: counts: %w", err)
	}
	return out, nil
}

/*
 * PlacePhotos lists the photographs filed under one place, newest first. A
 * placeID of zero is the Elsewhere bucket: a position, and no town near it.
 *
 * The same exclusion as PhotoPlaces, so a place never opens onto more or fewer
 * photographs than its count said — "the town says 40 and shows 43" is the
 * bug the Timeline's listing was written to avoid.
 */
func (s *Store) PlacePhotos(ctx context.Context, libraryID, placeID int64, limit, offset int) ([]Item, int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	where := `library_id = ? AND kind = 'photo' AND missing = 0 AND sensitive_effective = 0
		AND id IN (SELECT item_id FROM photo_location WHERE `
	args := []any{libraryID}
	if placeID == 0 {
		where += `lat IS NOT NULL AND place_id IS NULL)`
	} else {
		where += `place_id = ?)`
		args = append(args, placeID)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_item WHERE `+where, args...).
		Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("place photos: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM media_item WHERE `+where+`
		ORDER BY COALESCE(taken_at, mtime, added_at) DESC, sort_title
		LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("place photos: %w", err)
	}
	defer rows.Close()
	items, err := scanItems(rows)
	if err != nil {
		return nil, 0, fmt.Errorf("place photos: %w", err)
	}
	return items, total, nil
}

/*
 * PendingLocations returns photographs the location pass has not read.
 *
 * Marked folders are absent, so the pass never opens one: a location that is
 * never read is one that cannot be kept by mistake.
 */
func (s *Store) PendingLocations(ctx context.Context, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM media_item
		WHERE kind = 'photo' AND missing = 0 AND sensitive_effective = 0
		  AND NOT EXISTS (SELECT 1 FROM photo_location l WHERE l.item_id = media_item.id)
		ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("pending locations: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// PendingLocationCount is the queue depth, for the activity panel.
func (s *Store) PendingLocationCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_item
		WHERE kind = 'photo' AND missing = 0 AND sensitive_effective = 0
		  AND NOT EXISTS (SELECT 1 FROM photo_location l WHERE l.item_id = media_item.id)`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("pending location count: %w", err)
	}
	return n, nil
}

/*
 * RecordPhotoLocation stamps one photograph as read: where it was taken, and
 * the town it was filed under. Either may be nil — a photo with no position
 * has neither, and one far from any town has a position and no place.
 *
 * The row is written in every case, because the row is the stamp. A photo
 * that says nothing about where it was taken will say nothing tomorrow, and
 * leaving it unstamped would hand the queue the same row for ever.
 */
func (s *Store) RecordPhotoLocation(ctx context.Context, itemID int64, at *LatLon, place *PhotoPlace) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("record photo location: %w", err)
	}
	defer tx.Rollback()

	var lat, lon, placeID any
	if at != nil {
		lat, lon = at.Lat, at.Lon
	}
	if place != nil {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO photo_place (id, name, region, country_code, country)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				name = excluded.name, region = excluded.region,
				country_code = excluded.country_code, country = excluded.country`,
			place.ID, place.Name, place.Region, place.CountryCode, place.Country); err != nil {
			return fmt.Errorf("record photo location: place: %w", err)
		}
		placeID = place.ID
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO photo_location (item_id, lat, lon, place_id, read_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(item_id) DO UPDATE SET
			lat = excluded.lat, lon = excluded.lon,
			place_id = excluded.place_id, read_at = excluded.read_at`,
		itemID, lat, lon, placeID, time.Now().Unix()); err != nil {
		return fmt.Errorf("record photo location: %w", err)
	}
	return tx.Commit()
}

/*
 * ForgetPhotoLocations deletes every location and every place, and answers how
 * many photographs had been read.
 *
 * What turning the setting off does. A deletion rather than a filter, for the
 * reason the face and embedding purges give: a hidden coordinate is still in
 * the database and in every backup taken afterwards, and "off" has to mean
 * what ADR 0028 meant by never reading it.
 */
func (s *Store) ForgetPhotoLocations(ctx context.Context) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("forget photo locations: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM photo_location`)
	if err != nil {
		return 0, fmt.Errorf("forget photo locations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM photo_place`); err != nil {
		return 0, fmt.Errorf("forget photo locations: places: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("forget photo locations: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

/*
 * DeleteLocationsUnderSensitive removes the locations of anything a mark now
 * covers.
 *
 * The third of the purges a mark runs, after faces and embeddings, and for
 * their reason: marking a folder that was already read must delete where its
 * photographs were taken, not merely stop showing it.
 */
func (s *Store) DeleteLocationsUnderSensitive(ctx context.Context, libraryID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM photo_location
		 WHERE item_id IN (
			SELECT id FROM media_item
			 WHERE library_id = ? AND sensitive_effective = 1)`, libraryID)
	if err != nil {
		return 0, fmt.Errorf("delete covered photo locations: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
