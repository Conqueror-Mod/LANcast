package scan

import (
	"context"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"lancast/internal/media"
	"lancast/internal/probe"
	"lancast/internal/store"
)

// TagReader reads a music file's embedded metadata. Satisfied by *probe.Prober.
//
// An interface so a scan can run without one — a server with no ffprobe still
// indexes music, it just falls back to folder and filename — and so tests do
// not need ffmpeg on the machine.
type TagReader interface {
	ReadTags(ctx context.Context, path string) (probe.Tags, error)
}

// SetTagReader wires embedded-tag reading for music libraries. Without one,
// scanning still works and tracks keep whatever their filenames gave them.
func (s *Scanner) SetTagReader(r TagReader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tags = r
}

// tagConcurrency is how many files are read at once.
//
// Measured, not guessed: reading tags one file at a time took 82 ms each — over
// two minutes for a 1,592-track library, which is long enough that a scan looks
// hung. The cost is process startup rather than IO, so it parallelises well:
// the same library takes about 30 seconds at eight workers and 19 at sixteen.
//
// Half the CPUs, floor of four, for the same reason the probe worker uses a
// fraction — a first scan should not make the machine unpleasant to use.
func tagConcurrency() int {
	n := runtime.NumCPU() / 2
	if n < 4 {
		n = 4
	}
	return n
}

