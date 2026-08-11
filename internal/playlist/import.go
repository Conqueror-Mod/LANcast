package playlist

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Store is the slice of the database an import needs.
//
// An interface rather than *store.Store so the import logic can be tested
// against a fake with no database at all — the interesting behaviour here is
// which lines resolve and what is reported about the ones that do not, and none
// of that is about SQL.
type Store interface {
	ItemIDByPath(ctx context.Context, path string) (int64, error)
	EnsurePlaylist(ctx context.Context, libraryID int64, path, title, sortTitle string) (int64, error)
	SetPlaylistEntries(ctx context.Context, playlistID int64, itemIDs []int64) error
}

// Result is what an import did, and what it could not do.
//
// Missing is carried rather than logged and forgotten: ADR 0030 requires an
// import to say "47 of 52, and here are the five", because silently importing
// 47 produces a playlist that looks complete and is not. Nobody ever discovers
// which five are absent from a log line written last Tuesday.
type Result struct {
	PlaylistID int64
	Title      string
	Imported   int
	// Missing holds the paths, as written in the file, that matched nothing in
	// the library. Capped — see maxReported.
	Missing []string
	// MissingCount is the true total, which may exceed len(Missing).
	MissingCount int
	// Skipped counts lines that are not local files at all: URLs, mostly. Not
	// "missing", because they were never going to be here — reporting a radio
	// stream as a file we could not find would send someone looking for it.
	Skipped int
}

// A playlist pointing at a whole library that has moved produces thousands of
// missing entries, and a report is not improved by listing all of them.
const maxReported = 20

// ImportFile reads one .m3u and writes it into the database as a playlist.
//
// The playlist is keyed by the .m3u's own path, so re-importing the same file
// updates that playlist rather than making another. Membership is replaced
// wholesale, which is the ADR's "an .m3u seeds a playlist" and not "an .m3u is
// the playlist" — see the caller, which is responsible for not re-importing a
// playlist a human has since edited.
func ImportFile(ctx context.Context, st Store, libraryID int64, m3uPath string) (Result, error) {
	f, err := os.Open(m3uPath)
	if err != nil {
		return Result{}, fmt.Errorf("open playlist %q: %w", m3uPath, err)
	}
	defer f.Close()

	entries, err := Parse(f)
	if err != nil {
		// Includes ErrHLS, which the caller treats as "not a media playlist"
		// rather than as a failure worth reporting to a user.
		return Result{}, fmt.Errorf("parse %q: %w", m3uPath, err)
	}

	base := filepath.Dir(m3uPath)
	res := Result{Title: titleFor(m3uPath)}

	ids := make([]int64, 0, len(entries))
	for _, e := range entries {
		abs, ok := Resolve(base, e.Path)
		if !ok {
			res.Skipped++
			continue
		}
		id, err := st.ItemIDByPath(ctx, abs)
		if err != nil {
			return Result{}, err
		}
		if id == 0 {
			res.MissingCount++
			if len(res.Missing) < maxReported {
				res.Missing = append(res.Missing, e.Path)
			}
			continue
		}
		ids = append(ids, id)
	}

	// A playlist whose every line is missing is still made, empty, on purpose.
	// The alternative is a scan that finds a file called "Road Trip.m3u",
	// imports nothing, says nothing, and leaves someone wondering why their
	// playlist never appeared — the same failure as the silent kind-mismatch
	// scan. An empty playlist with a count of what could not be found is a
	// question a person can answer.
	pid, err := st.EnsurePlaylist(ctx, libraryID, m3uPath, res.Title, sortTitle(res.Title))
	if err != nil {
		return Result{}, err
	}
	if err := st.SetPlaylistEntries(ctx, pid, ids); err != nil {
		return Result{}, err
	}

	res.PlaylistID = pid
	res.Imported = len(ids)
	return res, nil
}

// titleFor names a playlist after its file, minus the extension.
//
// The file name is all there is. #PLAYLIST is a real extended-M3U tag and is
// vanishingly rare in the wild; using it when present would mean two playlists
// with the same name and different files, which is worse than a name that
// matches what is on disk.
func titleFor(m3uPath string) string {
	name := filepath.Base(m3uPath)
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// sortTitle is the local, deliberately minimal normalisation for a playlist
// name. It is NOT media.SortTitle: that strips leading articles, which is right
// for "The Godfather" and wrong for a playlist someone named "The Gym One" and
// expects to find under T.
func sortTitle(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
