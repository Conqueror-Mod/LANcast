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
	// SkippedExtras counts trailers, featurettes, deleted scenes and sample
	// files left out of a video library (ADR 0038).
	//
	// Reported for the same reason SkippedKind is, and with more force: this
	// number is the difference between a library that says 1,381 films and one
	// that says 1,192, and a person comparing those against another server has
	// no way to discover where the extra ones came from. Saying "189 extras" is
	// the whole explanation.
	SkippedExtras int `json:"skipped_extras"`
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
	PlaylistsImported int `json:"playlists_imported"`

	/*
	 * Which of the library's locations this scan actually read (ADR 0034).
	 *
	 * Reported because the alternative is a scan that looks complete while
	 * having covered half the library. With one location a skip is a failure
	 * and shows as one; with several it is ordinary — an external drive asleep
	 * — and the honest report is "3 of 4", not silence.
	 *
	 * The path is included because "a location was skipped" is not actionable
	 * and "D:\Family is not available" is. These are user-configured
	 * directories the settings screen already shows, not the per-file paths
	 * Issue deliberately keeps relative.
	 */
	RootsScanned int           `json:"roots_scanned"`
	RootsSkipped []SkippedRoot `json:"roots_skipped,omitempty"`

	/*
	 * A verdict on the shape of what this scan produced, when it does not look
	 * like the kind the library was created as (see shapecheck.go).
	 *
	 * Separate from Issues, which are files that could not be read. Nothing
	 * failed here — the scan succeeded, and that is the problem: kind is
	 * immutable, so a library scanned as the wrong one is wrong permanently
	 * unless somebody is told at the moment it happens.
	 */
	ShapeWarning *ShapeWarning `json:"shape_warning,omitempty"`

	Issues     []Issue `json:"issues,omitempty"`
	StartedAt  int64   `json:"started_at"`
	FinishedAt *int64  `json:"finished_at,omitempty"`
	Error      string  `json:"error,omitempty"`

	/*
	 * When the scan began, at a resolution worth measuring with.
	 *
	 * Unexported, so the API contract is unchanged: `started_at` and
	 * `finished_at` stay the second-resolution stamps clients already read.
	 * They are also why this exists. Deciding whether a scan is worth
	 * optimising needs its duration, and subtracting two second-stamps reports
	 * an unchanged 9,276-track music library as "0" or "3" — the difference
	 * between one second and three being most of the answer.
	 *
	 * That measurement was taken with a stopwatch and the settings pane,
	 * because the log recorded counts and no duration at all. It cost a driven
	 * UI and a polling loop to learn a number the scan already knew.
	 */
	started time.Time
}

// Issue is a file or directory the scan could not fully process. It carries a
// library-relative path — never the absolute server path, which the API keeps
// private for the same reason it withholds item paths.
type Issue struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// SkippedRoot is a library location this scan could not read.
type SkippedRoot struct {
	ID   int64  `json:"id"`
	Path string `json:"path"`
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

	/*
	 * Whether a finished scan should remove what it marked missing.
	 *
	 * A function rather than a value, because the setting can change while the
	 * server runs and a scan takes minutes: reading it at the moment the
	 * question is asked is the only way the answer reflects what the setting
	 * says now. Nil means no — a scanner nobody told is a scanner that does not
	 * delete, which is the safe direction for a switch about destroying rows.
	 */
	emptyTrash func() bool
}

func New(st *store.Store, log *slog.Logger) *Scanner {
	return &Scanner{
		st:      st,
		log:     log,
		running: map[int64]bool{},
		last:    map[int64]*Progress{},
	}
}

/*
 * EmptyTrashWhen tells the scanner how to find out whether to empty the trash.
 *
 * Injected rather than imported, so `scan` does not depend on `config`: the
 * package that walks a filesystem has no business knowing the shape of a
 * settings file, and the one test that matters here is about *when* it is
 * allowed, not about where the flag is stored.
 */