// applyTrackTags reads every track's embedded tags and writes them onto its
// row, concurrently.
//
// Runs after the walk rather than inside it: the walk is a single ordered pass
// over the filesystem, and making it wait on a subprocess per file would serialise
// the expensive part behind the cheap one.
func (s *Scanner) applyTrackTags(ctx context.Context, lib store.Library, p *Progress) error {
	s.mu.Lock()
	reader := s.tags
	s.mu.Unlock()
	if reader == nil {
		return nil
	}

	/*
	 * Nothing moved on disk, and a previous scan finished: there is nothing to
	 * read.
	 *
	 * This pass opens and parses the tags of **every track in the library**,
	 * then rebuilds the artist and album grouping from what it read. On a
	 * 9,276-track library that was about 94 seconds, on every scan, and the
	 * three scans before this was written each reported `changed=0` — a minute
	 * and a half of reading files to arrive at the grouping already in the
	 * database.
	 *
	 * The reasoning that makes it safe to skip is that tags live *inside* the
	 * file. The walk has just established that no file's size or mtime moved,
	 * so no file's tags can have moved either, and the grouping they produced
	 * last time is still the right one.
	 *
	 * `ScannedAt` is the guard that matters. It is written only after a
	 * reconcile completes, so a scan interrupted halfway — which may have
	 * grouped some tracks and not others — leaves it unset and the next scan
	 * does the full pass rather than trusting a half-built hierarchy.
	 *
	 * The cost of this: improving the *grouping rules* no longer takes effect
	 * on a library where nothing has changed on disk, because the rules are only
	 * re-run when something does. That is the same trade the video path already
	 * makes with `reinterpreted`, and the escape hatch is the same — touch the
	 * library, or change anything in it, and the pass runs.
	 */
	if lib.ScannedAt != nil && p != nil && p.ItemsChanged == 0 && p.ItemsMissing == 0 {
		s.log.Debug("skipping tag pass; nothing changed since the last scan",
			"library", lib.ID)
		return nil
	}

	/*
	 * Phase timings, because "the tag pass is slow" is not a finding.
	 *
	 * Measured on a real library, a scan that changed 17 tracks took 92
	 * seconds while an unchanged one took 0.5 — so the cost is unrelated to
	 * what changed, and the question is which phase owns it. Logged at info
	 * rather than debug: this is the number anybody diagnosing a slow scan
	 * needs, and it is one line per scan.
	 */
	phaseStart := time.Now()

	tracks, err := s.st.LibraryTracks(ctx, lib.ID)
	if err != nil {
		return err
	}
	loadMS := time.Since(phaseStart).Milliseconds()
	// Each track's own location. Folder-derived artist and album are relative to
	// the location the file is in, and using the library's first one gives a
	// cross-volume filepath.Rel failure — which is not an error here, it is a
	// track that silently loses its artist and album (ADR 0034).
	roots, err := s.st.RootPaths(ctx, lib.ID)
	if err != nil {
		return err
	}
	if len(tracks) == 0 {
		// No present tracks, but reconciliation still runs: a container emptied
		// by an earlier pass is an empty shelf whether or not there is anything
		// left to read tags from.
		return s.reconcileMusic(ctx, lib, nil)
	}

	/*
	 * Stored keys stand in for the files that did not change (ADR 0056).
	 *
	 * The walk has just proven that no unchanged file's size or mtime moved,
	 * and tags live inside the file, so the key it produced last time is still
	 * the right one. Every track still contributes a group, because
	 * dropBucketAlbums judges a *folder* — one track's absence would change
	 * what its folder looks like.
	 *
	 * A track with no stored key is read: rows written before revision 39 have
	 * none, so an upgrade pays one full pass and then stops paying.
	 */
	/*
	 * Only a library with a completed scan may reuse anything.
	 *
	 * The same guard the whole-pass skip uses, and for the same reason: until
	 * a reconcile has finished, nothing about this library's grouping is known
	 * to be the product of a complete pass. Reusing keys there would extend
	 * trust to exactly the state the existing rule already refuses to trust.
	 *
	 * It also keeps the assumption honest. Reuse rests on "the walk proved the
	 * file did not move, and tags live inside the file" — which holds for a
	 * file on disk and not for a library nobody has finished reading yet.
	 */
	var stored map[int64]store.GroupKey
	if lib.ScannedAt != nil {
		stored, err = s.st.GroupKeys(ctx, lib.ID)
		if err != nil {
			return err
		}
	}

	readStart := time.Now()
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		untagged int
		groups   []trackGroup
		reused   int
	)
	work := make(chan store.Item)

	for i := 0; i < tagConcurrency(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range work {
				tags, err := reader.ReadTags(ctx, it.Path)
				if err != nil || tags.Empty() {
					// Not an error worth failing a scan over: an untagged file
					// keeps what its folder and filename gave it. Counted so the
					// scan can say so — a library that silently looks wrong is
					// the failure mode this project keeps rediscovering.
					mu.Lock()
					untagged++
					groups = append(groups, groupFromPath(rootOf(roots, it.RootID), it))
					mu.Unlock()
					continue
				}
				if err := s.st.ApplyTrackTags(ctx, it.ID, trackTagsFrom(tags)); err != nil {
					s.log.Warn("apply tags", "item", it.ID, "error", err)
				}
				// The grouping key is collected here, while the album artist
				// is still in hand, and stored on the track once the whole
				// library's groups are assembled (ADR 0056).
				//
				// It was deliberately not stored, on the reasoning that a
				// column existing only to carry a value between two functions
				// in one pass is a bad column. That was right while the pass
				// always ran. Once the unchanged case became free, the key
				// stopped being a value in flight and became the only reason
				// to reopen files the walk had just proven unmoved.
				mu.Lock()
				groups = append(groups, groupFromTags(rootOf(roots, it.RootID), it, tags))
				mu.Unlock()
			}
		}()
	}

	for _, it := range tracks {
		// Reuse the stored key unless this track is one the walk wrote.
		if k, ok := stored[it.ID]; ok && (p == nil || !p.changed[it.ID]) {
			groups = append(groups, trackGroup{
				itemID:          it.ID,
				artist:          k.Artist,
				album:           k.Album,
				dir:             k.Dir,
				albumFromFolder: k.AlbumFromFolder,
				albumAtRoot:     k.AlbumAtRoot,
			})
			reused++
			continue
		}
		select {
		case <-ctx.Done():
			close(work)
			wg.Wait()
			return ctx.Err()
		case work <- it:
		}
	}
	close(work)
	wg.Wait()
	readMS := time.Since(readStart).Milliseconds()

	if untagged > 0 {
		s.log.Info("tracks without usable tags", "library", lib.ID, "count", untagged,
			"note", "titles and albums for these come from folder and filename")
		// Not Skipped: these files imported and play. Counting them as
		// skipped made a music library report 38 failures it could not name,
		// because a failure records an Issue and this is not one.
		s.mu.Lock()
		p.SkippedUntagged += untagged
		s.mu.Unlock()
	}

	// The folder-derived albums are re-examined once every track has been seen:
	// whether a folder is a record or an alphabetical bucket is only visible
	// across the whole folder, never from one file inside it.
	groupStart := time.Now()
	dropBucketAlbums(groups)

	/*
	 * The keys are stored *before* reconcile, and after dropBucketAlbums has
	 * had its say.
	 *
	 * After, because a folder-derived album that did not survive the bucket
	 * check must not be stored as though it had — the next scan would reuse a
	 * key this one rejected, and the album would come back without any file
	 * having changed.
	 *
	 * Every grouped track is written, including the ones whose key was reused
	 * unchanged. One statement each keeps the invariant plain: a track the
	 * scanner grouped has a stored key, and there is no second rule about
	 * which ones do.
	 */
	keys := make(map[int64]store.GroupKey, len(groups))
	for _, g := range groups {
		keys[g.itemID] = store.GroupKey{
			Artist:          g.artist,
			Album:           g.album,
			Dir:             g.dir,
			AlbumFromFolder: g.albumFromFolder,
			AlbumAtRoot:     g.albumAtRoot,
		}
	}
	if err := s.st.SaveGroupKeys(ctx, keys); err != nil {
		// Not fatal: the grouping this pass computed is still correct, and the
		// only cost of failing to store it is that the next scan reads the
		// files again. Failing the scan over a cache write would trade a slow
		// scan for no scan.
		s.log.Warn("storing group keys", "library", lib.ID, "error", err)
	}

	err = s.reconcileMusic(ctx, lib, groups)
	s.log.Info("music tag pass", "library", lib.ID,
		"tracks", len(tracks),
		"changed", p.ItemsChanged,
		"tags_read", len(tracks)-reused,
		"keys_reused", reused,
		"load_ms", loadMS,
		"read_tags_ms", readMS,
		"reconcile_ms", time.Since(groupStart).Milliseconds())
	return err
}

