package photo

import (
	"context"
	"errors"
	"image"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/image/draw"

	"lancast/internal/store"
)

// displayMax bounds the long edge of what the artwork cache is given.
//
// The cache stores what it is handed as the "original" and derives smaller
// variants from it, so handing it a 24-megapixel photo would put a second full
// copy of the library on disk — 214 MB of pictures becoming 428 MB of pictures,
// and a 50,000-photo library becoming unusable. Full resolution is already on
// disk in the library; the viewer serves it from there.
//
// 1600 because the largest variant the cache generates is 1280 wide (fanart,
// which is what the library banner uses), and a source below that would be
// upscaled.
const displayMax = 1600

// Store is the persistence the worker needs.
type Store interface {
	PendingPhotos(ctx context.Context, limit int) ([]store.Item, error)
	PendingPhotoCount(ctx context.Context) (int, error)
	MarkArtworkChecked(ctx context.Context, itemID int64) error
	PutArtwork(ctx context.Context, itemID int64, hash, kind, sourceURL string, w, h int, size int64) error
	SetPhotoMeta(ctx context.Context, itemID int64, width, height int, takenAt int64) error
}

// Cache is the artwork side of the worker.
type Cache interface {
	Put(body []byte) (hash string, w, h int, size int64, err error)
}

// Stats is a snapshot of progress, shaped like every other worker's so the
// activity panel needs no special case.
type Stats struct {
	Running     bool `json:"running"`
	Done        int  `json:"done"`
	Failed      int  `json:"failed"`
	Unsupported int  `json:"unsupported"`
	Remaining   int  `json:"remaining"`
	Total       int  `json:"total"`
	UpdatedAt   int64
}

// Worker turns photos into thumbnails in the background.
//
// Its own worker rather than part of the scan, for the reason probing and cover
// art are: decoding and resizing a library of photographs is minutes of CPU,
// and a scan that did it inline would appear to hang on its first run. A scan
// reconciles files and finishes; thumbnails fill in behind it, visibly, in the
// activity panel.
type Worker struct {
	st  Store
	art Cache
	dec *Decoder
	log *slog.Logger

	// Concurrency defaults to half the CPUs. Unlike cover art this is real CPU
	// work rather than a process spawn, and saturating every core makes the
	// machine unpleasant to use while a first scan runs.
	Concurrency int
	BatchSize   int

	mu      sync.Mutex
	running bool
	stats   Stats
}

