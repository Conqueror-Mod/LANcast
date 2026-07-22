// Package scan reconciles a library's database rows against the filesystem.
package scan

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"lancast/internal/media"
	"lancast/internal/store"
)

// State is the lifecycle of one library's scan.
type State string

const (
	StateIdle    State = "idle"
	StateRunning State = "running"
	StateFailed  State = "failed"
)

// Progress is a snapshot of a scan, safe to serialize.
type Progress struct {
	LibraryID    int64  `json:"library_id"`
	State        State  `json:"state"`
	FilesSeen    int    `json:"files_seen"`
	ItemsChanged int    `json:"items_changed"`
	ItemsMissing int    `json:"items_missing"`
	StartedAt    int64  `json:"started_at"`
	FinishedAt   *int64 `json:"finished_at,omitempty"`
	Error        string `json:"error,omitempty"`
}

// ErrBusy is returned when a scan is already running for a library.
var ErrBusy = fmt.Errorf("scan already running")

// Scanner runs library scans, at most one per library at a time.
type Scanner struct {
	st  *store.Store
	log *slog.Logger

	mu      sync.Mutex
	running map[int64]bool
	last    map[int64]*Progress
}

func New(st *store.Store, log *slog.Logger) *Scanner {
	return &Scanner{
		st:      st,
		log:     log,
		running: map[int64]bool{},
		last:    map[int64]*Progress{},
	}
}

// Status returns the most recent progress for a library.
func (s *Scanner) Status(libraryID int64) Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.last[libraryID]; ok {
		c := *p
		return c
	}
	return Progress{LibraryID: libraryID, State: StateIdle}
}

// Start begins an asynchronous scan. It returns ErrBusy if one is already
// running for this library — scans are not queued, because a queued duplicate
// scan does no useful work the running one isn't already doing.
func (s *Scanner) Start(lib store.Library) (Progress, error) {
	s.mu.Lock()
	if s.running[lib.ID] {
		p := *s.last[lib.ID]
		s.mu.Unlock()
		return p, ErrBusy
	}
	s.running[lib.ID] = true
	p := &Progress{LibraryID: lib.ID, State: StateRunning, StartedAt: time.Now().Unix()}
	s.last[lib.ID] = p
	s.mu.Unlock()

	go s.run(lib, p)
	return *p, nil
}

func (s *Scanner) run(lib store.Library, p *Progress) {
	// Detached from any request context: a scan must outlive the HTTP call
	// that triggered it.
	ctx := context.Background()

	err := s.walk(ctx, lib, p)

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	p.FinishedAt = &now
	if err != nil {
		p.State = StateFailed
		p.Error = err.Error()
		s.log.Error("scan failed", "library", lib.ID, "error", err)
	} else {
		p.State = StateIdle
		s.log.Info("scan complete", "library", lib.ID,
			"seen", p.FilesSeen, "changed", p.ItemsChanged, "missing", p.ItemsMissing)
	}
	delete(s.running, lib.ID)
}

func (s *Scanner) walk(ctx context.Context, lib store.Library, p *Progress) error {
	known, err := s.st.KnownFiles(ctx, lib.ID)
	if err != nil {
		return err
	}

	seen := make(map[string]bool, len(known))

	err = filepath.WalkDir(lib.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A single unreadable directory shouldn't abort the whole scan.
			s.log.Warn("skipping unreadable path", "path", path, "error", err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !media.IsVideo(path) {
			return nil
		}

		seen[path] = true
		s.mu.Lock()
		p.FilesSeen++
		s.mu.Unlock()

		info, err := d.Info()
		if err != nil {
			s.log.Warn("stat failed", "path", path, "error", err)
			return nil
		}

		// Skip unchanged files without re-parsing. This is what makes rescans
		// cheap on a large library.
		//
		// A row currently flagged missing is never skipped, however identical
		// the file looks: the upsert is what clears the flag, and a file that
		// comes back byte-identical is the normal case, not the rare one.
		if st, ok := known[path]; ok && !st.Missing &&
			st.SizeBytes != nil && *st.SizeBytes == info.Size() &&
			st.MTime != nil && *st.MTime == info.ModTime().Unix() {
			return nil
		}

		if err := s.upsert(ctx, lib, path, info); err != nil {
			s.log.Warn("upsert failed", "path", path, "error", err)
			return nil
		}
		s.mu.Lock()
		p.ItemsChanged++
		s.mu.Unlock()
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk %q: %w", lib.Path, err)
	}

	// Anything previously known but not seen this pass is marked missing —
	// never deleted. An unmounted drive must not destroy library data.
	var gone []int64
	for path, st := range known {
		if !seen[path] {
			gone = append(gone, st.ID)
		}
	}
	if err := s.st.MarkMissing(ctx, gone); err != nil {
		return err
	}
	s.mu.Lock()
	p.ItemsMissing = len(gone)
	s.mu.Unlock()

	return s.st.TouchLibraryScanned(ctx, lib.ID)
}

func (s *Scanner) upsert(ctx context.Context, lib store.Library, path string, info fs.FileInfo) error {
	nfo := media.Parse(lib.Path, path)

	f := store.ScanFile{
		LibraryID: lib.ID,
		Path:      path,
		Kind:      string(nfo.Kind),
		Title:     nfo.Title,
		SortTitle: media.SortTitle(nfo.Title),
		Container: trimDot(filepath.Ext(path)),
		SizeBytes: info.Size(),
		MTime:     info.ModTime().Unix(),
	}
	if nfo.Year != 0 {
		y := nfo.Year
		f.Year = &y
	}
	if nfo.Series != "" {
		sr := nfo.Series
		f.Series = &sr
		// Episodes sort under their series, not their own episode title.
		f.SortTitle = media.SortTitle(sr)
	}
	if nfo.Kind == media.KindEpisode {
		se, ep := nfo.Season, nfo.Episode
		f.Season, f.Episode = &se, &ep
	}

	_, err := s.st.UpsertItem(ctx, f)
	return err
}

func trimDot(ext string) string {
	if len(ext) > 0 && ext[0] == '.' {
		return ext[1:]
	}
	return ext
}