// trackGroup is one track and the artist and album it belongs under.
type trackGroup struct {
	itemID int64
	artist string
	album  string

	// dir is the folder the file sits in, and albumFromFolder records that the
	// album name above was taken from that folder rather than from a tag. Both
	// exist only so dropBucketAlbums can second-guess the guess — a folder is
	// evidence of an album only if it behaves like one.
	dir             string
	albumFromFolder bool
	// albumAtRoot records that the folder the album name came from sits
	// directly in the library root, with no artist folder above it.
	albumAtRoot bool
}

// groupFromTags takes the grouping key from a track's tags.
//
// Album artist, not artist. A compilation carries one album artist and a
// different performer per track; grouping on the performer shatters the record
// into one album per guest, which looks like a scanner bug rather than the
// tag-precedence choice it is (ADR 0024).
func groupFromTags(root string, it store.Item, t probe.Tags) trackGroup {
	artist := t.AlbumArtist
	if artist == "" {
		artist = t.Artist
	}
	album := t.Album

	// A tagged file missing one of the two still has to go somewhere; the
	// folders are the only other evidence.
	fromFolder := false
	if artist == "" || album == "" {
		fallback := groupFromPath(root, it)
		if artist == "" {
			artist = fallback.artist
		}
		atRoot := false
		if album == "" {
			album = fallback.album
			fromFolder = album != ""
			atRoot = fallback.albumAtRoot
		}
		return trackGroup{
			itemID:          it.ID,
			artist:          artist,
			album:           album,
			dir:             filepath.Dir(it.Path),
			albumFromFolder: fromFolder,
			albumAtRoot:     atRoot,
		}
	}
	return trackGroup{
		itemID: it.ID,
		artist: artist,
		album:  album,
		dir:    filepath.Dir(it.Path),
	}
}

