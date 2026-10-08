// Package identify names a retro library's ROMs (ADR 0073).
//
// Its own worker rather than part of enrichment, for the reason album art has
// one: no provider can search for a ROM, so enrichment would never reach it.
// And not part of the scan, because it reads every file — a first scan of a
// few thousand cartridges should finish and list them, with names following
// behind.
package identify

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"lancast/internal/media"
	"lancast/internal/meta"
	"lancast/internal/retro/retrodb"
	"lancast/internal/retro/romhash"
	"lancast/internal/store"
)

// Provider is what a match is recorded as coming from.
const Provider = "libretro-db"

// Store is the persistence the worker needs.
type Store interface {
	PendingROMs(ctx context.Context, limit int) ([]store.Item, error)
	PendingROMCount(ctx context.Context) (int, error)
	GetROMHash(ctx context.Context, itemID int64) (*store.ROMHash, error)
	PutROMHash(ctx context.Context, itemID int64, h store.ROMHash) error
	SetPlatform(ctx context.Context, itemID int64, platform string) error
	MarkROMChecked(ctx context.Context, itemID int64) error
	LockedFields(ctx context.Context, itemID int64) ([]string, error)
	UpdateItemMetadata(ctx context.Context, itemID int64, m store.ItemMetadata) error
	ReplaceGenres(ctx context.Context, itemID int64, names []string) error
	PutArtwork(ctx context.Context, itemID int64, hash, kind, sourceURL string, w, h int, size int64) error
}

// ArtCache downloads an image into the content-addressed cache.
type ArtCache interface {
	Download(ctx context.Context, url string) (hash string, w, h int, size int64, err error)
}

// Stats is a snapshot of progress.
type Stats struct {
	Running   bool  `json:"running"`
	Matched   int   `json:"matched"`
	Unmatched int   `json:"unmatched"`
	Failed    int   `json:"failed"`
	Remaining int   `json:"remaining"`
	Total     int   `json:"total"`
	UpdatedAt int64 `json:"updated_at"`
	/*
	 * FinishedAt is when a pass that changed something last ended, or zero.
	 *
	 * It is what tells a client its grid is stale. Identification renames rows
	 * a list already holds, and a pass over a small library finishes between
	 * two polls of /api/activity — so a client watching only for the
	 * running-to-idle edge never sees it, and the grid keeps the filenames.
	 * Kept across passes: a later pass that found nothing to do must not hide
	 * the one before it.
	 */
	FinishedAt int64 `json:"finished_at,omitempty"`
}

// Worker identifies pending ROMs in the background.
type Worker struct {
	st  Store
	log *slog.Logger

	// Index returns the installed DAT index, or nil when none is installed.
	// A function so an install takes effect without a restart.
	Index func() *retrodb.Index
	// Art and Artwork fetch box art when the setting allows. Artwork is
	// asked per ROM, so switching it off stops fetching at once.
	Art     ArtCache
	Artwork func() bool
	// Read hashes a ROM. romhash.Read, replaceable in tests.
	Read func(path, platform string) (romhash.Result, error)

	Concurrency int
	BatchSize   int

	mu      sync.Mutex
	running bool
	stats   Stats
}

func NewWorker(st Store, log *slog.Logger) *Worker {
	conc := runtime.NumCPU() / 2
	if conc < 1 {
		conc = 1
	}
	return &Worker{
		st: st, log: log,
		Index:       func() *retrodb.Index { return nil },
		Artwork:     func() bool { return false },
		Read:        romhash.Read,
		Concurrency: conc,
		BatchSize:   100,
	}
}

// Stats returns current progress.
func (w *Worker) Stats() Stats {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stats
}

