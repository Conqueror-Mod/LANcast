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
	PendingCount(ctx context.Context) (int, error)
	GetLibrary(ctx context.Context, id int64) (*store.Library, error)
	LockedFields(ctx context.Context, itemID int64) ([]string, error)
	UpdateItemMetadata(ctx context.Context, itemID int64, m store.ItemMetadata) error
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
		}
		w.stats.UpdatedAt = time.Now().Unix()
		w.mu.Unlock()
	}()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		items, err := w.st.PendingEnrichment(ctx, w.BatchSize)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}

		progressed, err := w.processBatch(ctx, items)
		if err != nil {
			return err
		}

		// The queue is a query, not a cursor, so a batch that stamps nothing
		// returns the identical rows forever. This happens legitimately —
		// no provider configured, or every item failing — and without this
		// guard the worker spins at full tilt instead of stopping.
		if progressed == 0 {
			w.log.Debug("enrichment made no progress; stopping", "pending", len(items))
			return nil
		}

		if remaining, err := w.st.PendingCount(ctx); err == nil {
			w.mu.Lock()
			w.stats.Remaining = remaining
			w.mu.Unlock()
		}
	}
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
func (w *Worker) ApplyMatch(ctx context.Context, item store.Item, providerID, externalID string) error {
	kind := meta.Kind(item.Kind)

	provider, ok := w.reg.Provider(providerID)
	if !ok {
		return fmt.Errorf("unknown provider %q", providerID)
	}
	ref := meta.Ref{Kind: kind, ExternalID: externalID}
	if kind == meta.KindEpisode {
		ref.Season = derefInt(item.Season)
		ref.Episode = derefInt(item.Episode)
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
		if r, err := src.Read(ctx, item.Path, kind); err == nil && r != nil {
			locals = append(locals, *r)
		}
	}

	return w.applyRecords(ctx, item, kind, lockedSet, locals, []meta.Record{*rec}, meta.StateLocked, 1.0)
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
	if merged.ExternalID != "" {
		upd.Provider = &merged.Source
		upd.ExternalID = &merged.ExternalID
	}
	if state != "" {
		upd.MatchState = &state
		upd.MatchScore = &score
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

	if write := w.nfoWriter(); write != nil {
		if err := write(item.Path, kind, &merged); err != nil {
			// Failing to write a sidecar must not fail enrichment; the
			// database is still the working record.
			w.log.Warn("nfo write failed", "item", item.ID, "error", err)
		}
	}
	return nil
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