// dropBucketAlbums removes album names invented from a folder that is not an
// album.
//
// The fallback assumes the containing folder is a record. On a real library
// that assumption produced albums called "B's" and "C's": a library organised
// into alphabetical buckets, with loose singles sitting directly in them. Every
// one of those became an album named after a letter, holding one track by an
// artist who never made it.
//
// Two tells, and both are needed.
//
// **Cohesion.** A record is by one album artist; a bucket holds whatever starts
// with that letter. A folder-derived album survives only when every track in
// that folder agrees on the artist.
//
// **Depth.** Cohesion alone cannot see a bucket holding a single loose track —
// one track never disagrees with itself, which left an album called "C's" with
// one song in it on the real library. But a record does not sit loose at the
// top of a library: real albums live under an artist folder. A folder-derived
// album taken from a direct child of the library root is a category, not a
// record.
//
// A dropped album is not a lost track: the hierarchy already allows a track to
// hang directly off its artist (reconcileMusic parents it there when it has no
// album), which is the honest shape for a single with no record.
//
// Deliberately does not touch an album that came from a *tag*. A tagged album
// is a statement about the record; this only re-examines a guess.
func dropBucketAlbums(groups []trackGroup) {
	artistsPerDir := map[string]map[string]bool{}
	for _, g := range groups {
		if !g.albumFromFolder {
			continue
		}
		if artistsPerDir[g.dir] == nil {
			artistsPerDir[g.dir] = map[string]bool{}
		}
		artistsPerDir[g.dir][g.artist] = true
	}
	for i, g := range groups {
		if !g.albumFromFolder {
			continue
		}
		if len(artistsPerDir[g.dir]) > 1 || g.albumAtRoot {
			groups[i].album = ""
		}
	}
}

