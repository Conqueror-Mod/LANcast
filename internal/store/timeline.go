package store

import (
	"context"
	"fmt"
)

/*
 * The photo timeline — a picture library grouped by when the pictures were
 * taken rather than by which folder they are in.
 *
 * A folder grid answers "where did I put it". A timeline answers "when was
 * that", which is the question people actually arrive with for photographs and
 * the one a folder grid cannot answer at all: a holiday spread across three
 * folders is one week, and three folders is how it looks today.
 *
 * Built on `taken_at`, which is EXIF capture time and is **not** `added_at` —
 * one is when the picture was made, the other when it reached this disk (ADR
 * 0028). That distinction is the whole feature. Measured on the reporting
 * library before any of this was designed: 3,469 of 3,676 photographs carry a
 * capture time, 94.4%, spanning 2006 to 2026. The remaining 207 are a real
 * bucket rather than an error, and they sort last.
 */

// TimelineBucket is one month of a picture library, newest first.
type TimelineBucket struct {
	// Year and Month are the capture month in **local** time, which is what a
	// person means by "August". Undated is the bucket for photographs with no
	// capture time at all; its Year and Month are zero.
	Year    int  `json:"year"`
	Month   int  `json:"month"`
	Undated bool `json:"undated,omitempty"`
	Count   int  `json:"count"`
}

/*
 * PhotoTimeline counts a picture library's photographs by capture month.
 *
 * Counts rather than the photographs themselves: 3,676 items is a page nobody
 * wants and a payload nobody needs, and the client fetches a month at a time
 * once it knows which months exist. A library's whole shape arrives in one
 * small response.
 *
 * **Sensitive folders are excluded**, and that is a decision rather than an
 * oversight (ADR 0051, amended). A cover may only be lifted inside the library
 * grid or the folder itself, so timeline entries could never be uncovered here
 * — and a row of covered tiles scattered through a holiday still discloses
 * *when* the marked photographs were taken, which is most of what somebody
 * marking a folder is trying not to say. They remain reachable exactly where
 * the amendment puts them: in their folder.
 */
func (s *Store) PhotoTimeline(ctx context.Context, libraryID int64) ([]TimelineBucket, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			CASE WHEN taken_at IS NULL THEN NULL
			     ELSE CAST(strftime('%Y', taken_at, 'unixepoch', 'localtime') AS INTEGER) END AS y,
			CASE WHEN taken_at IS NULL THEN NULL
			     ELSE CAST(strftime('%m', taken_at, 'unixepoch', 'localtime') AS INTEGER) END AS m,
			COUNT(*)
		  FROM media_item
		 WHERE library_id = ? AND kind = 'photo' AND missing = 0
		   AND sensitive_effective = 0
		 GROUP BY y, m
		 -- Undated last: NULL sorts first descending, so it is pushed out
		 -- explicitly rather than left to the collation.
		 ORDER BY (y IS NULL), y DESC, m DESC`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("photo timeline: %w", err)
	}
	defer rows.Close()

	out := []TimelineBucket{}
	for rows.Next() {
		var y, m *int
		var n int
		if err := rows.Scan(&y, &m, &n); err != nil {
			return nil, fmt.Errorf("photo timeline: %w", err)
		}
		b := TimelineBucket{Count: n}
		if y == nil || m == nil {
			b.Undated = true
		} else {
			b.Year, b.Month = *y, *m
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
