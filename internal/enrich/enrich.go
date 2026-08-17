// Package enrich fills in metadata and artwork for scanned items.
//
// Scanning and enriching are separate phases on purpose: a large first scan
// populates the grid from filenames in seconds while metadata fills in behind
// it, rather than the user staring at nothing for twenty minutes.
package enrich

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"lancast/internal/media"
	"lancast/internal/meta"
	"lancast/internal/store"
)

// Store is the persistence surface the worker needs.
type Store interface {
	PendingEnrichment(ctx context.Context, limit int) ([]store.Item, error)
	PendingEnrichmentFrom(ctx context.Context, limit, offset int) ([]store.Item, error)
	PendingCount(ctx context.Context) (int, error)
	GetLibrary(ctx context.Context, id int64) (*store.Library, error)
	LockedFields(ctx context.Context, itemID int64) ([]string, error)
	ParentIdentity(ctx context.Context, itemID int64) (provider, externalID string, ok bool, err error)
	UpdateItemMetadata(ctx context.Context, itemID int64, m store.ItemMetadata) error
	SaveRatings(ctx context.Context, itemID int64, ratings []store.ItemRating) error
	ReplaceGenres(ctx context.Context, itemID int64, names []string) error
	ReplaceCredits(ctx context.Context, itemID int64, provider string, credits []store.Credit) error
	PutArtwork(ctx context.Context, itemID int64, hash, kind, sourceURL string, w, h int, size int64) error
	ArtworkExists(ctx context.Context, hash string) (bool, error)
	EnsureCollection(ctx context.Context, libraryID int64, provider, externalID, name, sortTitle string) (int64, bool, error)
	AddToCollection(ctx context.Context, itemID, collectionID int64, ord int) error
}

// ArtCache is the artwork side of the worker.
type ArtCache interface {
	Download(ctx context.Context, url string) (hash string, w, h int, size int64, err error)
	Stored(hash string) bool
}

// Worker enriches pending items in the background.
type Worker struct {
	st       Store
	reg      *meta.Registry
	art      ArtCache
	log      *slog.Logger
	nfoWrite func(path string, kind meta.Kind, rec *meta.Record) error

	// Concurrency bounds simultaneous provider calls.
	Concurrency int
	// BatchSize is how many items one pass claims.
	BatchSize int

	mu      sync.Mutex
	running bool
	stats   Stats
}

// Stats is a snapshot of enrichment progress.
//
// Remaining is the real outstanding count, not the current batch size, and
// Total is the size of the job when it started. Together they give a figure
// that means something: a bare count that resets whenever a new pass begins
// looks like the work is being redone.
type Stats struct {
	Running   bool  `json:"running"`
	Enriched  int   `json:"enriched"`
	Failed    int   `json:"failed"`
	Remaining int   `json:"remaining"`
	Total     int   `json:"total"`
	UpdatedAt int64 `json:"updated_at"`
}

// Option customizes a Worker.
type Option func(*Worker)

// WithNFOWriter enables writing metadata back to sidecar files. Opt-in per
// deployment: nothing is written into media folders without being asked.
func WithNFOWriter(fn func(path string, kind meta.Kind, rec *meta.Record) error) Option {
	return func(w *Worker) { w.nfoWrite = fn }
}

func New(st Store, reg *meta.Registry, art ArtCache, log *slog.Logger, opts ...Option) *Worker {
	w := &Worker{
		st: st, reg: reg, art: art, log: log,
		Concurrency: 4,
		BatchSize:   50,
	}
	for _, o := range opts {
		o(w)
	}
	return w
}

// SetNFOWriter swaps the sidecar writer at runtime, so toggling the setting
// takes effect without a restart. Pass nil to disable writing.
func (w *Worker) SetNFOWriter(fn func(path string, kind meta.Kind, rec *meta.Record) error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.nfoWrite = fn
}

// nfoWriter reads the hook under lock, since settings can change mid-run.
func (w *Worker) nfoWriter() func(string, meta.Kind, *meta.Record) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.nfoWrite
}

// Stats returns the current progress snapshot.
func (w *Worker) Stats() Stats {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stats
}

