package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"lancast/internal/diskuse"
)

/*
 * GET /api/storage: what LANcast keeps on disk, part by part (settings-3-plan
 * step 7). Admin only, and read-only — nothing here offers to delete.
 *
 * Measuring is a walk of the data folder, and the artwork cache alone is tens
 * of thousands of files, so it never runs inside a request. A GET answers
 * with the last measurement at once and, when that is missing or older than
 * storageFresh, starts a new one in the background; `measuring` says one is
 * running, and the client asks again until it is not. `?fresh=1` measures
 * again now, for the person who has just freed some space and wants to see it.
 *
 * One measurement at a time, however many windows are asking.
 */

// storageFresh is how old a measurement may be before a GET starts another.
const storageFresh = 10 * time.Minute

// storageLimit bounds one walk. A data folder that takes longer than this is
// reported as far as it got, marked partial, rather than walked for ever.
const storageLimit = 5 * time.Minute

type storageState struct {
	mu        sync.Mutex
	last      *diskuse.Report
	measuring bool
	// measure replaces the walk in tests.
	measure func(context.Context, diskuse.Layout) diskuse.Report
}

// storageLayout says which part each top-level entry of the data folder is.
// Anything not named here is "other", and is listed by name.
func (s *Server) storageLayout() diskuse.Layout {
	return diskuse.Layout{
		DataDir: s.dataDir,
		ByName: map[string]string{
			"lancast.db": "database", "lancast.db-wal": "database", "lancast.db-shm": "database",
			"artwork":   "artwork",
			"previews":  "previews",
			"optimized": "optimized",
			"backups":   "backups",
			"transcode": "transcode",
			"subtitles": "subtitles",
			"faces":     "models",
			"saves":     "saves",
			// Fetched from elsewhere and fetched again if removed.
			"tools": "downloads", "webcores": "downloads", "retrodb": "downloads",
			"plugins": "downloads", "visualizers": "downloads", "cache": "downloads",
		},
		ByPrefix: map[string]string{"lancastd": "logs"},
		Order: []string{
			"database", "artwork", "previews", "optimized", "backups", "transcode",
			"subtitles", "models", "saves", "downloads", "logs",
		},
		// Optimized copies may be kept on another drive (ADR 0087).
		Elsewhere: map[string]string{"optimized": s.optimizedDir()},
	}
}

func (s *Server) storageOverview(w http.ResponseWriter, r *http.Request) {
	st := &s.storage
	st.mu.Lock()
	stale := st.last == nil || time.Since(time.Unix(st.last.MeasuredAt, 0)) > storageFresh ||
		r.URL.Query().Get("fresh") == "1"
	if stale && !st.measuring && s.dataDir != "" {
		st.measuring = true
		measure := st.measure
		if measure == nil {
			measure = diskuse.Measure
		}
		layout := s.storageLayout()
		go func() {
			// Detached from the request: a window closed mid-walk must not
			// leave the next one waiting on a measurement nobody finishes.
			ctx, cancel := context.WithTimeout(context.Background(), storageLimit)
			defer cancel()
			rep := measure(ctx, layout)
			st.mu.Lock()
			st.last, st.measuring = &rep, false
			st.mu.Unlock()
			s.log.Info("storage measured", "bytes", rep.TotalBytes, "took_ms", rep.TookMS, "partial", rep.Partial)
		}()
	}
	out := map[string]any{"measuring": st.measuring, "report": st.last}
	st.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
}