func NewWorker(st Store, art Cache, dec *Decoder, log *slog.Logger) *Worker {
	conc := runtime.NumCPU() / 2
	if conc < 1 {
		conc = 1
	}
	return &Worker{
		st: st, art: art, dec: dec, log: log,
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

// Run processes pending photos until the queue drains or ctx is done.
func (w *Worker) Run(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.stats = Stats{Running: true, UpdatedAt: time.Now().Unix()}
	w.mu.Unlock()

	if total, err := w.st.PendingPhotoCount(ctx); err == nil {
		w.mu.Lock()
		w.stats.Total, w.stats.Remaining = total, total
		w.mu.Unlock()
	}

	defer func() {
		remaining, err := w.st.PendingPhotoCount(context.WithoutCancel(ctx))
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

		photos, err := w.st.PendingPhotos(ctx, w.BatchSize)
		if err != nil {
			return err
		}
		if len(photos) == 0 {
			return nil
		}

		progressed := w.processBatch(ctx, photos)
		if err := ctx.Err(); err != nil {
			return err
		}

		// The same guard probing, enrichment and cover art use: the queue is a
		// query, not a cursor, so a batch that stamps nothing returns identical
		// rows forever. Every outcome stamps — including a file no decoder can
		// read — so this only trips if writes are failing.
		if progressed == 0 {
			w.log.Warn("photo thumbnails made no progress; stopping", "pending", len(photos))
			return nil
		}

		if remaining, err := w.st.PendingPhotoCount(ctx); err == nil {
			w.mu.Lock()
			w.stats.Remaining = remaining
			w.mu.Unlock()
		}
	}
}

func (w *Worker) processBatch(ctx context.Context, photos []store.Item) int {
	sem := make(chan struct{}, w.Concurrency)
	var wg sync.WaitGroup
	var progressed atomic.Int64

	for i := range photos {
		ph := photos[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			if w.one(ctx, ph) {
				progressed.Add(1)
			}
		}()
	}
	wg.Wait()
	return int(progressed.Load())
}

// one processes a single photo. It returns whether the row was stamped, which
// is what keeps the queue moving.
func (w *Worker) one(ctx context.Context, ph store.Item) bool {
	img, meta, err := w.dec.Read(ctx, ph.Path)
	if err != nil {
		// Both outcomes stamp. A file that cannot be read today will not read
		// differently tomorrow, and leaving it unstamped would hand the queue
		// the same row forever — the shape that stalled enrichment for every
		// music library until v0.6.1.
		if errors.Is(err, ErrUnsupported) {
			w.bump(func(s *Stats) { s.Unsupported++ })
			w.log.Info("no decoder for this picture", "path", ph.Path)
		} else {
			w.bump(func(s *Stats) { s.Failed++ })
			w.log.Warn("could not read picture", "path", ph.Path, "error", err)
		}
		_ = w.st.MarkArtworkChecked(ctx, ph.ID)
		return true
	}

	// Dimensions and capture time are recorded even when the thumbnail fails
	// below: they came from the same pass and they are useful on their own.
	if err := w.st.SetPhotoMeta(ctx, ph.ID, meta.Width, meta.Height, meta.TakenAt); err != nil {
		w.log.Warn("could not record picture metadata", "path", ph.Path, "error", err)
	}

	body, err := JPEG(Fit(Orient(img, meta.Orientation), displayMax))
	if err != nil {
		w.bump(func(s *Stats) { s.Failed++ })
		w.log.Warn("could not encode thumbnail", "path", ph.Path, "error", err)
		_ = w.st.MarkArtworkChecked(ctx, ph.ID)
		return true
	}

	hash, cw, chh, size, err := w.art.Put(body)
	if err != nil {
		w.bump(func(s *Stats) { s.Failed++ })
		w.log.Warn("could not cache thumbnail", "path", ph.Path, "error", err)
		_ = w.st.MarkArtworkChecked(ctx, ph.ID)
		return true
	}

	// Stored as the poster, which is what every grid tile already asks for. A
	// photo has one image and it plays every role — the tile, the banner, the
	// detail view — so a second artwork kind would be the same bytes under
	// another name.
	if err := w.st.PutArtwork(ctx, ph.ID, hash, "poster", "", cw, chh, size); err != nil {
		w.bump(func(s *Stats) { s.Failed++ })
		w.log.Warn("could not record thumbnail", "path", ph.Path, "error", err)
		_ = w.st.MarkArtworkChecked(ctx, ph.ID)
		return true
	}

	w.bump(func(s *Stats) { s.Done++ })
	_ = w.st.MarkArtworkChecked(ctx, ph.ID)
	return true
}

func (w *Worker) bump(fn func(*Stats)) {
	w.mu.Lock()
	fn(&w.stats)
	w.stats.UpdatedAt = time.Now().Unix()
	w.mu.Unlock()
}

// Fit scales an image down so its long edge is at most max, preserving aspect.
// An image already within bounds is returned untouched rather than re-encoded
// at the same size, which would cost quality for nothing.
func Fit(img image.Image, max int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= max && h <= max {
		return img
	}
	nw, nh := w, h
	if w >= h {
		nw = max
		nh = h * max / w
	} else {
		nh = max
		nw = w * max / h
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	// CatmullRom for the same reason the artwork cache uses a quality scaler:
	// this is the copy people look at, and a cheap filter on a photograph is
	// visible in a way it is not on a poster thumbnail.
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}
