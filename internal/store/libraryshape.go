package store

import (
	"context"
	"encoding/json"
	"fmt"
)

/*
 * What shape did a scan actually produce?
 *
 * Library kind is chosen once and is immutable by design: it decides which
 * files are scanned at all and it biases movie-versus-TV matching, so changing
 * it later would mean a rescan re-litigating identity for a whole library —
 * the thing the locked-fields rule exists to forbid.
 *
 * The consequence is that choosing wrongly is unrecoverable except by removing
 * and re-adding the library, which makes being loud *at the moment it happens*
 * the only defence there is. Half of that was already built: a music library
 * created as a movie library reports how many audio files its own kind
 * discarded, because the audio-versus-video gate makes that case obvious —
 * zero items imported.
 *
 * The show-versus-movie case has no such signal, because both kinds scan
 * exactly the same files. Nothing is skipped, the count stays zero, the scan
 * succeeds, and the library is quietly wrong in its *shape* rather than its
 * size. Measured on the test library, one show stopped being a show: its
 * episodes were read as a film in three parts.
 *
 * A skip count cannot see that. A census of what was produced can.
 */

/*
 * ShapeWarning is a verdict about a finished scan: the library does not look
 * like the kind it was created as. Empty Code means no warning.
 *
 * It lives here rather than in the scanner because the library row stores the
 * last one (schema 20). The scanner produces it; the store keeps it; both refer
 * to one type, so the JSON a client sees cannot depend on which of them
 * answered.
 */
type ShapeWarning struct {
	// Code is stable and machine-readable; a client renders its own wording for
	// a code it knows and falls back to Message for one it does not.
	Code string `json:"code"`
	// Message is a whole sentence, written server-side. The audit log takes the
	// same position for the same reason: a client assembling prose from a code
	// will assemble it differently from the next client.
	Message string `json:"message"`
	// Remedy is separate because it is the part that is hard to hear — kind
	// cannot be changed, so the only fix is to remove the library and add it
	// again. Saying that plainly beats implying a settings toggle exists.
	Remedy string `json:"remedy,omitempty"`
}

// SetShapeWarning stores (or clears, when w is nil) a library's last verdict.
// Called at the end of every successful scan, including to clear one that a
// re-scan no longer justifies — a warning that outlives the condition it
// describes is worse than none.
func (s *Store) SetShapeWarning(ctx context.Context, libraryID int64, w *ShapeWarning) error {
	var payload any
	if w != nil && w.Code != "" {
		b, err := json.Marshal(w)
		if err != nil {
			return fmt.Errorf("set shape warning: %w", err)
		}
		payload = string(b)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE library SET shape_warning = ? WHERE id = ?`, payload, libraryID)
	if err != nil {
		return fmt.Errorf("set shape warning: %w", err)
	}
	return nil
}

// parseShapeWarning turns the stored column into a value, tolerating anything
// unreadable: a warning that cannot be parsed is not worth failing a library
// listing over.
func parseShapeWarning(raw *string) *ShapeWarning {
	if raw == nil || *raw == "" {
		return nil
	}
	var w ShapeWarning
	if err := json.Unmarshal([]byte(*raw), &w); err != nil || w.Code == "" {
		return nil
	}
	return &w
}

// LibraryShape counts what a library actually holds, by kind.
type LibraryShape struct {
	Shows    int `json:"shows"`
	Seasons  int `json:"seasons"`
	Episodes int `json:"episodes"`
	Movies   int `json:"movies"`
	// Parts are pieces of one work (ADR 0017). They matter here because the
	// specific way a shows library goes wrong as a movie library is that a
	// miniseries becomes one film in several parts.
	Parts  int `json:"parts"`
	Tracks int `json:"tracks"`
	Photos int `json:"photos"`
	Total  int `json:"total"`
}

// Shape counts a library's items by kind, ignoring missing rows: the question
// is what this library is, and a file that has gone offline says nothing about
// whether the kind was chosen correctly.
func (s *Store) Shape(ctx context.Context, libraryID int64) (LibraryShape, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT kind, COUNT(*) FROM media_item
		WHERE library_id = ? AND missing = 0
		GROUP BY kind`, libraryID)
	if err != nil {
		return LibraryShape{}, fmt.Errorf("library shape: %w", err)
	}
	defer rows.Close()

	var sh LibraryShape
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			return LibraryShape{}, fmt.Errorf("library shape: %w", err)
		}
		switch kind {
		case "show":
			sh.Shows = n
		case "season":
			sh.Seasons = n
		case "episode":
			sh.Episodes = n
		case "movie":
			sh.Movies = n
		case "part", "chapter":
			sh.Parts += n
		case "track":
			sh.Tracks = n
		case "photo":
			sh.Photos = n
		}
		// Playlists and albums are deliberately not counted: a playlist is not
		// evidence about a library's kind, and counting it in Total would make
		// "did this library import anything" answer yes for a library that
		// imported one .m3u and no music.
		if kind != "playlist" && kind != "album" && kind != "artist" &&
			kind != "gallery" {
			sh.Total += n
		}
	}
	return sh, rows.Err()
}
