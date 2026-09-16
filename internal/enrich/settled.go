package enrich

import (
	"context"
	"fmt"
	"log/slog"

	"lancast/internal/meta"
	"lancast/internal/store"
)

/*
 * Re-asking the provider about a title whose identity is already settled.
 *
 * The background pass searches and scores; ApplyMatch writes an identity a
 * person chose. This is the third thing, and it is the one that was missing: an
 * identity nobody is questioning, re-fetched because the *provider* now answers
 * a question it did not used to.
 *
 * That is not hypothetical. Certificates were not fetched at all until v0.9.23,
 * so every title enriched before it carries none — and a locked title could
 * never be told, because every refresh in the project works by requeueing a row
 * for the search-and-score pass, which is precisely what a lock forbids. On the
 * library this was built for, eighteen locked titles had no `content_rating`,
 * three of them were shows, and the 88 episodes beneath them inherited nothing.
 * An account with a rating ceiling could not play any of them.
 *
 * What makes this safe where a requeue is not:
 *
 *   it fetches by the row's OWN recorded id, so nothing is searched
 *   it does not touch match_state or match_score, so nothing is re-scored
 *   it passes the row's existing locks to the merge, so no locked field moves
 *
 * The locked-fields rule is that a locked field is never overwritten and a
 * locked match is never re-scored. This does neither. A rescan reconciles files
 * and does not re-litigate identity — and neither does this; it re-asks about
 * an identity that is not in question.
 */

// RefetchSettled re-fetches one item from its recorded provider id and merges
// the answer, leaving its identity exactly as it was.
//
// Returns false with no error when the item cannot be asked about — no provider
// id, or a provider this server no longer has configured. That is an ordinary
// outcome for a library enriched by a provider whose key has since been
// removed, and a pass over hundreds of rows must not stop for it.
func (w *Worker) RefetchSettled(ctx context.Context, item store.Item) (bool, error) {
	// Nullable in the schema: a row that no provider ever identified carries
	// neither, and a locked row set by hand may carry neither either.
	providerID, externalID := derefStr(item.Provider), derefStr(item.ExternalID)
	if providerID == "" || externalID == "" {
		return false, nil
	}
	provider, ok := w.reg.Provider(providerID)
	if !ok {
		return false, nil
	}

	kind := meta.Kind(item.Kind)
	ref := meta.Ref{Kind: kind, ExternalID: externalID}
	switch kind {
	case meta.KindEpisode:
		ref.Season = derefInt(item.Season)
		ref.Episode = derefInt(item.Episode)
	case meta.KindSeason:
		ref.Season = derefInt(item.Season)
	}

	rec, err := provider.Fetch(ctx, ref)
	if err != nil {
		return false, fmt.Errorf("refetch %q: %w", item.Title, err)
	}
	if rec == nil {
		// The id no longer resolves — a TMDB entry merged or withdrawn. Not an
		// error for this row and certainly not a reason to abandon the pass;
		// the identity stays exactly as it was, which is the conservative
		// answer when the provider has stopped agreeing it exists.
		return false, nil
	}

	locked, err := w.st.LockedFields(ctx, item.ID)
	if err != nil {
		return false, err
	}
	lockedSet := meta.LockedSet(locked)

	var locals []meta.Record
	for _, src := range w.reg.Locals() {
		if r, err := src.Read(ctx, item.Path, kind); err == nil && r != nil {
			locals = append(locals, *r)
		}
	}

	/*
	 * The row's own state and score are passed straight back in.
	 *
	 * This is the line that makes the whole operation legitimate, so it is
	 * worth saying plainly rather than leaving to be inferred: ApplyMatch
	 * *decides* a state because a person just chose an identity. Here nobody
	 * chose anything, so writing any state would be this pass forming an
	 * opinion about an identity it was told not to question. A locked row stays
	 * locked at the score it had.
	 */
	return true, w.applyRecords(ctx, item, kind, lockedSet, locals,
		[]meta.Record{*rec}, item.MatchState, derefFloat(item.MatchScore))
}

/*
 * RefetchSettledAll runs the pass over a library's settled rows.
 *
 * Reports how many were actually updated, which is not the same as how many
 * were tried: a row whose provider is gone, or whose id no longer resolves, is
 * skipped rather than failed. The caller priced the attempt; this reports the
 * outcome, and the two differing is information rather than a fault.
 *
 * One bad row does not end the run. A pass that abandoned eighteen titles
 * because the fourth had been withdrawn from TMDB would be a button nobody
 * could rely on, so failures are logged per row and counted.
 */
func (w *Worker) RefetchSettledAll(ctx context.Context, items []store.Item, log *slog.Logger) (updated, failed int) {
	for _, item := range items {
		// A cancelled context ends the run rather than burning through the
		// remaining lookups; the pass is resumable because nothing about a row
		// changes until its fetch succeeds.
		if ctx.Err() != nil {
			break
		}
		ok, err := w.RefetchSettled(ctx, item)
		switch {
		case err != nil:
			failed++
			if log != nil {
				log.Warn("settled refresh failed for one title",
					"item", item.ID, "title", item.Title, "error", err)
			}
		case ok:
			updated++
		}
	}
	return updated, failed
}

// derefStr and derefFloat read the nullable identity columns. Beside derefInt
// in enrich.go for the same reason it is there: a `provider` of NULL and one of
// "" mean the same thing to this package, and spelling the check out at every
// site invites one of them to be forgotten.
func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefFloat(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}
