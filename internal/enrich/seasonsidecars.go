package enrich

import (
	"context"
	"os"

	"lancast/internal/meta"
	"lancast/internal/meta/nfo"
	"lancast/internal/store"
)

/*
 * sweepSeasonSidecars removes the `tvshow.nfo` files an earlier build wrote
 * into season folders (R08-5), once per process, on the worker's first run.
 *
 * Here rather than on the next enrichment of each season because a season that
 * is already matched is not enriched again, so the files would stay for ever.
 * It runs whether or not "write NFO" is on now: the setting may have been on
 * when they were written and off since.
 *
 * nfo.RemoveStraySeasonSidecar decides what is LANcast's to delete, and keeps
 * anything it cannot prove LANcast wrote and nobody has changed.
 */
func (w *Worker) sweepSeasonSidecars(ctx context.Context) {
	removed := 0
	for offset := 0; ; {
		page, _, err := w.st.ListItems(ctx, store.ItemFilter{
			Kind: string(meta.KindSeason), Limit: 500, Offset: offset,
		})
		if err != nil {
			w.log.Debug("season sidecar sweep: list failed", "error", err)
			return
		}
		for _, s := range page {
			if ctx.Err() != nil {
				return
			}
			// Only a real season folder ever got a file; a season without one
			// is identified by `<show>::season=N`, which is not a directory.
			if st, err := os.Stat(s.Path); err != nil || !st.IsDir() {
				continue
			}
			ok, err := nfo.RemoveStraySeasonSidecar(s.Path)
			if err != nil {
				w.log.Debug("season sidecar sweep: skipped", "item", s.ID, "error", err)
				continue
			}
			if ok {
				removed++
			}
		}
		if len(page) < 500 {
			break
		}
		offset += len(page)
	}
	if removed > 0 {
		w.log.Info("removed tvshow.nfo files an earlier version wrote into season folders",
			"count", removed)
	}
}
