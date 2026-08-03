package scan

import (
	"context"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

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

	tracks, err := s.st.LibraryTracks(ctx, lib.ID)
	if err != nil {
		return err
	}
	if len(tracks) == 0 {
		// No present tracks, but reconciliation still runs: a container emptied
		// by an earlier pass is an empty shelf whether or not there is anything
		// left to read tags from.
		return s.reconcileMusic(ctx, lib, nil)
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		untagged int
		groups   []trackGroup
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
					groups = append(groups, groupFromPath(lib.Path, it))
					mu.Unlock()
					continue
				}
				if err := s.st.ApplyTrackTags(ctx, it.ID, trackTagsFrom(tags)); err != nil {
					s.log.Warn("apply tags", "item", it.ID, "error", err)
				}
				// The grouping key is collected here, while the album artist is
				// still in hand. It is not stored on the track — it belongs to
				// the album, and persisting it per row to read it back one step
				// later would be a column that exists only to survive a function
				// boundary.
				mu.Lock()
				groups = append(groups, groupFromTags(lib.Path, it, tags))
				mu.Unlock()
			}
		}()
	}

	for _, it := range tracks {
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

	if untagged > 0 {
		s.log.Info("tracks without usable tags", "library", lib.ID, "count", untagged,
			"note", "titles and albums for these come from folder and filename")
		s.mu.Lock()
		p.Skipped += untagged
		s.mu.Unlock()
	}

	return s.reconcileMusic(ctx, lib, groups)
}

// trackGroup is one track and the artist and album it belongs under.
type trackGroup struct {
	itemID int64
	artist string
	album  string
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
	if artist == "" || album == "" {
		fallback := groupFromPath(root, it)
		if artist == "" {
			artist = fallback.artist
		}
		if album == "" {
			album = fallback.album
		}
	}
	return trackGroup{itemID: it.ID, artist: artist, album: album}
}

// groupFromPath is the untagged fallback: the containing folder is the album
// and the one above it is the artist.
//
// This is a guess, and a weak one — a real library was laid out as
// `A's/Artist/track.mp3`, where the folder above the track is the artist and
// there is no album folder at all. It is what there is when the file says
// nothing, and it is why tags lead.
func groupFromPath(root string, it store.Item) trackGroup {
	g := trackGroup{itemID: it.ID}
	rel, err := filepath.Rel(root, filepath.Dir(it.Path))
	if err != nil || rel == "." || rel == "" {
		return g
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	g.album = parts[len(parts)-1]
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
		if g.artist == "" && g.album == "" {
			continue
		}

		var parent *int64
		if g.artist != "" {
			key := lib.Path + "::artist=" + g.artist
			id, ok := artists[key]
			if !ok {
				var err error
				id, err = s.st.EnsureMusicContainer(ctx, lib.ID, "artist", key,
					g.artist, media.SortTitle(g.artist), nil)
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
			key := lib.Path + "::artist=" + g.artist + "::album=" + g.album
			id, ok := albums[key]
			if !ok {
				var err error
				id, err = s.st.EnsureMusicContainer(ctx, lib.ID, "album", key,
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
	if n, err := s.st.DeleteEmptyMusicContainers(ctx, lib.ID); err != nil {
		s.log.Warn("cleaning empty music containers", "library", lib.ID, "error", err)
	} else if n > 0 {
		s.log.Info("removed empty music containers", "library", lib.ID, "count", n)
	}
	return nil
}
