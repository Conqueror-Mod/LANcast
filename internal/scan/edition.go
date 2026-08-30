package scan

import (
	"context"
	"fmt"

	"lancast/internal/media"
	"lancast/internal/store"
)

/*
 * Reading the edition marker onto rows written before the column existed.
 *
 * ADR 0049 measured the gap: `edition` is written by UpsertItem, the scanner
 * only upserts files whose size or mtime changed, and so every row older than
 * the column reads NULL for ever. On the reporting library that was all 1,229
 * movies. No rule can group on a field that is null everywhere, which is why
 * this is the prerequisite for any edition feature rather than a detail of one.
 *
 * # Why this runs in a scan, when re-parse deliberately does not
 *
 * Reparse is an explicit operator action because it rewrites *identity*, and a
 * rescan reconciles files rather than re-litigating what they are (CLAUDE.md,
 * ADR 0030). That reasoning does not reach this. An edition marker is not an
 * identity: it does not change which work a file belongs to, what it matched,
 * or what any provider was asked. It records which cut of a film this
 * particular file holds — a fact about the file, which is exactly what a scan
 * is for.
 *
 * It is also cheap enough to belong there. NULL keeps meaning "no marker" — the
 * existing contract, which `Edition *string` exists to express (ADR 0042) — so
 * this pass cannot tell an unmarked film from an unread one and re-reads the
 * unmarked ones every scan. Priced rather than assumed: one indexed query for
 * the candidates, pure string work to parse them, and a write only for the rows
 * that actually carry a marker. On the reporting library that is six writes
 * once, and a query returning ~1,200 rows thereafter.
 *
 * Running inside the scan is what makes the display correct without a second
 * invalidation, too: a completed scan already refreshes the lists this changes.
 */

// EditionResult reports what a backfill did, in the terms the question is asked
// in: how many rows carried no marker to begin with, and how many turned out to
// have one in their filename all along.
type EditionResult struct {
	Examined int `json:"examined"`
	Marked   int `json:"marked"`
}

// EditionStore is the persistence surface an edition backfill needs.
type EditionStore interface {
	EditionBackfillTargets(ctx context.Context, libraryID int64) ([]store.EditionTarget, error)
	SetEdition(ctx context.Context, itemID int64, edition string) (bool, error)
}

/*
 * BackfillEditions reads the filename of every film that carries no edition
 * marker, and records the ones that turn out to have had a marker all along.
 *
 * A film with no marker is skipped rather than stamped. NULL is already how
 * "no marker" is spelt, and writing anything else would be a second spelling of
 * it for the API to disagree with itself over.
 *
 * The parse goes through the same media.Parse the scanner uses. A second
 * opinion about what a filename means is what CLAUDE.md forbids, and an
 * edition-only parser would be exactly that.
 *
 * A row that fails ends the pass. The caller logs it and lets the scan succeed,
 * because an unread marker is a missing label rather than a broken library —
 * and the next scan simply tries again, since this pass is driven by the state
 * of the column rather than by a stamp it has to keep.
 */
func BackfillEditions(ctx context.Context, st EditionStore, libraryID int64) (EditionResult, error) {
	targets, err := st.EditionBackfillTargets(ctx, libraryID)
	if err != nil {
		return EditionResult{}, fmt.Errorf("edition backfill: %w", err)
	}

	res := EditionResult{Examined: len(targets)}
	for _, t := range targets {
		if err := ctx.Err(); err != nil {
			return res, err
		}

		info := media.Parse(t.Root, t.Path, t.LibKind)
		if info.Edition == "" {
			continue
		}
		marked, err := st.SetEdition(ctx, t.ItemID, info.Edition)
		if err != nil {
			return res, fmt.Errorf("edition backfill item %d: %w", t.ItemID, err)
		}
		if marked {
			res.Marked++
		}
	}
	return res, nil
}
