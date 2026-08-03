package scan

import (
	"context"
	"runtime"
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
		return nil
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		untagged int
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
					mu.Unlock()
					continue
				}
				if err := s.st.ApplyTrackTags(ctx, it.ID, trackTagsFrom(tags)); err != nil {
					s.log.Warn("apply tags", "item", it.ID, "error", err)
				}
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
	return nil
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
