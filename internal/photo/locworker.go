package photo

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"lancast/internal/geo"
	"lancast/internal/store"
)

// LocationStore is the persistence the location pass needs.
type LocationStore interface {
	PendingLocations(ctx context.Context, limit int) ([]store.Item, error)
	PendingLocationCount(ctx context.Context) (int, error)
	RecordPhotoLocation(ctx context.Context, itemID int64, at *store.LatLon, place *store.PhotoPlace) error
	ForgetPhotoLocations(ctx context.Context) (int64, error)
}

// LocationStats is a snapshot of progress, shaped like every other worker's.
type LocationStats struct {
	Running bool `json:"running"`
	Done    int  `json:"done"`
	// Located counts the photos read this pass that carried a position.
	Located   int `json:"located"`
	Remaining int `json:"remaining"`
	Total     int `json:"total"`
	UpdatedAt int64
	// FinishedAt is when a pass last ended having read something, so a
	// client can tell the places it is showing are stale.
	FinishedAt int64
}

/*
 * LocationWorker reads where photographs were taken and files each one under
 * a town (ADR 0078).
 *
 * Its own pass rather than a step in the thumbnail worker, for two reasons.
 * It must be able to run with nothing else to do: turning the setting on
 * should place a whole library already thumbnailed, without re-decoding it.
 * And it reads only the start of each file — EXIF is near the front — so a
 * pass over 3,073 photographs took about eleven seconds where a thumbnail
 * pass takes minutes.
 *
 * Off unless Enabled says otherwise, checked before every photograph so that
 * turning the setting off mid-pass stops it at once rather than at the end of
 * a batch.
 */
type LocationWorker struct {
	st      LocationStore
	log     *slog.Logger
	enabled func() bool
	// load is geo.Load, replaceable so tests need not decompress the world.
	load      func() (*geo.Gazetteer, error)
	read      func(path string) (Location, bool)
	BatchSize int

	// runMu is held for a whole pass. Forget takes it too, so a deletion
	// never interleaves with a pass that would write a row straight back.
	runMu sync.Mutex

	mu    sync.Mutex
	stats LocationStats
}

func NewLocationWorker(st LocationStore, enabled func() bool, log *slog.Logger) *LocationWorker {
	return &LocationWorker{
		st: st, log: log, enabled: enabled,
		load: geo.Load, read: ReadLocation,
		BatchSize: 200,
	}
}

// Stats returns current progress.
func (w *LocationWorker) Stats() LocationStats {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stats
}

// Run reads pending photos until the queue drains, the setting is turned
// off, or ctx is done.
func (w *LocationWorker) Run(ctx context.Context) error {
	if !w.enabled() {
		return nil
	}
	w.runMu.Lock()
	defer w.runMu.Unlock()

	total, err := w.st.PendingLocationCount(ctx)
	if err != nil || total == 0 {
		return err
	}
	/*
	 * Loaded per pass and dropped at the end of it. The tables decompress to
	 * a few tens of megabytes; a server that read its library's locations an
	 * hour ago has no reason to keep them, because the names it assigned are
	 * already in the database.
	 */
	g, err := w.load()
	if err != nil {
		return err
	}

	w.mu.Lock()
	w.stats = LocationStats{Running: true, Total: total, Remaining: total, UpdatedAt: time.Now().Unix()}
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.stats.Running = false
		w.stats.UpdatedAt = time.Now().Unix()
		if w.stats.Done > 0 {
			w.stats.FinishedAt = w.stats.UpdatedAt
		}
		w.mu.Unlock()
	}()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		photos, err := w.st.PendingLocations(ctx, w.BatchSize)
		if err != nil {
			return err
		}
		if len(photos) == 0 {
			return nil
		}
		progressed := 0
		for _, ph := range photos {
			if ctx.Err() != nil || !w.enabled() {
				return ctx.Err()
			}
			if w.one(ctx, g, ph) {
				progressed++
			}
		}
		// The queue is a query, not a cursor: a batch that stamps nothing
		// would return the same rows for ever. Every outcome stamps, so this
		// only trips if writes are failing.
		if progressed == 0 {
			w.log.Warn("photo location pass made no progress; stopping", "pending", len(photos))
			return nil
		}
	}
}

func (w *LocationWorker) one(ctx context.Context, g *geo.Gazetteer, ph store.Item) bool {
	var at *store.LatLon
	var place *store.PhotoPlace
	if loc, ok := w.read(ph.Path); ok {
		at = &store.LatLon{Lat: loc.Lat, Lon: loc.Lon}
		if p, _, ok := g.Nearest(loc.Lat, loc.Lon); ok {
			place = &store.PhotoPlace{
				ID: p.ID, Name: p.Name, Region: p.Region,
				CountryCode: p.CountryCode, Country: p.Country,
			}
		}
	}
	// A photo that cannot be opened today is stamped like one that says
	// nothing: it will read the same tomorrow, and a rescan that finds it
	// changed clears the stamp.
	if err := w.st.RecordPhotoLocation(ctx, ph.ID, at, place); err != nil {
		w.log.Warn("could not record where a photo was taken", "item", ph.ID, "error", err)
		return false
	}
	w.mu.Lock()
	w.stats.Done++
	if at != nil {
		w.stats.Located++
	}
	w.stats.Remaining = w.stats.Total - w.stats.Done
	w.stats.UpdatedAt = time.Now().Unix()
	w.mu.Unlock()
	return true
}

/*
 * Forget deletes every location and place, after any pass in flight has
 * stopped.
 *
 * Called when the setting is turned off, which the caller has already saved:
 * the pass checks Enabled before every photograph, so it returns within one
 * file, and only then is the table emptied. Deleting first would race a pass
 * that writes the next row straight back.
 */
func (w *LocationWorker) Forget(ctx context.Context) (int64, error) {
	w.runMu.Lock()
	defer w.runMu.Unlock()
	n, err := w.st.ForgetPhotoLocations(ctx)
	if err == nil {
		w.mu.Lock()
		w.stats = LocationStats{UpdatedAt: time.Now().Unix(), FinishedAt: time.Now().Unix()}
		w.mu.Unlock()
	}
	return n, err
}