// Run processes pending ROMs until the queue drains or ctx is done.
func (w *Worker) Run(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.stats = Stats{Running: true, UpdatedAt: time.Now().Unix(), FinishedAt: w.stats.FinishedAt}
	w.mu.Unlock()

	if total, err := w.st.PendingROMCount(ctx); err == nil {
		w.mu.Lock()
		w.stats.Total, w.stats.Remaining = total, total
		w.mu.Unlock()
	}
	defer func() {
		remaining, err := w.st.PendingROMCount(context.WithoutCancel(ctx))
		w.mu.Lock()
		w.running = false
		w.stats.Running = false
		if err == nil {
			w.stats.Remaining = remaining
		}
		w.stats.UpdatedAt = time.Now().Unix()
		if w.stats.Matched+w.stats.Unmatched+w.stats.Failed > 0 {
			w.stats.FinishedAt = w.stats.UpdatedAt
		}
		w.mu.Unlock()
	}()

	// Loaded once per pass, not per ROM: parsing the DATs is a fifth of a
	// second, and a pass is the unit an install is noticed at.
	ix := w.Index()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		roms, err := w.st.PendingROMs(ctx, w.BatchSize)
		if err != nil {
			return err
		}
		if len(roms) == 0 {
			return nil
		}
		progressed := w.processBatch(ctx, ix, roms)
		if err := ctx.Err(); err != nil {
			return err
		}
		// The queue is a query, not a cursor: a batch that stamps nothing
		// returns the same rows for ever. Every outcome stamps, so this trips
		// only if writes are failing.
		if progressed == 0 {
			w.log.Warn("rom identification made no progress; stopping", "pending", len(roms))
			return nil
		}
		if remaining, err := w.st.PendingROMCount(ctx); err == nil {
			w.mu.Lock()
			w.stats.Remaining = remaining
			w.mu.Unlock()
		}
	}
}

func (w *Worker) processBatch(ctx context.Context, ix *retrodb.Index, roms []store.Item) int {
	sem := make(chan struct{}, max(1, w.Concurrency))
	var wg sync.WaitGroup
	var progressed atomic.Int64
	for i := range roms {
		rom := roms[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			if w.processOne(ctx, ix, rom) {
				progressed.Add(1)
			}
		}()
	}
	wg.Wait()
	return int(progressed.Load())
}

type outcome int

const (
	outcomeMatched outcome = iota
	outcomeUnmatched
	outcomeFailed
	outcomeSkipped
)

// processOne identifies one ROM and reports whether it was stamped.
func (w *Worker) processOne(ctx context.Context, ix *retrodb.Index, rom store.Item) bool {
	o, err := w.identify(ctx, ix, rom)
	if err != nil {
		w.log.Warn("rom identification failed", "item", rom.ID, "path", rom.Path, "error", err)
		o = outcomeFailed
	}
	if err := w.st.MarkROMChecked(ctx, rom.ID); err != nil {
		w.log.Warn("stamping rom failed", "item", rom.ID, "error", err)
		return false
	}
	w.mu.Lock()
	switch o {
	case outcomeMatched:
		w.stats.Matched++
	case outcomeUnmatched:
		w.stats.Unmatched++
	case outcomeFailed:
		w.stats.Failed++
	}
	w.stats.UpdatedAt = time.Now().Unix()
	w.mu.Unlock()
	return true
}

func (w *Worker) identify(ctx context.Context, ix *retrodb.Index, rom store.Item) (outcome, error) {
	platform := ""
	if rom.Platform != nil {
		platform = *rom.Platform
	}

	h, err := w.st.GetROMHash(ctx, rom.ID)
	if err != nil {
		return outcomeFailed, err
	}
	if h == nil {
		res, err := w.Read(rom.Path, platform)
		if err != nil {
			// Unreadable is an outcome, not a retry: a truncated zip will be
			// truncated next time too. A changed file is queued again by its
			// upsert.
			if errors.Is(err, romhash.ErrTooLarge) {
				w.log.Info("not hashing a file too large to be a cartridge", "item", rom.ID, "path", rom.Path)
				return outcomeSkipped, nil
			}
			return outcomeFailed, err
		}
		h = &store.ROMHash{Serial: res.Serial, InnerName: res.InnerName}
		if len(res.Sums) > 0 {
			h.CRC32, h.SHA1 = res.Sums[0].CRC32, res.Sums[0].SHA1
		}
		if len(res.Sums) > 1 {
			h.AltCRC32, h.AltSHA1 = res.Sums[1].CRC32, res.Sums[1].SHA1
		}
		if err := w.st.PutROMHash(ctx, rom.ID, *h); err != nil {
			return outcomeFailed, err
		}
		if platform == "" && res.Platform != "" {
			platform = res.Platform
			if err := w.st.SetPlatform(ctx, rom.ID, platform); err != nil {
				return outcomeFailed, err
			}
		}
	}

	// Nothing to look up against yet. Stamped all the same — installing the
	// DATs re-queues every ROM, and the hash just stored makes that a lookup.
	if ix == nil {
		return outcomeSkipped, nil
	}
	// A rescan reconciles files; it never re-litigates an identity somebody
	// settled. The hash above is still refreshed, because it is a fact about
	// bytes rather than a decision.
	if rom.MatchState == meta.StateLocked {
		return outcomeSkipped, nil
	}

	sums := []romhash.Sums{{CRC32: h.CRC32, SHA1: h.SHA1}}
	if h.AltCRC32 != "" || h.AltSHA1 != "" {
		sums = append(sums, romhash.Sums{CRC32: h.AltCRC32, SHA1: h.AltSHA1})
	}
	g := ix.Lookup(platform, sums, h.Serial)
	if g == nil {
		state := meta.StateUnmatched
		return outcomeUnmatched, w.st.UpdateItemMetadata(ctx, rom.ID, store.ItemMetadata{MatchState: &state})
	}
	return outcomeMatched, w.apply(ctx, rom, platform, g)
}