// Run processes pending items until the queue is empty or ctx is done. It is
// safe to call repeatedly; a second concurrent call returns immediately.
func (w *Worker) Run(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.stats = Stats{Running: true, UpdatedAt: time.Now().Unix()}
	w.mu.Unlock()

	// Size the job up front so progress can be reported against something.
	if total, err := w.st.PendingCount(ctx); err == nil {
		w.mu.Lock()
		w.stats.Total = total
		w.stats.Remaining = total
		w.mu.Unlock()
	}

	defer func() {
		// Recompute rather than trusting the last loop value: a run can end
		// early (no progress possible, context cancelled), and reporting a
		// stale figure after finishing is how "still 25 left" outlives the job.
		remaining, err := w.st.PendingCount(context.WithoutCancel(ctx))

		w.mu.Lock()
		w.running = false
		w.stats.Running = false
		if err == nil {
			w.stats.Remaining = remaining
			// The final figure has to agree with itself too: a run that enriched
			// more than it was sized for must not sign off saying "682 of 449".
			w.stats.Total = growTotal(w.stats.Total, w.stats.Enriched, remaining)
		}
		w.stats.UpdatedAt = time.Now().Unix()
		w.mu.Unlock()
	}()

	// offset walks past items this run cannot stamp. The queue is a query, not a
	// cursor: enriched rows leave it, but rows nothing can enrich stay at the
	// front forever. Stopping at the first unproductive batch — which is what
	// this loop used to do — strands everything behind it, so a music backlog no
	// provider handles meant no film added afterwards was ever enriched.
	offset := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		items, err := w.st.PendingEnrichmentFrom(ctx, w.BatchSize, offset)
		if err != nil {
			return err
		}
		// Nothing left to look at. With a non-zero offset this is the real
		// terminating condition: the run has walked the whole queue.
		if len(items) == 0 {
			return nil
		}

		progressed, err := w.processBatch(ctx, items)
		if err != nil {
			return err
		}

		if progressed == 0 {
			// Skip this batch rather than the run. The offset is what stops the
			// worker re-reading the identical unproductive rows at full tilt,
			// which is the spin the old guard was protecting against.
			offset += len(items)
			w.log.Debug("enrichment batch made no progress; looking past it",
				"skipped", len(items), "offset", offset)
			continue
		}

		// Stamped rows have left the queue, so the rows this run already skipped
		// have shifted forward by however many were enriched. Restarting from
		// zero re-reads them, which is cheap and cannot miss anything.
		offset = 0

		if remaining, err := w.st.PendingCount(ctx); err == nil {
			w.mu.Lock()
			w.stats.Remaining = remaining
			w.stats.Total = growTotal(w.stats.Total, w.stats.Enriched, remaining)
			w.mu.Unlock()
		}
	}
}

/*
 * growTotal keeps the reported total at least as big as the job turned out to be.
 *
 * Total was sized once, when the run began, and never revised — so anything that
 * joined the queue mid-run marched progress straight past it. The activity panel
 * read **"682 of 449"**, which is not a rounding error but a bar that has lost
 * its meaning: the work was real, the denominator was from several minutes ago.
 *
 * Requeueing mid-run is ordinary rather than exceptional. A scan adds rows while
 * enrichment is already going, `refresh` clears the stamp on a whole library, and
 * `reparse` requeues everything it corrected — the queue is a query, and a query
 * answers differently when the data changes underneath it.
 *
 * Done plus outstanding, and never allowed to shrink. Monotonic because a
 * progress bar that jumps backwards reads as a fault in the thing it is
 * measuring, and callers watching `Total` should see an estimate that improves
 * rather than one that oscillates.
 *
 * Failures are deliberately not added on top: a failed item stays pending and is
 * retried, so it is already inside `remaining`, and counting it twice would
 * inflate the total by every transient provider error.
 */
func growTotal(prev, enriched, remaining int) int {
	if n := enriched + remaining; n > prev {
		return n
	}
	return prev
}

// processBatch enriches items concurrently and reports how many advanced.
func (w *Worker) processBatch(ctx context.Context, items []store.Item) (int, error) {
	sem := make(chan struct{}, max(1, w.Concurrency))
	var wg sync.WaitGroup
	var progressed atomic.Int64

	for i := range items {
		item := items[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			advanced, err := w.enrichOne(ctx, item)
			if err != nil {
				// One item failing must not sink the batch. It stays pending
				// and is retried on the next pass.
				w.log.Warn("enrich failed", "item", item.ID, "title", item.Title, "error", err)
				w.mu.Lock()
				w.stats.Failed++
				w.mu.Unlock()
				return
			}
			if !advanced {
				return
			}
			progressed.Add(1)
			w.mu.Lock()
			w.stats.Enriched++
			w.stats.UpdatedAt = time.Now().Unix()
			w.mu.Unlock()
		}()
	}
	wg.Wait()
	return int(progressed.Load()), ctx.Err()
}

