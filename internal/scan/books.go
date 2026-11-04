package scan

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"lancast/internal/media"
	"lancast/internal/store"
)

// reconcileBooks makes books out of an audiobooks library's files (ADR 0116).
//
// Which files are one book, and what it is called, is internal/media's
// judgement (GroupBooks); this pass only gathers what that judgement needs —
// each part's tags — and writes the answer down. Every book is a `book` row
// over its `book_part` files, keyed by its folder, so a rescan finds the same
// book again and a part moved between folders follows the folders.
//
// Tags are read again only when something on disk moved, the rule the music
// pass follows and for the same reason: tags live inside the files, and the
// walk has just shown none of them changed.
func (s *Scanner) reconcileBooks(ctx context.Context, lib store.Library, p *Progress) error {
	if lib.ScannedAt != nil && p != nil && p.ItemsChanged == 0 && p.ItemsMissing == 0 {
		s.log.Debug("skipping book pass; nothing changed since the last scan", "library", lib.ID)
		return nil
	}
	start := time.Now()

	parts, err := s.st.LibraryBookParts(ctx, lib.ID)
	if err != nil {
		return err
	}
	roots, err := s.st.RootPaths(ctx, lib.ID)
	if err != nil {
		return err
	}

	s.mu.Lock()
	reader := s.tags
	s.mu.Unlock()

	// Each part's tags, read concurrently: the cost is a process per file.
	files := make([]media.BookFile, len(parts))
	for i, it := range parts {
		files[i] = media.BookFile{Path: it.Path}
	}
	if reader != nil {
		var wg sync.WaitGroup
		work := make(chan int)
		for w := 0; w < tagConcurrency(); w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range work {
					t, err := reader.ReadTags(ctx, files[i].Path)
					if err != nil {
						// A part with unreadable tags still belongs to its
						// folder's book; it just tells the book nothing.
						continue
					}
					files[i].Tags = media.BookTags{
						Title: t.Title, Artist: t.Artist, Album: t.Album,
						AlbumArtist: t.AlbumArtist, Track: t.Track, Year: t.Year,
					}
				}
			}()
		}
	feed:
		for i := range files {
			select {
			case <-ctx.Done():
				break feed
			case work <- i:
			}
		}
		close(work)
		wg.Wait()
		if err := ctx.Err(); err != nil {
			return err
		}
	}

	// Grouped per library location: a folder's place in its series is
	// relative to the location it sits in (ADR 0034).
	byRoot := map[string][]media.BookFile{}
	ids := map[string]int64{}
	for i, it := range parts {
		root := rootOf(roots, it.RootID)
		if root == "" {
			continue
		}
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
		byRoot[root] = append(byRoot[root], files[i])
		ids[it.Path] = it.ID
	}

	books := 0
	for root, group := range byRoot {
		for _, b := range media.GroupBooks(root, group) {
			id, err := s.st.EnsureDerivedContainer(ctx, lib.ID, string(media.KindBook),
				b.Key, b.Title, media.SortTitle(b.Title), nil)
			if err != nil {
				return err
			}
			if err := s.st.SetBookDetails(ctx, id, store.BookDetails{
				Title: b.Title, SortTitle: media.SortTitle(b.Title), Author: b.Author,
				Series: b.Series, Number: b.Number, Edition: b.Edition, Year: b.Year,
			}); err != nil {
				return err
			}
			for _, part := range b.Parts {
				pid, ok := ids[part.Path]
				if !ok {
					continue
				}
				if err := s.st.SetBookPart(ctx, pid, id, part.Number,
					part.Title, media.SortTitle(part.Title)); err != nil {
					return err
				}
			}
			books++
		}
	}
	s.log.Info("book pass", "library", lib.ID, "parts", len(parts), "books", books,
		"tags_read", reader != nil, "ms", time.Since(start).Milliseconds())
	return nil
}
