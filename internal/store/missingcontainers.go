package store

import (
	"context"
	"fmt"
	"time"
)

/*
 * A container with nothing left in it.
 *
 * Scanning marks files missing and never deletes them — an unmounted drive must
 * not destroy library data, and that rule is not negotiable. But it only ever
 * applied to *files*. A season, a show, an album, an artist, a gallery: none of
 * them have a file of their own, so nothing ever marked one missing, and a
 * container whose every episode had gone kept its tile for ever.
 *
 * Found on a real library. A show had been reorganised on disk, leaving three
 * folders' worth of history:
 *
 *	Season 1  (synthetic)   8 live episodes, 8 missing
 *	Season 1  (…S01 folder) 8 episodes, all missing, folder deleted
 *
 * Two "Season 1" tiles on the show page, one of which described a directory that
 * no longer existed. Reported as a duplicated season, which is what it looks
 * like — the duplicate is real, and it is the *empty* one that should not be
 * there.
 *
 * Keyed on structure rather than on a list of kinds. A container is a row with
 * children via `parent_id` and no file of its own, which in a real library is
 * exactly the album, artist, season, gallery and show rows and nothing else.
 * Collections and playlists group through their own tables and have no
 * `parent_id` children, so they are untouched by construction rather than by an
 * exclusion somebody has to remember to maintain.
 */

// ReconcileMissingContainers marks containers whose children have all gone, and
// restores any whose children have come back.
//
// Both directions, and the second is the point: this is the same reversibility
// the missing flag already has for files. Remount the drive, rescan, and a
// season that was hidden is a season again — nothing was deleted, so nothing has
// to be rebuilt.
//
// Iterated because the hierarchy nests: episodes going missing empties a season,
// which empties a show. Bottom-up in effect, without needing to know which level
// is which.
func (s *Store) ReconcileMissingContainers(ctx context.Context, libraryID int64) (marked, restored int64, err error) {
	now := time.Now().Unix()

	// Depth is three today (artist → album → track, show → season → episode) and
	// a multi-part work adds one. Five passes is room to spare; the loop exits as
	// soon as a pass changes nothing, so the cost of the ceiling is zero.
	const maxPasses = 5
	for pass := 0; pass < maxPasses; pass++ {
		/*
		 * Restore first.
		 *
		 * A pass that marked before restoring could mark a show whose season is
		 * about to be restored in the same sweep, and would need another
		 * iteration to undo it. Restoring first means each pass only ever
		 * tightens, which is what makes "no changes" a reliable stopping point.
		 */
		res, err := s.db.ExecContext(ctx, `
			UPDATE media_item SET missing = 0, updated_at = ?
			WHERE library_id = ? AND missing = 1 AND container IS NULL
			  AND EXISTS (SELECT 1 FROM media_item c
			              WHERE c.parent_id = media_item.id AND c.missing = 0)`,
			now, libraryID)
		if err != nil {
			return marked, restored, fmt.Errorf("restore containers: %w", err)
		}
		back, _ := res.RowsAffected()

		res, err = s.db.ExecContext(ctx, `
			UPDATE media_item SET missing = 1, updated_at = ?
			WHERE library_id = ? AND missing = 0 AND container IS NULL
			  AND EXISTS (SELECT 1 FROM media_item c WHERE c.parent_id = media_item.id)
			  AND NOT EXISTS (SELECT 1 FROM media_item c
			                  WHERE c.parent_id = media_item.id AND c.missing = 0)`,
			now, libraryID)
		if err != nil {
			return marked, restored, fmt.Errorf("mark empty containers: %w", err)
		}
		gone, _ := res.RowsAffected()

		marked += gone
		restored += back
		if gone == 0 && back == 0 {
			break
		}
	}
	return marked, restored, nil
}