// enrichOne resolves one item through local sources, then providers, then the
// merge engine. It reports whether the item advanced — false means it was left
// pending deliberately and will be retried later.
func (w *Worker) enrichOne(ctx context.Context, item store.Item) (bool, error) {
	kind := meta.Kind(item.Kind)

	locked, err := w.st.LockedFields(ctx, item.ID)
	if err != nil {
		return false, err
	}
	lockedSet := meta.LockedSet(locked)

	// Local sources first — they outrank providers for every field.
	var locals []meta.Record
	for _, src := range w.reg.Locals() {
		rec, err := src.Read(ctx, item.Path, kind)
		if err != nil {
			w.log.Debug("local source failed", "source", src.ID(), "item", item.ID, "error", err)
			continue
		}
		if rec != nil {
			locals = append(locals, *rec)
		}
	}

	remotes, score, state := w.fetchRemote(ctx, item, kind)

	// Nothing to apply and no verdict to record: leave it pending rather than
	// stamping it done, so a later run with a configured key picks it up.
	if len(locals) == 0 && len(remotes) == 0 && state == "" {
		return false, nil
	}

	// A local source resolved it and no provider weighed in. That is an
	// answer, not an absence of one — the user already said what this is.
	if len(locals) > 0 && state == "" {
		state = meta.StateLocal
		score = 1
	}

	return true, w.applyRecords(ctx, item, kind, lockedSet, locals, remotes, state, score)
}

// ApplyMatch writes a user-confirmed identity. Unlike the background pass it
// does not search or score — the user already chose the exact record — and it
// operates on a locked item, which the pending queue deliberately skips. This
// is why confirming a match cannot go through that queue: it would re-search and
// re-pick the very candidate the user rejected.
//
// matchKind is the chosen candidate's kind, which may differ from the item's own
// — correcting a movie-scanned miniseries to its TV entry fetches from /tv even
// though the item is a 'movie'. Empty falls back to the item's kind. The record
// is fetched as matchKind but applied under the item's own kind, so the metadata
// is right without a local-source or nfo path being read against the wrong type.
func (w *Worker) ApplyMatch(ctx context.Context, item store.Item, providerID, externalID string, matchKind meta.Kind) error {
	itemKind := meta.Kind(item.Kind)
	if matchKind == "" {
		matchKind = itemKind
	}

	provider, ok := w.reg.Provider(providerID)
	if !ok {
		return fmt.Errorf("unknown provider %q", providerID)
	}
	ref := meta.Ref{Kind: matchKind, ExternalID: externalID}
	switch matchKind {
	case meta.KindEpisode:
		ref.Season = derefInt(item.Season)
		ref.Episode = derefInt(item.Episode)
	case meta.KindSeason:
		// The chosen id is a show's; the season number selects within it.
		ref.Season = derefInt(item.Season)
	}
	rec, err := provider.Fetch(ctx, ref)
	if err != nil {
		return fmt.Errorf("fetch confirmed match: %w", err)
	}
	if rec == nil {
		return fmt.Errorf("provider %q returned no record for %q", providerID, externalID)
	}

	locked, err := w.st.LockedFields(ctx, item.ID)
	if err != nil {
		return err
	}
	lockedSet := meta.LockedSet(locked)

	var locals []meta.Record
	for _, src := range w.reg.Locals() {
		if r, err := src.Read(ctx, item.Path, itemKind); err == nil && r != nil {
			locals = append(locals, *r)
		}
	}

	return w.applyRecords(ctx, item, itemKind, lockedSet, locals, []meta.Record{*rec}, meta.StateLocked, 1.0)
}