// apply writes a match, field by field, leaving every locked field alone.
func (w *Worker) apply(ctx context.Context, rom store.Item, platform string, g *retrodb.Game) error {
	locked, err := w.st.LockedFields(ctx, rom.ID)
	if err != nil {
		return err
	}
	lockedSet := meta.LockedSet(locked)

	if platform == "" && g.Platform != "" {
		if err := w.st.SetPlatform(ctx, rom.ID, g.Platform); err != nil {
			return err
		}
	}

	provider, external, state, score := Provider, g.Name, meta.StateMatched, 1.0
	upd := store.ItemMetadata{
		Provider: &provider, ExternalID: &external, MatchState: &state, MatchScore: &score,
	}
	if !lockedSet[meta.FieldTitle] {
		// The DAT name carries region and revision tags; the title is the
		// name before them, through the same function the filename went
		// through, so a match and a guess read alike.
		if t := media.ROMTitle(g.Name); t != "" {
			upd.Title = &t
			if !lockedSet[meta.FieldSortTitle] {
				st := media.SortTitle(t)
				upd.SortTitle = &st
			}
		}
	}
	if g.Year > 0 && !lockedSet[meta.FieldYear] {
		y := g.Year
		upd.Year = &y
	}
	// Written with the system's name, which is what keeps ESRB's M (17) from
	// being read as Australia's M (15) on the ceiling ladder. "RP" — rating
	// pending — says nothing, and is left off rather than stored as a label
	// that blocks exactly as an absent one would.
	if g.ESRB != "" && g.ESRB != "RP" && !lockedSet[meta.FieldContentRating] {
		cr := "ESRB " + g.ESRB
		upd.ContentRating = &cr
	}
	if err := w.st.UpdateItemMetadata(ctx, rom.ID, upd); err != nil {
		return err
	}
	if g.Genre != "" && !lockedSet[meta.FieldGenres] {
		if err := w.st.ReplaceGenres(ctx, rom.ID, []string{g.Genre}); err != nil {
			return err
		}
	}
	if w.Art != nil && w.Artwork() && !lockedSet[meta.FieldArtwork] {
		w.fetchArt(ctx, rom.ID, g)
	}
	return nil
}

// fetchArt stores the box art as the poster and a screenshot as the fanart.
// A missing image is ordinary — the thumbnail sets do not cover every
// release — and is logged at debug, not as a failure.
func (w *Worker) fetchArt(ctx context.Context, itemID int64, g *retrodb.Game) {
	for _, a := range []struct{ set, kind string }{
		{retrodb.Boxart, string(meta.ArtPoster)},
		{retrodb.Snap, string(meta.ArtFanart)},
	} {
		u := retrodb.ThumbnailURL(g.Platform, a.set, g.Name)
		if u == "" {
			continue
		}
		hash, width, height, size, err := w.Art.Download(ctx, u)
		if err != nil {
			w.log.Debug("rom artwork not found", "item", itemID, "url", u, "error", err)
			continue
		}
		if err := w.st.PutArtwork(ctx, itemID, hash, a.kind, u, width, height, size); err != nil {
			w.log.Warn("rom artwork record failed", "item", itemID, "error", err)
		}
	}
}
