package comic

import (
	"bytes"
	"context"
	"image"
	_ "image/gif"  // pages may be GIF
	_ "image/jpeg" // or JPEG
	_ "image/png"  // or PNG
	"log/slog"
	"sync"

	_ "golang.org/x/image/webp" // or WebP

	"lancast/internal/photo"
	"lancast/internal/store"
)

/*
 * A comic's cover is its first page (ADR 0101), made into a poster the way a
 * photo's thumbnail is: decoded, fitted, re-encoded and stored in the artwork
 * cache as the item's poster, which is what every grid tile already asks for.
 * A cover that cannot be made still stamps the row, so a broken archive is
 * tried once rather than on every pass.
 */

// coverMax is the longest side of a cover. A tile is a few hundred pixels
// wide; a detail page's poster is not much larger.
const coverMax = 900

// Store is what the cover worker needs from the store.
type Store interface {
	PendingComicCovers(ctx context.Context, limit int) ([]store.Item, error)
	PutArtwork(ctx context.Context, itemID int64, hash, kind, sourceURL string, w, h int, size int64) error
	MarkArtworkChecked(ctx context.Context, itemID int64) error
}

// Cache is the artwork cache.
type Cache interface {
	Put(body []byte) (hash string, w, h int, size int64, err error)
}

// CoverWorker makes covers for comics that have none.
type CoverWorker struct {
	st  Store
	art Cache
	log *slog.Logger

	mu      sync.Mutex
	running bool
}

// NewCoverWorker makes the worker.
func NewCoverWorker(st Store, art Cache, log *slog.Logger) *CoverWorker {
	return &CoverWorker{st: st, art: art, log: log}
}

// Run makes covers until none are pending or ctx is done. A second call while
// one is running returns at once.
func (w *CoverWorker) Run(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.running = false
		w.mu.Unlock()
	}()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		items, err := w.st.PendingComicCovers(ctx, 50)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		for _, it := range items {
			if err := ctx.Err(); err != nil {
				return err
			}
			w.one(ctx, it)
		}
	}
}

func (w *CoverWorker) one(ctx context.Context, it store.Item) {
	defer func() { _ = w.st.MarkArtworkChecked(ctx, it.ID) }()
	body, err := Cover(it.Path)
	if err != nil {
		w.log.Warn("could not make a comic's cover", "path", it.Path, "error", err)
		return
	}
	hash, cw, ch, size, err := w.art.Put(body)
	if err != nil {
		w.log.Warn("could not cache a comic's cover", "path", it.Path, "error", err)
		return
	}
	if err := w.st.PutArtwork(ctx, it.ID, hash, "poster", "", cw, ch, size); err != nil {
		w.log.Warn("could not record a comic's cover", "path", it.Path, "error", err)
	}
}

// Cover is the first page of the archive at path, fitted and as JPEG.
func Cover(path string) ([]byte, error) {
	zr, err := Open(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	pages, err := Pages(&zr.Reader)
	if err != nil {
		return nil, err
	}
	raw, err := ReadPage(pages[0])
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	return photo.JPEG(photo.Fit(img, coverMax))
}
