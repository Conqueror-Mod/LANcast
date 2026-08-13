// Package scan reconciles a library's database rows against the filesystem.
package scan

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
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
	LibraryID    int64 `json:"library_id"`
	State        State `json:"state"`
	FilesSeen    int   `json:"files_seen"`
	ItemsChanged int   `json:"items_changed"`
	ItemsMissing int   `json:"items_missing"`
	Skipped      int   `json:"skipped"`
	// Media the library's own kind excludes: audio in a video library, video in
	// a music library. Kept apart from Skipped because it is not a failure —
	// nothing went wrong reading these files — but it is the one number that
	// explains an empty library, and silence here is what let a music library
	// created as a movie library report "0 items · scanned" over 1,592 tracks.
	SkippedKind int `json:"skipped_kind"`
	// EpisodesInMovieLibrary counts files in a `movie` library that parsed as
	// episodes — S01E02, 1x02, and the rest.
	//
	// The other half of the same warning. A music library created as a movie
	// library says so, because its audio is discarded outright and the count is
	// the only thing that explains an empty library. A *shows* library created
	// as a movie library imports everything and looks fine: every episode
	// becomes a film, sitting loose in the grid with no series and no seasons,
	// and nothing anywhere says why. Kind cannot be changed (it decides which
	// scanner runs and biases matching), so the mistake is unrecoverable except
	// by removing and re-adding the library — which makes being loud at the
	// moment it happens the only defence there is.
	EpisodesInMovieLibrary int `json:"episodes_in_movie_library"`
	// Playlists imported from .m3u files found in the library (ADR 0030).
	// Reported for the same reason SkippedKind is: a scan that quietly imported
	// nothing, or quietly imported forty, should not have to be inferred from
	// the library page afterwards.
	PlaylistsImported int     `json:"playlists_imported"`
	Issues            []Issue `json:"issues,omitempty"`
	StartedAt         int64   `json:"started_at"`
	FinishedAt        *int64  `json:"finished_at,omitempty"`
	Error             string  `json:"error,omitempty"`
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

// ErrRootUnavailable is returned when a library's root is not a directory this
// process can see — an unmounted drive, a disconnected share, a deleted folder.
//
// It is deliberately distinct from any other scan failure, because the correct
// response to it is to change *nothing*. See checkRoot.
var ErrRootUnavailable = fmt.Errorf("library root is unavailable")

// Scanner runs library scans, at most one per library at a time.
type Scanner struct {
	st  *store.Store
	log *slog.Logger

	mu       sync.Mutex
	running  map[int64]bool
	last     map[int64]*Progress
	onFinish func()
	tags     TagReader
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

/*
 * checkRoot reports whether a library's root is a directory we can currently
 * see.
 *
 * This exists because of how filepath.WalkDir fails, which is not how it looks
 * like it fails. Given a root that does not exist it calls the walk function
 * once with a nil DirEntry and an error, and then **returns nil** — a scan that
 * reports complete success having seen zero files. The handler below cannot
 * rescue it either: with a nil DirEntry there is nothing to answer SkipDir
 * about, so it logs the unreadable path and carries on.
 *
 * A successful walk that saw nothing is indistinguishable, further down, from a
 * library whose every file was deleted. So reconciliation marked the entire
 * library missing the first time anyone scanned with a drive unplugged — under
 * a comment promising that an unmounted drive must not destroy library data.
 * Nothing was deleted, so the letter of that rule held while the outcome it
 * exists to prevent happened anyway.
 *
 * The fix is to never let reconciliation run against an absent root, rather
 * than to make the walk report harder — an empty result is a legitimate answer
 * for an empty directory, and only the root's own existence tells the two
 * apart.
 */
func checkRoot(root string) error {
	st, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrRootUnavailable, root)
	}
	// A root that has become a *file* is as unusable as one that is absent, and
	// rather more alarming: WalkDir would happily walk it as a single entry and
	// reconciliation would mark every real item missing against it.
	if !st.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrRootUnavailable, root)
	}
	return nil
}

