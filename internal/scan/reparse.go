package scan

import (
	"context"
	"fmt"

	"lancast/internal/media"
	"lancast/internal/store"
)

/*
 * Re-parsing: applying today's filename heuristics to rows parsed by an older
 * version of them.
 *
 * A scan reconciles *files* — what is on disk, what has gone missing. It does
 * not re-litigate identity, and that separation is deliberate (CLAUDE.md). So
 * when the parser improves, every row already in the database keeps the guess
 * the old parser made, forever, and the improvement only ever reaches files
 * added afterwards.
 *
 * That is the gap this closes, and it is an explicit operator action rather
 * than something a rescan starts doing quietly, precisely because it rewrites
 * identity. The same reasoning as ADR 0030 for playlists: a rescan reconciles
 * files, a person decides what things are.
 */

// ReparseResult reports what a re-parse did, in the terms an operator asked the
// question in: how many rows were eligible, and how many actually changed.
type ReparseResult struct {
	Examined int `json:"examined"`
	Changed  int `json:"changed"`
}

// ReparseStore is the persistence surface a re-parse needs.
type ReparseStore interface {
	ReparseTargets(ctx context.Context, libraryID int64) ([]store.ReparseTarget, error)
	ApplyGuess(ctx context.Context, itemID int64, g store.Guess) (bool, error)
}

/*
 * Reparse re-runs the filename heuristics over a library's uncertain rows and
 * requeues the ones whose guess changed.
 *
 * Scope, ordering and locking all live in the store: only 'review' and
 * 'unmatched' rows are offered, and locked fields are skipped per field. This
 * function's whole job is to be the one place that turns a path back into a
 * guess, using the same media.Parse the scanner uses — a second opinion about
 * what a filename means is exactly what CLAUDE.md forbids.
 *
 * A failure on one row does not stop the run. The rows are independent, and an
 * operator who asked to repair a library is worse served by a run that stops at
 * the first odd path than by one that fixes the rest and says how many.
 */
func Reparse(ctx context.Context, st ReparseStore, libraryID int64) (ReparseResult, error) {
	targets, err := st.ReparseTargets(ctx, libraryID)
	if err != nil {
		return ReparseResult{}, fmt.Errorf("reparse: %w", err)
	}

	res := ReparseResult{Examined: len(targets)}
	for _, t := range targets {
		if err := ctx.Err(); err != nil {
			return res, err
		}

		info := media.Parse(t.Root, t.Path, t.LibKind)
		changed, err := st.ApplyGuess(ctx, t.ItemID, store.Guess{
			Title:     info.Title,
			SortTitle: media.SortTitle(info.Title),
			Year:      info.Year,
			Series:    info.Series,
			Season:    info.Season,
			Episode:   info.Episode,
		})
		if err != nil {
			return res, fmt.Errorf("reparse item %d: %w", t.ItemID, err)
		}
		if changed {
			res.Changed++
		}
	}
	return res, nil
}
