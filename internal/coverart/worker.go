package coverart

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"lancast/internal/store"
)

// Store is the persistence the worker needs.
type Store interface {
	PendingCoverArt(ctx context.Context, limit int) ([]store.Item, error)
	PendingCoverArtCount(ctx context.Context) (int, error)
	AlbumTrackPaths(ctx context.Context, albumID int64) ([]string, error)
	MarkCoverArtChecked(ctx context.Context, itemID int64) error
	PutArtwork(ctx context.Context, itemID int64, hash, kind, sourceURL string, w, h int, size int64) error
}

// Cache is the artwork side of the worker.
type Cache interface {
	Put(body []byte) (hash string, w, h int, size int64, err error)
}

// Stats is a snapshot of progress.
type Stats struct {
	Running   bool `json:"running"`
	Found     int  `json:"found"`
	None      int  `json:"none"`
	Failed    int  `json:"failed"`
	Remaining int  `json:"remaining"`
	Total     int  `json:"total"`
	UpdatedAt int64
}

// Worker finds album artwork in the background.
//
// Its own worker rather than part of the scan, for the reason probing is its
// own worker: extraction spawns a process per album, and a first scan of a
// large library should not wait on four hundred of them. A scan reconciles
// files and finishes; artwork fills in behind it.
//
// Separate from enrichment too, and for a sharper reason — enrichment is
// driven by remote providers, and ADR 0024 deliberately ships no music
// provider. An album would never be enriched at all, so artwork hung off
// enrichment would never run.
type Worker struct {
	st  Store
	art Cache
	res *Resolver
	log *slog.Logger

	// Concurrency defaults to half the CPUs. Extraction is a short ffmpeg
	// spawn that is mostly IO; saturating every core makes the machine
	// unpleasant to use while a first scan runs.
	Concurrency int
	BatchSize   int

	mu      sync.Mutex
	running bool
	stats   Stats
}

func NewWorker(st Store, art Cache, res *Resolver, log *slog.Logger) *Worker {
	conc := runtime.NumCPU() / 2
	if conc < 1 {
		conc = 1
	}
	return &Worker{
		st: st, art: art, res: res, log: log,
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

// Available reports whether embedded extraction can run. Sidecar covers are
// found regardless, so this being false does not mean the worker is idle.
func (w *Worker) Available() bool { return w.res.Available() }

// Run processes pending albums until the queue drains or ctx is done.
func (w *Worker) Run(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.stats = Stats{Running: true, UpdatedAt: time.Now().Unix()}
	w.mu.Unlock()

	if !w.res.Available() {
		// Not a reason to stop: a library that keeps cover.jpg beside the music
		// still gets covers without ffmpeg. Said once, rather than per album.
		w.log.Info("ffmpeg not found; album art will come from sidecar files only")
	}

	if total, err := w.st.PendingCoverArtCount(ctx); err == nil {
		w.mu.Lock()
		w.stats.Total, w.stats.Remaining = total, total
		w.mu.Unlock()
	}

	defer func() {
		remaining, err := w.st.PendingCoverArtCount(context.WithoutCancel(ctx))
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

		albums, err := w.st.PendingCoverArt(ctx, w.BatchSize)
		if err != nil {
			return err
		}
		if len(albums) == 0 {
			return nil
		}

		progressed := w.processBatch(ctx, albums)
		if err := ctx.Err(); err != nil {
			return err
		}

		// The same guard probing and enrichment use: the queue is a query, not
		// a cursor, so a batch that stamps nothing returns identical rows
		// forever. Every outcome including "found nothing" stamps, so this only
		// trips if writes are failing.
		if progressed == 0 {
			w.log.Warn("album art made no progress; stopping", "pending", len(albums))
			return nil
		}

		if remaining, err := w.st.PendingCoverArtCount(ctx); err == nil {
			w.mu.Lock()
			w.stats.Remaining = remaining
			w.mu.Unlock()
		}
	}
}

func (w *Worker) processBatch(ctx context.Context, albums []store.Item) int {
	sem := make(chan struct{}, w.Concurrency)
	var wg sync.WaitGroup
	var progressed atomic.Int64

	for i := range albums {
		album := albums[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			if w.processAlbum(ctx, album) {
				progressed.Add(1)
			}
		}()
	}
	wg.Wait()
	return int(progressed.Load())
}

// processAlbum resolves and stores one album's cover, reporting whether it made
// progress — which means "left a mark", not "found a picture".
func (w *Worker) processAlbum(ctx context.Context, album store.Item) bool {
	paths, err := w.st.AlbumTrackPaths(ctx, album.ID)
	if err != nil {
		w.log.Warn("album tracks unreadable", "album", album.ID, "error", err)
		return false
	}

	img, err := w.res.ForAlbum(ctx, paths)
	switch {
	case errors.Is(err, ErrNoArt):
		// The ordinary outcome for a lot of albums. Stamped so it is not asked
		// again, counted so the total is honest, and logged at debug because a
		// warning per artless album would bury everything else.
		w.log.Debug("no album art found", "album", album.ID, "title", album.Title)
		w.mu.Lock()
		w.stats.None++
		w.mu.Unlock()
		return w.markChecked(ctx, album.ID)

	case err != nil:
		if ctx.Err() != nil {
			return false
		}
		w.log.Warn("album art lookup failed", "album", album.ID, "title", album.Title, "error", err)
		w.mu.Lock()
		w.stats.Failed++
		w.mu.Unlock()
		// Stamped anyway. Leaving it pending means one unreadable album is
		// retried on every pass forever and the queue never drains.
		return w.markChecked(ctx, album.ID)
	}

	hash, width, height, size, err := w.art.Put(img.Bytes)
	if err != nil {
		w.log.Warn("album art not storable", "album", album.ID,
			"from", img.From, "error", err)
		w.mu.Lock()
		w.stats.Failed++
		w.mu.Unlock()
		return w.markChecked(ctx, album.ID)
	}

	// Recorded as a poster: an album cover occupies the same slot in the grid a
	// film's poster does, so the client needs no new artwork kind to render it.
	// source_url is empty because there is no URL — the image came off the
	// disk, and inventing a file:// one would put a local path into a column
	// the API hands to clients.
	if err := w.st.PutArtwork(ctx, album.ID, hash, "poster", "", width, height, size); err != nil {
		w.log.Warn("album art record failed", "album", album.ID, "error", err)
		w.mu.Lock()
		w.stats.Failed++
		w.mu.Unlock()
		return false
	}

	w.log.Debug("album art found", "album", album.ID, "title", album.Title,
		"source", img.Source, "from", img.From)
	w.mu.Lock()
	w.stats.Found++
	w.stats.UpdatedAt = time.Now().Unix()
	w.mu.Unlock()
	return w.markChecked(ctx, album.ID)
}

func (w *Worker) markChecked(ctx context.Context, id int64) bool {
	if err := w.st.MarkCoverArtChecked(ctx, id); err != nil {
		w.log.Warn("marking album checked failed", "album", id, "error", err)
		return false
	}
	return true
}
