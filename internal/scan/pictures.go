package scan

import (
	"context"
	"path/filepath"

	"lancast/internal/media"
	"lancast/internal/store"
)

// reconcilePictures groups photos into galleries by the folder they sit in
// (ADR 0028).
//
// This is the simplest of the three hierarchies in the scanner, and
// deliberately so. Shows are inferred from filename patterns and music from
// embedded tags, because in both cases the folder might be lying. A picture
// library has nothing else to go on: the filename is a UUID and there is no
// provider to ask, so the folder is not a hint about the grouping — it *is* the
// grouping, and taking it literally is the honest reading rather than a
// shortcut.
//
// Galleries do not nest. A photo's gallery is its immediate parent directory
// whatever the depth, so a tree three deep produces three sibling galleries
// rather than a hierarchy. Nesting would need a rule for what an intermediate
// folder means when it holds both photos and folders, and there is no answer to
// that which is right more often than it is wrong.
//
// A photo sitting directly in the library root gets no gallery. It is already
// top-level, and wrapping the root in a gallery named after the library would
// add a level of navigation that contains everything and separates nothing.
func (s *Scanner) reconcilePictures(ctx context.Context, lib store.Library) error {
	// Unpaged and ordered in the query: reconciliation must see the whole
	// library at once, and a stable order keeps gallery ids predictable across
	// runs, which makes the diff of a rescan boring.
	photos, err := s.st.LibraryPhotoFiles(ctx, lib.ID)
	if err != nil {
		return err
	}

	// Compared against each photo's own location (ADR 0034). Against the
	// library's first one, a photo sitting loose in a *second* location never
	// matches, so instead of staying top-level it is grouped into a gallery
	// named after that drive's folder.
	roots, err := s.st.RootPaths(ctx, lib.ID)
	if err != nil {
		return err
	}

	galleries := map[string]int64{}
	for _, ph := range photos {
		root := rootOf(roots, ph.RootID)
		if root == "" {
			continue
		}
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
		dir := filepath.Dir(ph.Path)
		if abs, err := filepath.Abs(dir); err == nil && abs == root {
			// Loose at the root: already top-level, nothing to group it under.
			if ph.ParentID != nil {
				if err := s.st.SetParent(ctx, ph.ID, nil); err != nil {
					return err
				}
			}
			continue
		}

		id, ok := galleries[dir]
		if !ok {
			name := filepath.Base(dir)
			id, err = s.st.EnsureDerivedContainer(ctx, lib.ID, string(media.KindGallery),
				dir, name, media.SortTitle(name), nil)
			if err != nil {
				return err
			}
			galleries[dir] = id
		}
		if ph.ParentID == nil || *ph.ParentID != id {
			if err := s.st.SetParent(ctx, ph.ID, &id); err != nil {
				return err
			}
		}
	}
	return nil
}
