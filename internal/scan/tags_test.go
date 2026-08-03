package scan

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/probe"
	"lancast/internal/store"
)

// fakeTags answers from a table keyed by filename, so these tests need no
// ffmpeg and no real media.
type fakeTags struct {
	byName map[string]probe.Tags
	err    error
	calls  int
}

func (f *fakeTags) ReadTags(_ context.Context, path string) (probe.Tags, error) {
	f.calls++
	if f.err != nil {
		return probe.Tags{}, f.err
	}
	return f.byName[filepath.Base(path)], nil
}

func trackByPath(t *testing.T, st *store.Store, libID int64, suffix string) store.Item {
	t.Helper()
	items, err := st.LibraryTracks(context.Background(), libID)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if strings.HasSuffix(filepath.ToSlash(it.Path), suffix) {
			return it
		}
	}
	t.Fatalf("no track ending %q; have %d tracks", suffix, len(items))
	return store.Item{}
}

// Tags outrank the filename. The fallback would have called this track
// "01 Whatever The Ripper Called It" with no album, artist, number or year.
func TestScanAppliesTagsOverFilename(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "Bucket/01 Whatever The Ripper Called It.mp3", 10)

	sc.SetTagReader(&fakeTags{byName: map[string]probe.Tags{
		"01 Whatever The Ripper Called It.mp3": {
			Title:       "I Want to Marry a Lighthouse Keeper",
			Artist:      "Erika Eigen",
			AlbumArtist: "Various Artists",
			Album:       "Stanley Kubrick's Clockwork Orange",
			Track:       11,
			Year:        1989,
		},
	}})
	scanAndWait(t, sc, lib)

	it := trackByPath(t, st, lib.ID, "01 Whatever The Ripper Called It.mp3")
	if it.Title != "I Want to Marry a Lighthouse Keeper" {
		t.Errorf("Title = %q — the tag must win over the filename", it.Title)
	}
	if it.Artist == nil || *it.Artist != "Erika Eigen" {
		t.Errorf("Artist = %v — the track's own performer, not the album artist", it.Artist)
	}
	if it.Series == nil || *it.Series != "Stanley Kubrick's Clockwork Orange" {
		t.Errorf("album = %v — the tag, not the folder name", it.Series)
	}
	if it.Episode == nil || *it.Episode != 11 {
		t.Errorf("track = %v, want 11", it.Episode)
	}
	if it.Year == nil || *it.Year != 1989 {
		t.Errorf("Year = %v, want 1989", it.Year)
	}
}

// An untagged file keeps what its folder and filename gave it, and the scan
// says so rather than leaving a library that silently looks wrong.
func TestScanUntaggedTrackKeepsFilenameAndIsReported(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "Portishead/Dummy/03 Strangers.flac", 10)

	sc.SetTagReader(&fakeTags{byName: map[string]probe.Tags{}}) // every file untagged
	p := scanAndWait(t, sc, lib)

	it := trackByPath(t, st, lib.ID, "03 Strangers.flac")
	if it.Title != "Strangers" {
		t.Errorf("Title = %q, want the filename fallback %q", it.Title, "Strangers")
	}
	if it.Episode == nil || *it.Episode != 3 {
		t.Errorf("track = %v, want 3 from the filename", it.Episode)
	}
	if p.Skipped == 0 {
		t.Error("an untagged track was not reported; a library that looks wrong must say why")
	}
}

// Empty tag values must not erase what the filename supplied. A tagger that
// filled in a title but left the album blank should not blank the album.
func TestScanPartialTagsDoNotEraseFallback(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "Aphex Twin/Selected Ambient Works/02 Xtal.flac", 10)

	sc.SetTagReader(&fakeTags{byName: map[string]probe.Tags{
		"02 Xtal.flac": {Title: "Xtal"}, // title only
	}})
	scanAndWait(t, sc, lib)

	it := trackByPath(t, st, lib.ID, "02 Xtal.flac")
	if it.Title != "Xtal" {
		t.Errorf("Title = %q", it.Title)
	}
	if it.Series == nil || *it.Series != "Selected Ambient Works" {
		t.Errorf("album = %v — an empty tag must not erase the folder fallback", it.Series)
	}
	if it.Episode == nil || *it.Episode != 2 {
		t.Errorf("track = %v — an absent track tag must not erase the filename's", it.Episode)
	}
}

// A scan with no tag reader still indexes music; it just cannot improve on the
// filename. A server without ffprobe must not fail to scan.
func TestScanWithoutTagReaderStillIndexes(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "Artist/Album/05 Song.mp3", 10)

	p := scanAndWait(t, sc, lib) // no SetTagReader
	if p.FilesSeen != 1 {
		t.Fatalf("FilesSeen = %d, want 1", p.FilesSeen)
	}
	it := trackByPath(t, st, lib.ID, "05 Song.mp3")
	if it.Title != "Song" {
		t.Errorf("Title = %q, want the filename fallback", it.Title)
	}
}

// A reader that errors on every file is the "ffprobe is broken" case: the scan
// completes, tracks keep their filenames, and nothing fails.
func TestScanSurvivesATagReaderThatErrors(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "Artist/Album/06 Song.mp3", 10)

	sc.SetTagReader(&fakeTags{err: context.DeadlineExceeded})
	p := scanAndWait(t, sc, lib)
	if p.State == StateFailed {
		t.Fatalf("scan failed because tags could not be read: %s", p.Error)
	}
	if it := trackByPath(t, st, lib.ID, "06 Song.mp3"); it.Title != "Song" {
		t.Errorf("Title = %q", it.Title)
	}
}

// The album artist groups the record; the track keeps its own performer. When
// only an album artist is tagged it is used for both, since there is nothing
// better.
func TestTrackTagsPrefersTrackArtistFallsBackToAlbumArtist(t *testing.T) {
	both := trackTagsFrom(probe.Tags{Artist: "Erika Eigen", AlbumArtist: "Various Artists"})
	if both.Artist != "Erika Eigen" {
		t.Errorf("Artist = %q, want the track's own performer", both.Artist)
	}
	only := trackTagsFrom(probe.Tags{AlbumArtist: "Aesop Rock"})
	if only.Artist != "Aesop Rock" {
		t.Errorf("Artist = %q, want the album artist when the track has none", only.Artist)
	}
}