// applyRecords merges resolved records over an item's current metadata and
// writes the result, honouring locked fields. Shared by the background pass and
// a confirmed match so the two can never disagree about how a record is applied.
func (w *Worker) applyRecords(ctx context.Context, item store.Item, kind meta.Kind, lockedSet map[string]bool, locals, remotes []meta.Record, state string, score float64) error {
	current := currentRecord(item)
	merged := meta.Merge(current, lockedSet, locals, remotes)

	upd := store.ItemMetadata{
		Title:         merged.Fields.Title,
		SortTitle:     merged.Fields.SortTitle,
		Year:          merged.Fields.Year,
		Overview:      merged.Fields.Overview,
		Rating:        merged.Fields.Rating,
		ContentRating: merged.Fields.ContentRating,
		ReleasedAt:    merged.Fields.ReleasedAt,
		DurationMS:    merged.Fields.DurationMS,
		Series:        merged.Fields.Series,
		Season:        merged.Fields.Season,
		Episode:       merged.Fields.Episode,
	}
	// Derive the sort title from whatever title won, unless it is locked.
	if upd.Title != nil && !lockedSet[meta.FieldSortTitle] {
		s := media.SortTitle(*upd.Title)
		if merged.Fields.Series != nil && *merged.Fields.Series != "" {
			s = media.SortTitle(*merged.Fields.Series)
		}
		upd.SortTitle = &s
	}
	// A season sorts by its number, not by its name.
	//
	// The default listing order leads with sort_title, and a season's name is
	// "Season 10" — which sorts before "Season 2" as text. Zero-padding is not a
	// title-normalization opinion competing with internal/media (CLAUDE.md); it
	// is a numeric key for a row whose name *is* a number.
	if kind == meta.KindSeason && item.Season != nil && !lockedSet[meta.FieldSortTitle] {
		s := fmt.Sprintf("season %03d", *item.Season)
		upd.SortTitle = &s
	}
	if merged.ExternalID != "" {
		upd.Provider = &merged.Source
		upd.ExternalID = &merged.ExternalID
	}
	if state != "" {
		upd.MatchState = &state
		upd.MatchScore = &score
	}
	// The imdb id is the join key for external ratings (ADR 0019). Only remote
	// records carry it — a local NFO does not model one — so read the remotes.
	imdbID := ""
	for _, rec := range remotes {
		if rec.IMDbID != "" {
			imdbID = rec.IMDbID
			break
		}
	}
	if imdbID != "" {
		upd.IMDbID = &imdbID
	}

	if err := w.st.UpdateItemMetadata(ctx, item.ID, upd); err != nil {
		return err
	}
	if len(merged.Genres) > 0 && !lockedSet[meta.FieldGenres] {
		if err := w.st.ReplaceGenres(ctx, item.ID, merged.Genres); err != nil {
			return err
		}
	}
	if len(merged.Credits) > 0 && !lockedSet[meta.FieldCredits] {
		credits := make([]store.Credit, 0, len(merged.Credits))
		for _, c := range merged.Credits {
			credits = append(credits, store.Credit{
				Name: c.Name, Role: c.Role, Character: c.Character, Order: c.Order,
			})
		}
		if err := w.st.ReplaceCredits(ctx, item.ID, merged.Source, credits); err != nil {
			return err
		}
	}
	if !lockedSet[meta.FieldArtwork] {
		w.storeArtwork(ctx, item.ID, merged.Artwork)
	}

	w.ingestCollection(ctx, item, remotes)
	w.fetchRatings(ctx, item.ID, imdbID)

	// A sidecar is written into the user's media folder with LANcast's own
	// provenance stamp on it, and it outlives the database that produced it —
	// rebuild the library and the file is still there, speaking. So it is only
	// written for an identity LANcast actually established.
	//
	// An unmatched item has a title from the filename and whatever a local
	// source offered, at a confidence the matcher itself declined to accept.
	// Committing that to disk turns a guess into a durable local fact, and the
	// next fresh database inherits it as one. This is the half of "a wrong
	// title outlived three databases" that was ours to stop.
	if write := w.nfoWriter(); write != nil && writableIdentity(state) {
		if err := write(item.Path, kind, &merged); err != nil {
			// Failing to write a sidecar must not fail enrichment; the
			// database is still the working record.
			w.log.Warn("nfo write failed", "item", item.ID, "error", err)
		}
	}
	return nil
}

