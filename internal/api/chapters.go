package api

import (
	"net/http"
	"os"
	"sync"
	"time"

	"lancast/internal/probe"
)

/*
 * GET /api/items/{id}/chapters: the marks a film's file carries, for the scrub
 * bar and the chapter keys.
 *
 * Read with ffprobe on first ask (probe.ReadChapters) and kept in memory for
 * as long as the file's size and modification time are unchanged, so opening
 * the same film twice runs ffprobe once. Nothing is stored in the database.
 *
 * Every failure is an empty list, not an error: a file without chapters, a
 * server without ffprobe, a container row with no file, and a read that timed
 * out all leave the player exactly as it was before chapters existed.
 */

type chapterList struct {
	Chapters []probe.Chapter `json:"chapters"`
}

type chapterCacheEntry struct {
	size     int64
	mod      time.Time
	chapters []probe.Chapter
}

// chapterCache is bounded: a household opens far fewer films in a session than
// this, and an unbounded map keyed by item would grow for as long as the
// server runs.
type chapterCache struct {
	mu      sync.Mutex
	entries map[int64]chapterCacheEntry
}

const chapterCacheMax = 512

func (c *chapterCache) get(id int64, fi os.FileInfo) ([]probe.Chapter, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	if !ok || e.size != fi.Size() || !e.mod.Equal(fi.ModTime()) {
		return nil, false
	}
	return e.chapters, true
}

func (c *chapterCache) put(id int64, fi os.FileInfo, chapters []probe.Chapter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[int64]chapterCacheEntry{}
	}
	if len(c.entries) >= chapterCacheMax {
		// Crude, and enough: drop everything and start again, rather than
		// tracking ages for a cache whose misses cost one container read.
		c.entries = map[int64]chapterCacheEntry{}
	}
	c.entries[id] = chapterCacheEntry{size: fi.Size(), mod: fi.ModTime(), chapters: chapters}
}

func (s *Server) itemChapters(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	// GetItem applies the account's library and rating limits, so a film the
	// caller may not see is a 404 here as everywhere else.
	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	none := chapterList{Chapters: []probe.Chapter{}}
	if it.Path == "" || s.prober == nil || !s.prober.Available() {
		writeJSON(w, http.StatusOK, none)
		return
	}
	path, err := s.itemFilePath(r, it)
	if err != nil {
		s.log.Error("chapters containment check failed", "item", id, "path", it.Path, "error", err)
		writeError(w, http.StatusNotFound, "not_found", "no such item")
		return
	}
	fi, err := os.Stat(path)
	if err != nil {
		writeJSON(w, http.StatusOK, none)
		return
	}
	if cached, ok := s.chapters.get(id, fi); ok {
		writeJSON(w, http.StatusOK, chapterList{Chapters: cached})
		return
	}
	chapters, err := s.prober.ReadChapters(r.Context(), path)
	if err != nil {
		// Logged at debug: a file ffprobe cannot read has already said so
		// louder through the probe worker, and this is a nicety on top.
		s.log.Debug("read chapters", "item", id, "error", err)
		writeJSON(w, http.StatusOK, none)
		return
	}
	s.chapters.put(id, fi, chapters)
	writeJSON(w, http.StatusOK, chapterList{Chapters: chapters})
}
