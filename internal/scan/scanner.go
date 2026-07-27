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
	"lancast/internal/subtitle"
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
	LibraryID    int64   `json:"library_id"`
	State        State   `json:"state"`
	FilesSeen    int     `json:"files_seen"`
	ItemsChanged int     `json:"items_changed"`
	ItemsMissing int     `json:"items_missing"`
	Skipped      int     `json:"skipped"`
	Issues       []Issue `json:"issues,omitempty"`
	StartedAt    int64   `json:"started_at"`
	FinishedAt   *int64  `json:"finished_at,omitempty"`
	Error        string  `json:"error,omitempty"`
}

// Issue is a file or directory the scan could not fully process. It carries a
// library-relative path — never the absolute server path, which the API keeps
// private for the same reason it withholds item paths.
type Issue struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// maxIssues caps the recorded list so a pathological library (thousands of
// unreadable files) cannot grow scan status without bound. The count keeps
// counting past the cap.
const maxIssues = 50

// ErrBusy is returned when a scan is already running for a library.
var ErrBusy = fmt.Errorf("scan already running")

// Scanner runs library scans, at most one per library at a time.
type Scanner struct {
	st  *store.Store
	log *slog.Logger

	mu       sync.Mutex
	running  map[int64]bool
	last     map[int64]*Progress
	onFinish func()
}

func New(st *store.Store, log *slog.Logger) *Scanner {
	return &Scanner{
		st:      st,
		log:     log,
		running: map[int64]bool{},
		last:    map[int64]*Progress{},
	}
}

// OnFinish registers a callback invoked after every completed scan.
//
// Scanning produces pending items and enrichment consumes them, so without
// this a fresh scan sits unenriched until the next restart or manual refresh —
// metadata appears to simply not work.
func (s *Scanner) OnFinish(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onFinish = fn
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
	done := s.onFinish
	s.mu.Unlock()

	// Called outside the lock: the callback starts enrichment, which reads
	// scan status, and holding the mutex here would deadlock.
	if done != nil {
		done()
	}
}

// recordIssue notes a file the scan could not process, under the same lock that
// guards the rest of Progress. The path is made library-relative before it is
// stored so the absolute server layout never leaves the machine.
func (s *Scanner) recordIssue(p *Progress, root, path, reason string) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "" {
		rel = filepath.Base(path)
	}
	s.mu.Lock()
	p.Skipped++
	if len(p.Issues) < maxIssues {
		p.Issues = append(p.Issues, Issue{Path: rel, Reason: reason})
	}
	s.mu.Unlock()
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
			s.recordIssue(p, lib.Path, path, "unreadable")
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
			s.recordIssue(p, lib.Path, path, "could not read file info")
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
			// Subtitles are still re-checked. A sidecar dropped next to an
			// untouched film is the common way they arrive, and skipping the
			// video would otherwise mean it is never noticed.
			s.syncSubtitles(ctx, st.ID, path)
			return nil
		}

		id, err := s.upsert(ctx, lib, path, info)
		if err != nil {
			s.log.Warn("upsert failed", "path", path, "error", err)
			s.recordIssue(p, lib.Path, path, "could not be recorded")
			return nil
		}
		s.syncSubtitles(ctx, id, path)
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

	if err := s.reconcileHierarchy(ctx, lib); err != nil {
		// Hierarchy is a grouping convenience layered over episodes that already
		// exist and play; a failure here must not fail the whole scan.
		s.log.Warn("hierarchy reconciliation failed", "library", lib.ID, "error", err)
	}
	// Multi-part films first, then serials: both re-parse the movie files, and a
	// file promoted to a part or chapter drops out of the second pass's query.
	if err := s.reconcileParts(ctx, lib); err != nil {
		s.log.Warn("multi-part reconciliation failed", "library", lib.ID, "error", err)
	}
	if err := s.reconcileSerials(ctx, lib); err != nil {
		s.log.Warn("serial reconciliation failed", "library", lib.ID, "error", err)
	}

	return s.st.TouchLibraryScanned(ctx, lib.ID)
}