// writableIdentity reports whether a match state is settled enough to write to
// the user's media folder.
//
// matched and local qualify: one is the matcher's own verdict, the other is a
// user or a sidecar that already said what this is. review and unmatched do
// not — review means the matcher wants a human to look, and writing the
// candidate it was unsure about would pre-empt that answer with a file.
func writableIdentity(state string) bool {
	return state == meta.StateMatched || state == meta.StateLocal
}

// fetchRemote searches providers, scores the candidates, and fetches the best
// one if it clears the review threshold.
func (w *Worker) fetchRemote(ctx context.Context, item store.Item, kind meta.Kind) ([]meta.Record, float64, string) {
	// "Nothing is configured" and "we looked and found nothing" are different
	// verdicts. Only the second is an answer worth recording; the first must
	// leave the item pending so a later run with a key picks it up.
	if len(w.reg.Providers(kind)) == 0 {
		return nil, 0, ""
	}

	// A season is resolved from its show, never searched for by name.
	if kind == meta.KindSeason {
		return w.fetchSeason(ctx, item)
	}

	q := meta.Query{
		Kind:  kind,
		Title: item.Title,
		Year:  derefInt(item.Year),
	}
	if item.Series != nil {
		q.Series = *item.Series
	}
	if item.Season != nil {
		q.Season = *item.Season
	}
	if item.Episode != nil {
		q.Episode = *item.Episode
	}

	cands, err := w.reg.Search(ctx, q)
	if err != nil || len(cands) == 0 {
		if err != nil {
			w.log.Debug("provider search failed", "item", item.ID, "error", err)
			// An unconfigured or unreachable provider is not a match failure.
			return nil, 0, ""
		}
		return nil, 0, meta.StateUnmatched
	}

	best := cands[0]
	state := meta.StateFor(best.Score)
	if state == meta.StateUnmatched {
		// Below the floor: record the uncertainty, apply nothing.
		return nil, best.Score, state
	}

	provider, ok := w.reg.Provider(best.Provider)
	if !ok {
		return nil, best.Score, state
	}

	ref := meta.Ref{Kind: kind, ExternalID: best.ExternalID}
	if kind == meta.KindEpisode {
		ref.Season = derefInt(item.Season)
		ref.Episode = derefInt(item.Episode)
	}

	rec, err := provider.Fetch(ctx, ref)
	if err != nil || rec == nil {
		if err != nil {
			w.log.Debug("provider fetch failed", "item", item.ID, "error", err)
		}
		return nil, best.Score, state
	}
	return []meta.Record{*rec}, best.Score, state
}

// fetchSeason resolves a season through the show that owns it.
//
// A season has no identity of its own. Its name is "Season 2" — a position,
// not the name of a work — so a name search cannot succeed, and when it does
// appear to succeed it has found a real show that merely has that phrase in its
// title. Those hits normalize to an exact title match and clear the auto-apply
// threshold, and because the query depends only on the season number the *same*
// wrong show wins for every show in the library. That is precisely how season 2
// of nine unrelated series came to share one poster.
//
// So the season number is looked up exactly against the parent's id rather than
// scored. There is nothing to be uncertain about: either the show is matched and
// the season is that show's season n, or the show is not and the season stays
// unmatched until it is. It is never sent to the review queue, which is the
// other half of the rule store.notReviewable already states.
func (w *Worker) fetchSeason(ctx context.Context, item store.Item) ([]meta.Record, float64, string) {
	if item.Season == nil {
		return nil, 0, meta.StateUnmatched
	}

	providerID, externalID, ok, err := w.st.ParentIdentity(ctx, item.ID)
	if err != nil {
		w.log.Debug("season parent lookup failed", "item", item.ID, "error", err)
		return nil, 0, ""
	}
	if !ok {
		// The show is not matched yet. Leave the season pending rather than
		// recording a verdict, so enriching the show later brings its seasons
		// along instead of stranding them at "unmatched" for ever.
		return nil, 0, ""
	}

	provider, found := w.reg.Provider(providerID)
	if !found {
		return nil, 0, ""
	}

	rec, err := provider.Fetch(ctx, meta.Ref{
		Kind:       meta.KindSeason,
		ExternalID: externalID,
		Season:     *item.Season,
	})
	if err != nil || rec == nil {
		if err != nil {
			w.log.Debug("season fetch failed", "item", item.ID, "error", err)
			return nil, 0, ""
		}
		// The show is matched and has no such season — the numbering is wrong,
		// which is a real answer and not a provider failure.
		return nil, 0, meta.StateUnmatched
	}
	// Confidence is not a scale here: the show's id was already established and
	// the season number is an exact lookup, so this is as matched as it gets.
	return []meta.Record{*rec}, 1, meta.StateMatched
}

