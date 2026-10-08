package retrodb

import (
	"log/slog"
	"sync"
)

/*
 * Cache holds the loaded index for the life of the process.
 *
 * Loaded lazily, on the first pass that wants it, rather than at startup: a
 * server with no retro library should not spend a fifth of a second and
 * fifteen megabytes on DATs nothing will consult. Forget drops it after an
 * install, so the next pass loads what was just fetched without a restart.
 */
type Cache struct {
	Dir string
	Log *slog.Logger

	mu     sync.Mutex
	ix     *Index
	loaded bool
}

// Index returns the installed index, or nil when none is installed or it
// failed to load. A failure is logged once and not retried until Forget,
// because a DAT that did not parse will not parse on the next pass either.
func (c *Cache) Index() *Index {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded {
		return c.ix
	}
	c.loaded = true
	if !Installed(c.Dir) {
		return nil
	}
	ix, err := Load(c.Dir)
	if err != nil {
		if c.Log != nil {
			c.Log.Warn("rom database did not load", "dir", c.Dir, "error", err)
		}
		return nil
	}
	c.ix = ix
	return ix
}

// Preloaded is a Cache already holding ix, for a caller that built its own
// index — tests of what consults one, without DAT files on disk.
func Preloaded(ix *Index) *Cache {
	return &Cache{ix: ix, loaded: true}
}

// Forget drops the loaded index so the next call reads the directory again.
func (c *Cache) Forget() {
	c.mu.Lock()
	c.ix, c.loaded = nil, false
	c.mu.Unlock()
}