// reconcileHierarchy builds the show → season → episode structure ADR 0010
// describes: every episode is nested under a season media_item, itself under a
// show media_item, with the relationship held in parent_id. It runs over every
// episode each scan (not only new ones) so a library re-organised on disk, or
// one scanned before this existed, is brought into shape without a rescan of
// unchanged files.
//
// Shows and seasons are keyed by their directory paths, which the ensure calls
// make idempotent, so re-running is cheap and safe. A show whose episodes sit
// loose in the show folder still gets a season, under a synthetic identity
// derived from the show and season number.
func (s *Scanner) reconcileHierarchy(ctx context.Context, lib store.Library) error {
	episodes, err := s.st.LibraryEpisodes(ctx, lib.ID)
	if err != nil {
		return err
	}
	shows := map[string]int64{}   // show dir  -> show id
	seasons := map[string]int64{} // season key -> season id

	for _, ep := range episodes {
		showDir := media.ShowDir(lib.Path, ep.Path)
		if showDir == "" {
			// An episode sitting directly in the library root is not a show
			// layout; leave it top-level rather than invent a show for it.
			continue
		}

		showID, ok := shows[showDir]
		if !ok {
			title := deref(ep.Series)
			if title == "" {
				title = filepath.Base(showDir)
			}
			id, _, err := s.st.EnsureShow(ctx, lib.ID, showDir, title, media.SortTitle(title))
			if err != nil {
				return err
			}
			shows[showDir] = id
			showID = id
		}

		seasonNum := deref2(ep.Season)
		seasonPath := media.SeasonDir(ep.Path)
		if seasonPath == "" {
			// No "Season N" folder: synthesize a stable identity under the show.
			seasonPath = fmt.Sprintf("%s::season=%d", showDir, seasonNum)
		}
		seasonID, ok := seasons[seasonPath]
		if !ok {
			title := fmt.Sprintf("Season %d", seasonNum)
			if seasonNum == 0 {
				title = "Specials"
			}
			id, _, err := s.st.EnsureSeason(ctx, lib.ID, showID, seasonNum, seasonPath, title, media.SortTitle(title))
			if err != nil {
				return err
			}
			seasons[seasonPath] = id
			seasonID = id
		}

		if ep.ParentID == nil || *ep.ParentID != seasonID {
			if err := s.st.SetParent(ctx, ep.ID, &seasonID); err != nil {
				return err
			}
		}
	}
	return nil
}

// reconcileParts groups multi-part films — "Baahubali Part 1/2" — under a single
// 'movie' work with 'part' children (ADR 0017).
func (s *Scanner) reconcileParts(ctx context.Context, lib store.Library) error {
	return s.reconcileGrouped(ctx, lib, media.PartOf, s.st.EnsureWork, "part")
}

// reconcileSerials groups the chapters of a theatrical serial or miniseries —
// "Batman Chapter 1..15" — under a 'serial' container with 'chapter' children.
func (s *Scanner) reconcileSerials(ctx context.Context, lib store.Library) error {
	return s.reconcileGrouped(ctx, lib, media.ChapterOf, s.st.EnsureSerial, "chapter")
}

// reconcileGrouped is the shared body: re-parse the movie files, group those
// that share a work title in the same directory, and — only when two or more
// agree, which is what keeps a standalone film with "Part" in its name intact —
// create the container and fold its members in as ordered children.
//
// Grouping is scoped to a directory so two unrelated "Part 1" films in different
// folders never merge. Idempotent across rescans: the ensure and promote calls
// re-apply the same values.
func (s *Scanner) reconcileGrouped(
	ctx context.Context,
	lib store.Library,
	detect func(path string) (string, int, bool),
	ensure func(ctx context.Context, libraryID int64, workKey, title, sortTitle string) (int64, bool, error),
	childKind string,
) error {
	movies, err := s.st.LibraryMovieFiles(ctx, lib.ID)
	if err != nil {
		return err
	}

	type member struct {
		item  store.Item
		order int
	}
	groups := map[string][]member{}
	titles := map[string]string{}
	for _, m := range movies {
		work, order, ok := detect(m.Path)
		if !ok {
			continue
		}
		key := filepath.Dir(m.Path) + "|" + media.SortTitle(work)
		groups[key] = append(groups[key], member{item: m, order: order})
		titles[key] = work
	}

	for key, members := range groups {
		if len(members) < 2 {
			continue
		}
		work := titles[key]
		parentID, _, err := ensure(ctx, lib.ID, key, work, media.SortTitle(work))
		if err != nil {
			return err
		}
		for _, mem := range members {
			if err := s.st.PromoteToChild(ctx, mem.item.ID, parentID, childKind, mem.order); err != nil {
				return err
			}
		}
	}
	return nil
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func deref2(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// syncSubtitles records the sidecar files sitting beside a video.
//
// Best-effort: a subtitle that cannot be indexed must not fail a scan, and a
// library on a read-only mount is a normal deployment.
func (s *Scanner) syncSubtitles(ctx context.Context, itemID int64, videoPath string) {
	found := subtitle.FindSidecars(videoPath)
	if len(found) == 0 {
		// Still call through, so a sidecar that was deleted stops being listed.
		if err := s.st.ReplaceSidecarSubtitles(ctx, itemID, nil); err != nil {
			s.log.Debug("clearing sidecars failed", "item", itemID, "error", err)
		}
		return
	}

	subs := make([]store.ExternalSubtitle, 0, len(found))
	for _, f := range found {
		subs = append(subs, store.ExternalSubtitle{
			ItemID: itemID, Path: f.Path, Language: f.Language,
			Title: f.Title, Forced: f.Forced, Format: f.Format,
		})
	}
	if err := s.st.ReplaceSidecarSubtitles(ctx, itemID, subs); err != nil {
		s.log.Warn("indexing sidecars failed", "item", itemID, "error", err)
	}
}

func (s *Scanner) upsert(ctx context.Context, lib store.Library, path string, info fs.FileInfo) (int64, error) {
	nfo := media.Parse(lib.Path, path, lib.Kind)

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

	return s.st.UpsertItem(ctx, f)
}

func trimDot(ext string) string {
	if len(ext) > 0 && ext[0] == '.' {
		return ext[1:]
	}
	return ext
}