func (s *Scanner) EmptyTrashWhen(fn func() bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emptyTrash = fn
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
	now := time.Now()
	p := &Progress{
		LibraryID: lib.ID,
		State:     StateRunning,
		StartedAt: now.Unix(),
		started:   now,
	}
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
	end := time.Now()
	fin := end.Unix()
	p.FinishedAt = &fin
	/*
	 * Milliseconds, on both outcomes.
	 *
	 * A scan that failed after forty minutes and one that failed on its first
	 * directory are different faults, and the line said the same thing about
	 * both.
	 */
	elapsed := end.Sub(p.started).Milliseconds()
	/*
	 * The outcome is decided here and *published* at the end, after the trash.
	 *
	 * State leaving StateRunning is how everything else learns the scan is
	 * over — the client polls it, and so does every test that waits for one.
	 * Emptying the trash after that flip meant a scan announced itself finished
	 * and then went on deleting rows, so an observer could refresh in the gap
	 * and see rows that were already condemned.
	 *
	 * That is this project's most-repeated bug wearing a new hat: the request
	 * succeeds, the server is right, and only the picture is stale. Here the
	 * server was briefly wrong too, because it said finished before it was.
	 *
	 * Caught as a test failing two or three runs in a hundred on CI and never
	 * on a desk, which is the shape of a race that needs a slow machine.
	 */
	finalState := StateIdle
	if err != nil {
		finalState = StateFailed
		p.Error = err.Error()
		s.log.Error("scan failed", "library", lib.ID, "elapsed_ms", elapsed, "error", err)
	} else {
		s.log.Info("scan complete", "library", lib.ID,
			"seen", p.FilesSeen, "changed", p.ItemsChanged, "missing", p.ItemsMissing,
			"elapsed_ms", elapsed)

		// The shape check runs on success only. A failed scan produced a
		// partial library by definition, and telling somebody their TV library
		// has no shows in it because the drive went away halfway through would
		// be a false alarm about a permanent mistake — the most expensive kind
		// of false alarm this project could ship.
		if sh, err := s.st.Shape(ctx, lib.ID); err != nil {
			s.log.Error("library shape check", "library", lib.ID, "error", err)
		} else {
			w := CheckShape(lib.Kind, sh, *p)
			if w.Code != "" {
				p.ShapeWarning = &w
				s.log.Warn("library shape looks wrong for its kind",
					"library", lib.ID, "kind", lib.Kind, "code", w.Code)
			}
			/*
			 * Stored on the row, because live progress dies with the process and
			 * this reports a mistake that cannot be undone — a library scanned on
			 * Tuesday looked fine on Wednesday.
			 *
			 * Clearing is the subtle half. Part of the evidence — the count of
			 * files that parsed as episodes — is gathered during the walk, so it
			 * only reflects files *this* scan actually processed. A rescan that
			 * finds nothing changed therefore produces no evidence and no
			 * verdict, which is not the same as producing a clean bill of health.
			 * Clearing on that would mean any rescan silently erased a standing
			 * warning, which is how this was first written and what the run
			 * against real files caught.
			 *
			 * So a warning is replaced when there is one, and withdrawn only by a
			 * scan that did enough work to have seen the problem again.
			 */
			switch {
			case w.Code != "":
				if err := s.st.SetShapeWarning(ctx, lib.ID, &w); err != nil {
					s.log.Error("store shape warning", "library", lib.ID, "error", err)
				}
			case p.ItemsChanged > 0 || p.ItemsMissing > 0:
				if err := s.st.SetShapeWarning(ctx, lib.ID, nil); err != nil {
					s.log.Error("clear shape warning", "library", lib.ID, "error", err)
				}
			}
		}
	}
	wants := s.emptyTrash
	// The verdict wants the outcome, which is not on p yet: publishing it early
	// is the very thing this ordering exists to avoid.
	outcome := *p
	outcome.State = finalState
	verdict := MayEmptyTrash(outcome)
	s.mu.Unlock()

	/*
	 * Emptying the trash, if it was asked for and this scan can be trusted.
	 *
	 * Outside the lock, because it is a database write and the mutex here
	 * guards the progress map. After the shape check, because that reads the
	 * library's own counts and the rows being removed are part of them — a
	 * shape verdict computed against a library half-deleted would describe
	 * neither the before nor the after.
	 */
	if wants != nil && wants() {
		if !verdict.Allowed {
			// Info rather than Debug: a switch somebody turned on and that then
			// declines to act is exactly the state that reads as broken, and
			// the only place it can be read is here.
			s.log.Info("not emptying trash", "library", lib.ID, "reason", verdict.Reason)
		} else if n, err := s.st.EmptyTrash(ctx, lib.ID); err != nil {
			s.log.Error("empty trash", "library", lib.ID, "error", err)
		} else if n > 0 {
			s.log.Info("emptied trash", "library", lib.ID, "removed", n)
		}
	}

	/*
	 * Only now is the scan over.
	 *
	 * Everything that changes what the library holds has happened, so a client
	 * that sees this and refreshes gets the library as it will stay. The finish
	 * hook is called after the flip for the same reason it is called outside
	 * the lock: it starts enrichment, which reads scan status.
	 */
	s.mu.Lock()
	p.State = finalState
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
/*
 * CheckRoot reports whether a library location can be read right now.
 *
 * Exported because it is the only honest answer to "is this file really gone,
 * or is the drive asleep" — and that question is asked outside a scan too. The
 * API asks it before letting somebody forget a missing row: a location that
 * reads fine and does not hold the file is evidence the file has gone, where
 * `missing` alone is only evidence that a walk did not find it.
 */
func CheckRoot(root string) error { return checkRoot(root) }

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

/*
 * walk scans every location a library has, and reconciles each against itself.
 *
 * Partial availability is the normal case once a library lives in more than one
 * place (ADR 0034), not a failure: an external drive asleep while the internal
 * one is fine is a Tuesday. So a location that cannot be seen is skipped and
 * reported, the ones that can be seen are scanned, and only a library with no
 * reachable location at all fails.
 *
 * The reconciliation is per location and that is the load-bearing part. A scan
 * that walked two of three must compare what it saw against what those two
 * held — comparing against the *library* would find the third location's files
 * unseen and mark every one of them missing, which is the bug fixed in #228
 * arriving on a healthy server every time a drive slept.
 */
func (s *Scanner) walk(ctx context.Context, lib store.Library, p *Progress) error {
	roots, err := s.st.ListRoots(ctx, lib.ID)
	if err != nil {
		return err
	}
	if len(roots) == 0 {
		// The store refuses to create a library without a location and refuses
		// to remove the last one, so this is a corrupted row rather than an
		// ordinary state. Reconciling against nothing would mark the whole
		// library missing.
		return fmt.Errorf("%w: library %d has no location", ErrRootUnavailable, lib.ID)
	}

	// Library-scoped, so read once rather than per location: an ignored path is
	// a decision about a file, and which location it sits under is not part of
	// that decision.
	ignored, err := s.st.IgnoredPaths(ctx, lib.ID)
	if err != nil {
		return err
	}

	// .m3u files seen on the way past, imported once the tracks they reference
	// exist. Accumulated across locations, because a playlist on one drive may
	// legitimately reference tracks on another.
	var playlists []string

	for _, root := range roots {
		if err := checkRoot(root.Path); err != nil {
			// Not a failure. Recorded so the UI can say "3 of 4 locations
			// scanned" rather than silently doing less than it appears to.
			s.log.Info("skipping unavailable location",
				"library", lib.ID, "root", root.ID, "path", root.Path)
			s.mu.Lock()
			p.RootsSkipped = append(p.RootsSkipped, SkippedRoot{ID: root.ID, Path: root.Path})
			s.mu.Unlock()
			continue
		}
		if err := s.walkRoot(ctx, lib, root, ignored, &playlists, p); err != nil {
			return err
		}
		s.mu.Lock()
		p.RootsScanned++
		s.mu.Unlock()
	}

	if p.RootsScanned == 0 {
		// Every location is gone. With one location this is exactly the
		// single-root behaviour #228 introduced, generalised rather than
		// replaced: a scan with nothing true to say says nothing.
		return fmt.Errorf("%w: no location of library %d could be read", ErrRootUnavailable, lib.ID)
	}

	return s.reconcileLibrary(ctx, lib, p, playlists)
}

// walkRoot scans one location and reconciles that location alone.
func (s *Scanner) walkRoot(ctx context.Context, lib store.Library, root store.LibraryRoot,
	ignored map[string]bool, playlists *[]string, p *Progress) error {

	known, err := s.st.KnownFilesInRoot(ctx, root.ID)
	if err != nil {
		return err
	}

	seen := make(map[string]bool, len(known))

	/*
	 * One directory read per directory, not per file.
	 *
	 * The cache lives for this root's walk and is thrown away with it, so it
	 * cannot go stale between scans — a folder's contents are read once while
	 * the walk is inside it, which is exactly when WalkDir has just listed it
	 * anyway. Measured cost of not having it: a 9,276-track music library spent
	 * around 94 seconds per scan almost entirely here, and reported
	 * `changed=0` every time.
	 */
	dirCache := make(map[string][]fs.DirEntry)
	readDir := func(dir string) ([]fs.DirEntry, error) {
		if e, ok := dirCache[dir]; ok {
			return e, nil
		}
		e, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		dirCache[dir] = e
		return e, nil
	}

	/*
	 * Subtitles are a video question, and asking it of audio is not merely
	 * wasted work — it is thousands of directory reads and a write transaction
	 * per track, every scan, looking for `.srt` files beside an MP3.
	 *
	 * A picture library is the same. Neither can carry a sidecar subtitle in any
	 * sense the subtitle package recognises, so the honest thing is not to ask.
	 */
	wantsSubtitles := lib.Kind == media.LibraryMovie || lib.Kind == media.LibraryShow

	err = filepath.WalkDir(root.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A single unreadable directory shouldn't abort the whole scan.
			s.log.Warn("skipping unreadable path", "path", path, "error", err)
			s.recordIssue(p, root.Path, path, "unreadable")
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
			*playlists = append(*playlists, path)
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
		/*
		 * Extras are not works (ADR 0038).
		 *
		 * A trailer, a featurette and a `sample.mkv` are playable video sitting
		 * in a film's folder, and importing them made each one a film with a
		 * title, a tile and a line in the count. Video libraries only: the rule
		 * reads folder names that mean nothing in a music or picture library,
		 * where "Interviews" is a perfectly ordinary album or album folder.
		 *
		 * Not marked seen, so a file that was imported by an earlier build is
		 * marked *missing* on this scan rather than deleted — the scanner never
		 * deletes, and a rule that quietly removed rows would be a worse thing
		 * to be wrong about than the import it is correcting.
		 */
		if (lib.Kind == media.LibraryMovie || lib.Kind == media.LibraryShow) &&
			media.IsExtra(root.Path, path) {
			s.mu.Lock()
			p.SkippedExtras++
			s.mu.Unlock()
			return nil
		}

		seen[path] = true
		s.mu.Lock()
		p.FilesSeen++
		s.mu.Unlock()

		info, err := d.Info()
		if err != nil {
			s.log.Warn("stat failed", "path", path, "error", err)
			s.recordIssue(p, root.Path, path, "could not read file info")
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
			if reinterpreted(st.Kind, media.Parse(root.Path, path, lib.Kind).Kind) {
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
			if wantsSubtitles {
				s.syncSubtitles(ctx, st.ID, path, readDir)
			}
			return nil
		}

		id, err := s.upsert(ctx, lib, root, path, info, p)
		if err != nil {
			s.log.Warn("upsert failed", "path", path, "error", err)
			s.recordIssue(p, root.Path, path, "could not be recorded")
			return nil
		}
		if wantsSubtitles {
			s.syncSubtitles(ctx, id, path, readDir)
		}
		s.mu.Lock()
		p.ItemsChanged++
		s.mu.Unlock()
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk %q: %w", root.Path, err)
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
	if err := checkRoot(root.Path); err != nil {
		return err
	}

	// Anything previously known *in this location* but not seen this pass is
	// marked missing — never deleted. Scoped to the location by KnownFilesInRoot
	// above, so a drive that is merely asleep cannot cost a file on the drive
	// that is awake.
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
	p.ItemsMissing += len(gone)
	s.mu.Unlock()
	return nil
}

// reconcileLibrary runs the passes that are about the library as a whole rather
// than about any one location.
//
// Deliberately after every location has been walked, and deliberately not
// per-root: a show whose seasons are split across two drives is one show, and
// grouping it needs both walks to have happened. That is also why containers
// are keyed on library rather than on root — a container that belonged to a
// location could not span them.
func (s *Scanner) reconcileLibrary(ctx context.Context, lib store.Library, p *Progress, playlists []string) error {

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
		// A gallery empties the same way a season does — the folder is deleted
		// and its photographs are marked missing, leaving a tile over nothing.
		if marked, restored, err := s.st.ReconcileMissingContainers(ctx, lib.ID); err != nil {
			s.log.Warn("reconciling empty galleries failed", "library", lib.ID, "error", err)
		} else if marked > 0 || restored > 0 {
			s.log.Info("galleries followed their photographs",
				"library", lib.ID, "now_missing", marked, "restored", restored)
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

	// Rows written before the edition column existed have never had their
	// filename read for a marker, and nothing else will ever read it: the
	// scanner upserts only files whose bytes moved (ADR 0049). This examines
	// each film once in the life of the library and is a no-op thereafter.
	//
	// Non-fatal, like every pass here. An unread marker is a missing label, not
	// a broken library, and it must not cost a scan that otherwise succeeded.
	if res, err := BackfillEditions(ctx, s.st, lib.ID); err != nil {
		s.log.Warn("edition backfill failed", "library", lib.ID, "error", err)
	} else if res.Marked > 0 {
		s.log.Info("read edition markers", "library", lib.ID,
			"examined", res.Examined, "marked", res.Marked)
	}
	// After reconciliation, a container left empty by a reinterpretation (a
	// movie work whose parts became a show's episodes) is an orphan; remove it.
	if n, err := s.st.PruneEmptyContainers(ctx, lib.ID); err != nil {
		s.log.Warn("pruning empty containers failed", "library", lib.ID, "error", err)
	} else if n > 0 {
		s.log.Info("pruned empty containers", "library", lib.ID, "count", n)
	}

	/*
	 * A container whose children have all gone offline goes with them.
	 *
	 * PruneEmptyContainers above only removes a container with *no* children;
	 * one holding eight missing episodes keeps them, and so keeps its own tile.
	 * A show reorganised on disk therefore accumulated a season per old folder
	 * layout — reported as a duplicated season, which is what it looks like when
	 * the extra one describes a directory that no longer exists.
	 *
	 * Marked rather than deleted, because parent_id cascades: deleting the
	 * season would delete the missing episodes under it, and scanning marks
	 * missing precisely so that an unmounted drive costs nothing. Reversible for
	 * the same reason — remount, rescan, and the season is a season again.
	 */
	if marked, restored, err := s.st.ReconcileMissingContainers(ctx, lib.ID); err != nil {
		s.log.Warn("reconciling empty containers failed", "library", lib.ID, "error", err)
	} else if marked > 0 || restored > 0 {
		s.log.Info("containers followed their children",
			"library", lib.ID, "now_missing", marked, "restored", restored)
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
	roots, err := s.st.RootPaths(ctx, lib.ID)
	if err != nil {
		return err
	}
	/*
	 * Grouped in two passes, because a show's identity depends on all of its
	 * episodes and not on the first one seen.
	 *
	 * The single pass this replaces keyed shows on the episode's directory, so
	 * a series stored as `Show S01`, `Show S02`, … — a season per top-level
	 * folder, which no season-folder pattern can match because the folder name
	 * is not *only* a season marker — became one show per season. Twenty tiles
	 * for one series, each reading "1 season" (ADR 0037).
	 */
	groups := map[string]*showGroup{}
	var order []string
	for _, ep := range episodes {
		root := rootOf(roots, ep.RootID)
		if root == "" {
			continue
		}
		// Against the episode's own location. With the library's first one, an
		// episode sitting loose in a *second* location never meets the root the
		// walk is looking for, so instead of staying top-level it invents a show
		// named after that drive's folder.
		showDir := media.ShowDir(root, ep.Path)
		if showDir == "" {
			// An episode sitting directly in the library root is not a show
			// layout; leave it top-level rather than invent a show for it.
			continue
		}

		/*
		 * The parsed series name is the identity; the folder is the fallback.
		 *
		 * Where the filename says what series this is, that is a better answer
		 * than the directory, because it is the one thing that stays the same
		 * across every layout a series can be stored in. Where it says nothing,
		 * the directory is all there is, and keying on it preserves exactly the
		 * old behaviour for that episode rather than inventing a group.
		 */
		title := deref(ep.Series)
		key := media.SortTitle(title)
		if key == "" {
			title = filepath.Base(showDir)
			key = "\x00dir:" + showDir // cannot collide with a normalized title
		}

		g, ok := groups[key]
		if !ok {
			g = &showGroup{title: title, sortTitle: media.SortTitle(title)}
			groups[key] = g
			order = append(order, key)
		}
		g.dirs = append(g.dirs, showDir)
		g.episodes = append(g.episodes, ep)
	}

	shows := map[string]int64{}   // group key  -> show id
	seasons := map[string]int64{} // season key -> season id

	for _, key := range order {
		g := groups[key]
		showID, ok := shows[key]
		if !ok {
			id, _, err := s.st.EnsureShowByTitle(ctx, lib.ID, g.sortTitle, g.title, g.path())
			if err != nil {
				return err
			}
			shows[key] = id
			showID = id
		}

		for _, ep := range g.episodes {
			if err := s.attachEpisode(ctx, lib, showID, g, ep, seasons); err != nil {
				return err
			}
		}
	}
	return nil
}

/*
 * showGroup is the episodes that turned out to be one series, and the
 * directories they were found in.
 *
 * The directories are kept because the show row's `path` is where `tvshow.nfo`
 * is written (ADR 0010) — so a series living in one folder must keep that
 * folder as its path, or sidecar writing silently stops working for the
 * ordinary layout while fixing the unusual one.
 */
type showGroup struct {
	title     string
	sortTitle string
	dirs      []string
	episodes  []store.Item
}

/*
 * path is the directory a sidecar belongs in, or a synthetic identity when
 * there is no such directory.
 *
 * One directory means the ordinary layout, and it is used unchanged. Several
 * means the series is split across sibling folders, and there is no directory
 * that *is* the show — writing `tvshow.nfo` into whichever season folder was
 * scanned first would put a series-level file inside one season of it. So the
 * identity becomes synthetic, in the same shape collections already use, and
 * the sidecar writer skips it (a path that is not a filesystem path fails
 * containment, which is the behaviour that already exists for collections).
 */
func (g *showGroup) path() string {
	first := ""
	for _, d := range g.dirs {
		if first == "" || d < first {
			first = d
		}
	}
	for _, d := range g.dirs {
		if d != first {
			// Deterministic, and independent of scan order — the same series
			// must not change identity because a walk returned folders in a
			// different sequence.
			return "lancast:show:" + g.sortTitle
		}
	}
	return first
}

// attachEpisode files one episode under its season, creating the season when
// needed. Split out of reconcileHierarchy only because that function now has a
// grouping pass in front of it and the body was becoming two things at once.
func (s *Scanner) attachEpisode(ctx context.Context, lib store.Library, showID int64,
	g *showGroup, ep store.Item, seasons map[string]int64) error {

	seasonNum := deref2(ep.Season)
	seasonPath := media.SeasonDir(ep.Path)
	if seasonPath == "" {
		// No "Season N" folder: synthesize a stable identity under the show.
		seasonPath = fmt.Sprintf("%s::season=%d", g.path(), seasonNum)
	}
	seasonID, ok := seasons[seasonPath]
	if !ok {
		title := fmt.Sprintf("Season %d", seasonNum)
		if seasonNum == 0 {
			title = "Specials"
		}
		id, created, err := s.st.EnsureSeason(ctx, lib.ID, showID, seasonNum, seasonPath, title, media.SortTitle(title))
		if err != nil {
			return err
		}
		/*
		 * A season that already existed keeps the parent it was created with,
		 * because EnsureSeason inserts or does nothing. That is exactly the
		 * database every upgrade starts from: `Show\Season 01` keeps its season
		 * directory as its identity, so the row is found rather than made, and
		 * without this it stays hanging off the *old* per-folder show — the
		 * regrouping would appear to do nothing for the layout that was already
		 * correct.
		 */
		if !created {
			if err := s.st.SetParent(ctx, id, &showID); err != nil {
				return err
			}
		}
		seasons[seasonPath] = id
		seasonID = id
	}

	if ep.ParentID == nil || *ep.ParentID != seasonID {
		if err := s.st.SetParent(ctx, ep.ID, &seasonID); err != nil {
			return err
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
/*
 * syncSubtitles indexes the subtitle files sitting beside one video.
 *
 * `read` is the walk's shared directory reader. Sidecar discovery is a question
 * about a *folder* — what else is next to this file — and it was being asked
 * once per file and answered by reading the directory twice. A season of twenty
 * episodes read the same folder forty times per scan.
 */
func (s *Scanner) syncSubtitles(ctx context.Context, itemID int64, videoPath string,
	read subtitle.DirReader) {
	found := subtitle.FindSidecarsWith(videoPath, read)
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
	/*
	 * The edition marker, kept rather than discarded (ADR 0042).
	 *
	 * The strip has always happened -- it is what makes "Alien DC" match Alien
	 * -- and the finding used to be thrown away, which left two editions of one
	 * film as two rows identical in every field a person can see. Nil when the
	 * filename claimed nothing, so a row reads "no edition stated" rather than
	 * empty-string-means-something.
	 */
	if nfo.Edition != "" {
		ed := nfo.Edition
		f.Edition = &ed
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

/*
 * rootOf returns the location an item was scanned under, or "" when it has
 * none.
 *
 * The reconciliation passes below run once per library and previously derived
 * every item's structure from `lib.Path` — which is one location out of
 * possibly several, so a file in the second one was relativised against the
 * first one's root. That does not error. `ShowDir` walks up looking for a root
 * it never meets and returns a directory anyway; `groupFromPath` gets a
 * cross-volume `filepath.Rel` failure and quietly yields no artist and no
 * album. Both produce a plausible wrong answer, which is the failure shape this
 * whole change keeps running into (ADR 0034).
 *
 * Falling back to the library's first location would restore exactly that bug
 * for the rows most likely to hit it, so a rootless row is skipped by the
 * callers instead.
 */
func rootOf(roots map[int64]string, rootID *int64) string {
	if rootID == nil {
		return ""
	}
	return roots[*rootID]
}