// ingestCollection links an item into the franchise or series a provider says
// it belongs to (ADR 0017). It is membership, not containment: the item stays a
// top-level work and is only linked into a collection media_item, which is
// created on first sighting and given its own artwork once.
//
// Only remote records carry a collection — a local NFO does not model one — so
// this reads the remotes, not the merged record. Re-running is idempotent:
// EnsureCollection and AddToCollection both upsert.
func (w *Worker) ingestCollection(ctx context.Context, item store.Item, remotes []meta.Record) {
	for _, rec := range remotes {
		c := rec.Collection
		if c == nil || c.ExternalID == "" {
			continue
		}
		collID, created, err := w.st.EnsureCollection(
			ctx, item.LibraryID, rec.Source, c.ExternalID, c.Name, media.SortTitle(c.Name))
		if err != nil {
			w.log.Warn("ensure collection failed", "item", item.ID, "collection", c.Name, "error", err)
			return
		}
		if err := w.st.AddToCollection(ctx, item.ID, collID, 0); err != nil {
			w.log.Warn("add to collection failed", "item", item.ID, "collection", c.Name, "error", err)
		}
		// Download the collection's poster and backdrop once, when it is first
		// created — every other member would fetch the identical images.
		if created {
			w.storeArtwork(ctx, collID, c.Artwork)
		}
		return
	}
}

// fetchRatings pulls third-party scores for an item's imdb id and stores them
// (ADR 0019). It is best-effort: no imdb id or no rating source configured is a
// clean skip, a source failing is logged not fatal, and an empty result (the
// common case for older or non-US titles) simply writes nothing. It never blocks
// the item from being marked enriched — identity has already been applied.
func (w *Worker) fetchRatings(ctx context.Context, itemID int64, imdbID string) {
	if imdbID == "" || !w.reg.HasRatingSources() {
		return
	}
	ratings, err := w.reg.Ratings(ctx, imdbID)
	if err != nil {
		w.log.Debug("rating source failed", "item", itemID, "error", err)
	}
	if len(ratings) == 0 {
		return
	}
	rows := make([]store.ItemRating, 0, len(ratings))
	for _, r := range ratings {
		rows = append(rows, store.ItemRating{
			Source: r.Source, Score: r.Score, Display: r.Display, Votes: r.Votes,
		})
	}
	if err := w.st.SaveRatings(ctx, itemID, rows); err != nil {
		w.log.Warn("save ratings failed", "item", itemID, "error", err)
	}
}

// storeArtwork downloads and records images, skipping any already cached.
func (w *Worker) storeArtwork(ctx context.Context, itemID int64, refs []meta.ArtRef) {
	for _, ref := range refs {
		if ref.URL == "" {
			continue
		}
		hash, width, height, size, err := w.art.Download(ctx, ref.URL)
		if err != nil {
			w.log.Debug("artwork download failed", "item", itemID, "url", ref.URL, "error", err)
			continue
		}
		if err := w.st.PutArtwork(ctx, itemID, hash, string(ref.Kind), ref.URL, width, height, size); err != nil {
			w.log.Warn("artwork record failed", "item", itemID, "error", err)
		}
	}
}

// currentRecord expresses an item's existing values so the merge engine can
// keep locked fields and fall back to what is already there.
func currentRecord(item store.Item) meta.Record {
	rec := meta.Record{Kind: meta.Kind(item.Kind)}
	rec.Fields.Title = &item.Title
	rec.Fields.SortTitle = &item.SortTitle
	rec.Fields.Year = item.Year
	rec.Fields.Overview = item.Overview
	rec.Fields.Rating = item.Rating
	rec.Fields.ContentRating = item.ContentRating
	rec.Fields.ReleasedAt = item.ReleasedAt
	rec.Fields.DurationMS = item.DurationMS
	rec.Fields.Series = item.Series
	rec.Fields.Season = item.Season
	rec.Fields.Episode = item.Episode
	return rec
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ErrBusy is reserved for callers that want to distinguish a no-op Run.
var ErrBusy = errors.New("enrichment already running")