func (s *Scanner) walk(ctx context.Context, lib store.Library, p *Progress) error {
	// Before anything reads or writes: an absent root means this scan has
	// nothing true to say about the library, so it must not say anything.
	if err := checkRoot(lib.Path); err != nil {
		return err
	}

	/*
	 * The location being walked (ADR 0034).
	 *
	 * Resolved rather than assumed, because every row this pass writes records
	 * it, and every containment check downstream resolves against what is
	 * recorded. A library has exactly one root today, so this is its only one —
	 * step 4 turns this lookup into the loop that walks each of them, and this
	 * is deliberately shaped so that becomes a change of scope rather than a
	 * change of meaning.
	 */
	roots, err := s.st.ListRoots(ctx, lib.ID)
	if err != nil {
		return err
	}
	if len(roots) == 0 {
		// A library with no location cannot be scanned. The store refuses to
		// create one and refuses to remove the last, so this is a corrupted row
		// rather than an ordinary state — and reconciling against nothing would
		// mark the whole library missing, which is the failure checkRoot exists
		// to prevent, arrived at from a different direction.
		return fmt.Errorf("%w: library %d has no location", ErrRootUnavailable, lib.ID)
	}
	root := roots[0]

	// .m3u files seen on the way past, imported once the tracks they reference
	// exist. Never nil-checked below: ranging over an empty slice is a no-op.
	var playlists []string
	known, err := s.st.KnownFiles(ctx, lib.ID)
	if err != nil {
		return err
	}
	// Paths the user removed from the server without deleting the file. They are
	// skipped so a rescan never re-adds them.
	ignored, err := s.st.IgnoredPaths(ctx, lib.ID)
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
		// Playlists are not media and never become items on this pass — they are
		// collected here and imported after the tracks exist, because an import
		// resolves paths against rows the walk has not written yet. Collected
		// during the walk rather than by a second one: a library is walked once,
		// and a 200,000-file tree does not want traversing twice for the handful
		// of .m3u files in it.
		if isPlaylistFile(path) {
			playlists = append(playlists, path)
			return nil
		}
		// What the library is for decides what counts as media in it — a movie
		// library ignores the MP3s beside a film, a music library ignores the
		// MKV in an album folder (ADR 0024).
		if !media.IsScannable(path, lib.Kind) {
			// Counted, but only when the file is media of the *other* sort. A
			// library is full of .jpg, .nfo and .srt that are rightly ignored,
			// and counting those would bury the signal in noise. An audio file
			// in a movie library is the signal: either a soundtrack beside a
			// film, or — the case this exists for — a music library created with
			// the wrong kind, where every track is discarded and the scan
			// otherwise reports "0 items" as though the folder were empty.
			if media.IsAudio(path) || media.IsVideo(path) {
				s.mu.Lock()
				p.SkippedKind++
				s.mu.Unlock()
			}
			return nil
		}
		if ignored[path] {
			// On the ignore list — present on disk, deliberately kept out of the
			// library. Not counted as seen, so it is never re-added.
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
			// The bytes are unchanged, but the *interpretation* may not be:
			// re-typing a library as shows makes a "Part 2" file an episode
			// rather than a movie work, and better parsing in a new build can
			// reclassify a file too. Re-parsing a name is nearly free (no I/O),
			// so do it and act only when the classification actually moved
			// families — otherwise a rescan stays the cheap no-op it must be.
			if reinterpreted(st.Kind, media.Parse(lib.Path, path, lib.Kind).Kind) {
				if _, err := s.upsert(ctx, lib, root, path, info, p); err == nil {
					// A changed identity must be re-matched; the stamp is what
					// removes it from the enrichment queue.
					_ = s.st.ClearMetadataStamp(ctx, lib.ID, st.ID)
					s.mu.Lock()
					p.ItemsChanged++
					s.mu.Unlock()
				}
			}
			// Subtitles are still re-checked. A sidecar dropped next to an
			// untouched film is the common way they arrive, and skipping the
			// video would otherwise mean it is never noticed.
			s.syncSubtitles(ctx, st.ID, path)
			return nil
		}

		id, err := s.upsert(ctx, lib, root, path, info, p)
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

	/*
	 * Checked again, immediately before the only destructive step.
	 *
	 * The pre-flight check above cannot cover a drive pulled *during* a scan.
	 * That failure looks different and is just as bad: the walk is partway
	 * through, every remaining directory errors, each one is tolerated as "a
	 * single unreadable directory" — which is deliberate and right for a genuine
	 * one-off — and the walk ends normally with a `seen` set holding only what
	 * was reached before the drive went. Everything after that point is then
	 * marked missing.
	 *
	 * Re-statting the root is cheap and answers exactly the question that
	 * matters here: is this partial result worth reconciling against? A scan
	 * that loses its root mid-flight has nothing trustworthy to reconcile, so it
	 * fails rather than writing a half-truth.
	 */
	if err := checkRoot(lib.Path); err != nil {
		return err
	}

	// Anything previously known but not seen this pass is marked missing —
	// never deleted. An unmounted drive must not destroy library data, which is
	// enforced by the two checkRoot calls rather than by this loop.
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

	// Music reads its metadata from the files themselves before anything is
	// grouped, because for music the tags are the authority and the folder is
	// the guess — the reverse of video (ADR 0024).
	if lib.Kind == media.LibraryMusic {
		if err := s.applyTrackTags(ctx, lib, p); err != nil {
			s.log.Warn("reading tags failed", "library", lib.ID, "error", err)
		}
		// After the tags, not before: an import resolves each line to an item
		// by path, and on a first scan those rows are written by the pass above.
		s.importPlaylists(ctx, lib, p, playlists)
	}

	// Pictures group by folder and nothing else (ADR 0028), so they take none of
	// the video reconciliation below — no shows, no multi-part works, no
	// serials. Returning here rather than falling through is deliberate: those
	// passes re-parse filenames looking for season and part markers, and a photo
	// named "S01E02_beach.jpg" is not an episode of anything.
	if lib.Kind == media.LibraryPicture {
		if err := s.reconcilePictures(ctx, lib); err != nil {
			s.log.Warn("gallery reconciliation failed", "library", lib.ID, "error", err)
		}
		if n, err := s.st.PruneEmptyContainers(ctx, lib.ID); err != nil {
			s.log.Warn("pruning empty containers failed", "library", lib.ID, "error", err)
		} else if n > 0 {
			s.log.Info("pruned empty containers", "library", lib.ID, "count", n)
		}
		return s.st.TouchLibraryScanned(ctx, lib.ID)
	}

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
	// After reconciliation, a container left empty by a reinterpretation (a
	// movie work whose parts became a show's episodes) is an orphan; remove it.
	if n, err := s.st.PruneEmptyContainers(ctx, lib.ID); err != nil {
		s.log.Warn("pruning empty containers failed", "library", lib.ID, "error", err)
	} else if n > 0 {
		s.log.Info("pruned empty containers", "library", lib.ID, "count", n)
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

// reinterpreted reports whether a freshly parsed kind belongs to a different
// family than what is stored — the signal that an unchanged file must be
// re-recorded. A movie parse and its grouped forms (part, chapter) are one
// family, so grouping a film into a work is not a reinterpretation and does not
// churn every scan; a movie turning into an episode (a Part file in a library
// now typed as shows) is.
//
// Every kind that can be parsed needs a case. The default is for kinds nothing
// parses *to*, and it used to swallow the ones that do: a track parses as
// KindTrack, fell through to `stored != "other"`, and every rescan of a music
// library re-recorded every track and cleared its metadata stamp — re-queueing
// the entire library for enrichment on a scan that changed nothing. Found by a
// picture rescan test asking the same question of a new kind.
func reinterpreted(stored string, parsed media.Kind) bool {
	switch parsed {
	case media.KindEpisode:
		return stored != "episode"
	case media.KindMovie:
		return stored != "movie" && stored != "part" && stored != "chapter"
	case media.KindTrack:
		return stored != "track"
	case media.KindPhoto:
		return stored != "photo"
	default:
		return stored != "other"
	}
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

func (s *Scanner) upsert(ctx context.Context, lib store.Library, root store.LibraryRoot, path string, info fs.FileInfo, p *Progress) (int64, error) {
	// Parsed against the root this file was walked under, not the library's
	// first one (ADR 0034).
	//
	// This is the call with the quietest failure in the whole change. Parse
	// derives structure from the path *relative to the root* — for music,
	// ParseTrack reads artist and album out of the folder layout — so a file
	// under the second location parsed against the first one does not error,
	// it produces a plausible wrong answer. Containment fails closed if it gets
	// the root wrong; this fails open, and it fails as a mis-titled album
	// somebody notices weeks later.
	nfo := media.Parse(root.Path, path, lib.Kind)

	f := store.ScanFile{
		LibraryID: lib.ID,
		RootID:    root.ID,
		Path:      path,
		Kind:      string(nfo.Kind),
		Title:     nfo.Title,
		SortTitle: media.SortTitle(nfo.Title),
		Container: trimDot(filepath.Ext(path)),
		SizeBytes: info.Size(),
		MTime:     info.ModTime().Unix(),
	}
	if p != nil && lib.Kind == "movie" && nfo.Kind == media.KindEpisode {
		// Counted, not corrected. The parse is right — this file *is* an
		// episode — and the library says it holds films, so what is wrong is
		// the library. Changing the kind here would be a scan re-litigating
		// identity for a whole library, which is the thing the locked-fields
		// rule forbids.
		s.mu.Lock()
		p.EpisodesInMovieLibrary++
		s.mu.Unlock()
	}
	if nfo.Year != 0 {
		y := nfo.Year
		f.Year = &y
	}
	if nfo.Series != "" {
		sr := nfo.Series
		f.Series = &sr
		// Episodes sort under their series, not their own episode title —
		// which is also what makes them tie, so the default order falls
		// through to season/episode and a season plays in order.
		//
		// A track keeps its own title, because a music library is browsed and
		// searched by track title. That means tracks never tie, so an album
		// played in order needs the explicit "track" sort rather than the
		// default (store.ItemFilter).
		if nfo.Kind != media.KindTrack {
			f.SortTitle = media.SortTitle(sr)
		}
	}
	// Season and episode carry disc and track for music, which is the whole
	// reason ParseTrack extracts them — without this they were computed and
	// then dropped, and an album played in filename order.
	if nfo.Kind == media.KindEpisode || nfo.Kind == media.KindTrack {
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