// groupFromPath is the untagged fallback: the containing folder is the album
// and the one above it is the artist.
//
// This is a guess, and a weak one — a real library was laid out as
// `A's/Artist/track.mp3`, where the folder above the track is the artist and
// there is no album folder at all. It is what there is when the file says
// nothing, and it is why tags lead.
func groupFromPath(root string, it store.Item) trackGroup {
	g := trackGroup{itemID: it.ID, dir: filepath.Dir(it.Path)}
	rel, err := filepath.Rel(root, filepath.Dir(it.Path))
	if err != nil || rel == "." || rel == "" {
		return g
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	g.album = parts[len(parts)-1]
	g.albumFromFolder = g.album != ""
	g.albumAtRoot = len(parts) == 1
	if len(parts) >= 2 {
		g.artist = parts[len(parts)-2]
	}
	return g
}

// trackTagsFrom converts probe tags into the store's write shape, preferring
// the album artist.
//
// Album artist, not artist: a compilation carries one album artist and a
// different performer per track, and grouping on the performer shatters the
// record into one album per guest (ADR 0024). The track keeps its own artist
// so the performer is still shown.
func trackTagsFrom(t probe.Tags) store.TrackTags {
	artist := t.Artist
	if artist == "" {
		artist = t.AlbumArtist
	}
	return store.TrackTags{
		Title:     t.Title,
		SortTitle: media.SortTitle(t.Title),
		Artist:    artist,
		Album:     t.Album,
		Disc:      t.Disc,
		Track:     t.Track,
		Year:      t.Year,
	}
}

// reconcileMusic builds the artist → album → track hierarchy.
//
// The same shape as show → season → episode (ADR 0010, ADR 0024): containers
// are rows in media_item related by parent_id, identified by a synthetic path
// because they have no file of their own.
//
// A track with neither an artist nor an album is left top-level rather than
// filed under an invented container — the same choice reconcileHierarchy makes
// for an episode sitting loose in a library root.
func (s *Scanner) reconcileMusic(ctx context.Context, lib store.Library, groups []trackGroup) error {
	artists := map[string]int64{}
	albums := map[string]int64{}

	// Sorted so containers are created in a stable order across runs, which
	// keeps ids predictable and diffs of a rescan boring.
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].artist != groups[j].artist {
			return groups[i].artist < groups[j].artist
		}
		if groups[i].album != groups[j].album {
			return groups[i].album < groups[j].album
		}
		return groups[i].itemID < groups[j].itemID
	})

	for _, g := range groups {
		// A track with neither an artist nor an album has nothing to file it
		// under, and is left top-level rather than filed beneath an invented
		// one — a decision TestScanLooseTrackStaysTopLevel records.
		if g.artist == "" && g.album == "" {
			continue
		}

		artist := g.artist

		/*
		 * Keyed on letters and digits alone.
		 *
		 * `9VoltRevolt` and `9voltRevolt` are one band and were two tiles,
		 * because the key was the raw tag. The same trap as matching an XMLTV
		 * `tvg-id` case-sensitively (ADR 0036): a difference in spelling that
		 * nobody means as a difference in identity. The *display* name is
		 * whichever spelling sorted first, which is deterministic across runs
		 * because the groups are sorted above.
		 *
		 * Case was the first form of that trap and not the last. The same
		 * library also held `t.A.T.u` beside `t.A.T.u.`, `Blut Engel` beside
		 * `Blutengel`, `Box Car Racer` beside `Boxcar Racer`, and — the one
		 * that settles the argument — `alt-J` beside `alt‐J`, which differ only
		 * by U+002D against U+2010 and are *visually identical*. No amount of
		 * care while tagging catches that one. `media.MergeKey` folds all of
		 * them together; it is deliberately not `SortTitle`, which drops
		 * leading articles and would key a band called "The The" as "the".
		 */
		var parent *int64
		if artist != "" {
			key := lib.Path + "::artist=" + media.MergeKey(artist)
			id, ok := artists[key]
			if !ok {
				var err error
				id, err = s.st.EnsureDerivedContainer(ctx, lib.ID, "artist", key,
					artist, media.SortTitle(artist), nil)
				if err != nil {
					return err
				}
				artists[key] = id
			}
			parent = &id
		}

		target := parent
		if g.album != "" {
			// Scoped by artist: "Greatest Hits" is not one album shared by every
			// band that made one.
			key := lib.Path + "::artist=" + media.MergeKey(artist) +
				"::album=" + media.MergeKey(g.album)
			id, ok := albums[key]
			if !ok {
				var err error
				id, err = s.st.EnsureDerivedContainer(ctx, lib.ID, "album", key,
					g.album, media.SortTitle(g.album), parent)
				if err != nil {
					return err
				}
				albums[key] = id
			}
			target = &id
		}

		if err := s.st.SetParent(ctx, g.itemID, target); err != nil {
			return err
		}
	}

	// An album whose files moved away, or an artist whose last album did, is a
	// row LANcast invented with nothing under it — an empty shelf in the grid.
	// An album's own artist and year come from the rows around it, and are
	// re-derived here rather than written once at creation: a rescan that adds a
	// properly tagged track should fix an album that had nothing.
	if _, err := s.st.FillAlbumMetadata(ctx, lib.ID); err != nil {
		s.log.Warn("filling album metadata", "library", lib.ID, "error", err)
	}

	if n, err := s.st.DeleteEmptyMusicContainers(ctx, lib.ID); err != nil {
		s.log.Warn("cleaning empty music containers", "library", lib.ID, "error", err)
	} else if n > 0 {
		s.log.Info("removed empty music containers", "library", lib.ID, "count", n)
	}
	return nil
}
