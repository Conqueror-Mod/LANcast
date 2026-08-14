package scan

import (
	"context"
	"os"
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

// childrenOf returns an item's children, for walking the built hierarchy.
func childrenOf(t *testing.T, st *store.Store, parentID int64) []store.Item {
	t.Helper()
	id := parentID
	items, _, err := st.ListItems(context.Background(), store.ItemFilter{ParentID: &id})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func topLevel(t *testing.T, st *store.Store, libID int64) []store.Item {
	t.Helper()
	items, _, err := st.ListItems(context.Background(),
		store.ItemFilter{LibraryID: libID, TopLevel: true})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

// artist → album → track, the same shape as show → season → episode.
func TestScanBuildsArtistAlbumTrackHierarchy(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "bucket/a.flac", 10)
	writeFile(t, root, "bucket/b.flac", 10)

	sc.SetTagReader(&fakeTags{byName: map[string]probe.Tags{
		"a.flac": {Title: "Mystery Fish", Artist: "Aesop Rock", AlbumArtist: "Aesop Rock",
			Album: "The Impossible Kid", Track: 1},
		"b.flac": {Title: "Rings", Artist: "Aesop Rock", AlbumArtist: "Aesop Rock",
			Album: "The Impossible Kid", Track: 2},
	}})
	scanAndWait(t, sc, lib)

	tops := topLevel(t, st, lib.ID)
	if len(tops) != 1 || tops[0].Kind != "artist" || tops[0].Title != "Aesop Rock" {
		t.Fatalf("top level = %+v, want one artist row", tops)
	}
	albums := childrenOf(t, st, tops[0].ID)
	if len(albums) != 1 || albums[0].Kind != "album" || albums[0].Title != "The Impossible Kid" {
		t.Fatalf("albums = %+v, want one album", albums)
	}
	tracks := childrenOf(t, st, albums[0].ID)
	if len(tracks) != 2 {
		t.Fatalf("tracks = %d, want 2", len(tracks))
	}
	for _, tr := range tracks {
		if tr.Kind != "track" {
			t.Errorf("child kind = %q, want track", tr.Kind)
		}
	}
}

// The trap ADR 0024 names: grouping on the performer shatters a compilation
// into one album per guest. The album artist is what holds it together.
func TestScanGroupsCompilationByAlbumArtist(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	for _, n := range []string{"1.mp3", "2.mp3", "3.mp3"} {
		writeFile(t, root, "soundtrack/"+n, 10)
	}

	sc.SetTagReader(&fakeTags{byName: map[string]probe.Tags{
		"1.mp3": {Title: "Lighthouse Keeper", Artist: "Erika Eigen",
			AlbumArtist: "Various Artists", Album: "A Clockwork Orange", Track: 11},
		"2.mp3": {Title: "March", Artist: "Rachel Elkind",
			AlbumArtist: "Various Artists", Album: "A Clockwork Orange", Track: 5},
		"3.mp3": {Title: "Overture", Artist: "Terry Tucker",
			AlbumArtist: "Various Artists", Album: "A Clockwork Orange", Track: 10},
	}})
	scanAndWait(t, sc, lib)

	tops := topLevel(t, st, lib.ID)
	if len(tops) != 1 {
		t.Fatalf("top level = %d rows, want 1 — grouping on the performer shattered the record", len(tops))
	}
	if tops[0].Title != "Various Artists" {
		t.Errorf("artist = %q, want the album artist", tops[0].Title)
	}
	albums := childrenOf(t, st, tops[0].ID)
	if len(albums) != 1 {
		t.Fatalf("albums = %d, want 1", len(albums))
	}
	if got := len(childrenOf(t, st, albums[0].ID)); got != 3 {
		t.Errorf("tracks under the album = %d, want 3", got)
	}

	// Each track still shows who actually played it.
	for _, tr := range childrenOf(t, st, albums[0].ID) {
		if tr.Artist == nil || *tr.Artist == "Various Artists" {
			t.Errorf("track %q lost its performer: %v", tr.Title, tr.Artist)
		}
	}
}

// "Greatest Hits" is not one album shared by every band that made one.
func TestScanSameAlbumNameUnderDifferentArtists(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "x/a.mp3", 10)
	writeFile(t, root, "y/b.mp3", 10)

	sc.SetTagReader(&fakeTags{byName: map[string]probe.Tags{
		"a.mp3": {Title: "One", AlbumArtist: "Queen", Album: "Greatest Hits", Track: 1},
		"b.mp3": {Title: "Two", AlbumArtist: "Abba", Album: "Greatest Hits", Track: 1},
	}})
	scanAndWait(t, sc, lib)

	tops := topLevel(t, st, lib.ID)
	if len(tops) != 2 {
		t.Fatalf("artists = %d, want 2", len(tops))
	}
	for _, a := range tops {
		albums := childrenOf(t, st, a.ID)
		if len(albums) != 1 {
			t.Errorf("%s has %d albums, want 1 — the two records collided", a.Title, len(albums))
		}
		if got := len(childrenOf(t, st, albums[0].ID)); got != 1 {
			t.Errorf("%s / %s has %d tracks, want 1", a.Title, albums[0].Title, got)
		}
	}
}

// With no tags, the folders are the only evidence there is.
func TestScanUntaggedGroupsByFolders(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "Portishead/Dummy/01 Mysterons.flac", 10)
	writeFile(t, root, "Portishead/Dummy/02 Sour Times.flac", 10)

	sc.SetTagReader(&fakeTags{byName: map[string]probe.Tags{}})
	scanAndWait(t, sc, lib)

	tops := topLevel(t, st, lib.ID)
	if len(tops) != 1 || tops[0].Title != "Portishead" {
		t.Fatalf("top level = %+v, want the artist folder", tops)
	}
	albums := childrenOf(t, st, tops[0].ID)
	if len(albums) != 1 || albums[0].Title != "Dummy" {
		t.Fatalf("albums = %+v, want the album folder", albums)
	}
	if got := len(childrenOf(t, st, albums[0].ID)); got != 2 {
		t.Errorf("tracks = %d, want 2", got)
	}
}

// A track loose in the library root has no artist and no album to file it
// under, and is left top-level rather than filed beneath an invented one.
func TestScanLooseTrackStaysTopLevel(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "stray.mp3", 10)

	sc.SetTagReader(&fakeTags{byName: map[string]probe.Tags{}})
	scanAndWait(t, sc, lib)

	tops := topLevel(t, st, lib.ID)
	if len(tops) != 1 || tops[0].Kind != "track" {
		t.Fatalf("top level = %+v, want the loose track itself", tops)
	}
}

// A vanished file marks its track missing — the row stays, and so do the
// containers above it. "Scanning marks missing, never deletes" is what protects
// a library when a drive is unmounted, and an album that emptied itself the
// moment a disk went offline would be exactly the destruction that rule exists
// to prevent.
func TestRescanKeepsContainersWhenAFileGoesMissing(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	path := writeFile(t, root, "bucket/a.mp3", 10)

	sc.SetTagReader(&fakeTags{byName: map[string]probe.Tags{
		"a.mp3": {Title: "Only Song", AlbumArtist: "Lone Band", Album: "Only Album", Track: 1},
	}})
	scanAndWait(t, sc, lib)

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	p := scanAndWait(t, sc, lib)
	if p.ItemsMissing != 1 {
		t.Errorf("ItemsMissing = %d, want 1", p.ItemsMissing)
	}

	for _, kind := range []string{"artist", "album"} {
		items, _, err := st.ListItems(context.Background(),
			store.ItemFilter{LibraryID: lib.ID, Kind: kind})
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 {
			t.Errorf("%s rows = %d, want 1 — a missing file must not destroy the shelf above it", kind, len(items))
		}
	}
}

// A container with genuinely nothing under it is a row LANcast invented, not a
// file — an empty shelf in the grid. Retagging a track onto a different album
// leaves the old one truly childless, and that is what gets removed.
func TestRescanRemovesTrulyEmptiedContainers(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "bucket/a.mp3", 10)

	reader := &fakeTags{byName: map[string]probe.Tags{
		"a.mp3": {Title: "Song", AlbumArtist: "Lone Band", Album: "First Album", Track: 1},
	}}
	sc.SetTagReader(reader)
	scanAndWait(t, sc, lib)

	albumTitles := func() []string {
		items, _, err := st.ListItems(context.Background(),
			store.ItemFilter{LibraryID: lib.ID, Kind: "album"})
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, it := range items {
			out = append(out, it.Title)
		}
		return out
	}
	if got := albumTitles(); len(got) != 1 || got[0] != "First Album" {
		t.Fatalf("albums = %v, want [First Album]", got)
	}

	// The file is retagged onto a different record, so nothing is left under
	// the first one.
	reader.byName["a.mp3"] = probe.Tags{
		Title: "Song", AlbumArtist: "Lone Band", Album: "Second Album", Track: 1,
	}
	scanAndWait(t, sc, lib)

	if got := albumTitles(); len(got) != 1 || got[0] != "Second Album" {
		t.Errorf("albums = %v, want only [Second Album] — the emptied one is an empty shelf", got)
	}
}

/*
 * A track loose at a second location's own root has no album folder.
 *
 * Deliberately the *loose* case rather than a nested one. Nested, the bug is
 * invisible when both locations share a volume: filepath.Rel succeeds, produces
 * "../../second/Portishead/Dummy", and the last two components are the right
 * artist and album by luck. Loose at the root it cannot hide — the correct root
 * gives Rel "." and no album, while the wrong one gives "../../second" and an
 * album named after the other drive's own folder (ADR 0034).
 */
func TestTrackLooseInASecondLocationGetsNoFolderAlbum(t *testing.T) {
	sc, st := newScanner(t)
	lib, _ := musicFixture(t, st)

	second := t.TempDir()
	if _, err := st.AddRoot(context.Background(), lib.ID, second); err != nil {
		t.Fatal(err)
	}
	writeFile(t, second, "03 Strangers.flac", 10)

	sc.SetTagReader(&fakeTags{byName: map[string]probe.Tags{}})
	scanAndWait(t, sc, lib)

	// Asserted on the album *container*, which is where the damage lands. The
	// track's own Series stays nil either way, so a track-level assertion would
	// pass against the bug — the wrong root produces a real album row named
	// after the other location's folder, with this track parented to it.
	albums, _, err := st.ListItems(context.Background(),
		store.ItemFilter{LibraryID: lib.ID, Kind: "album"})
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 0 {
		var names []string
		for _, a := range albums {
			names = append(names, a.Title)
		}
		t.Errorf("albums = %v, want none — a track at a location's root has no album "+
			"folder, and these names come from relativising against the wrong location", names)
	}
}

// And the ordinary nested case still works from a second location.
func TestUntaggedTrackInASecondLocationKeepsItsFolderAlbum(t *testing.T) {
	sc, st := newScanner(t)
	lib, _ := musicFixture(t, st)

	second := t.TempDir()
	if _, err := st.AddRoot(context.Background(), lib.ID, second); err != nil {
		t.Fatal(err)
	}
	writeFile(t, second, "Portishead/Dummy/03 Strangers.flac", 10)

	sc.SetTagReader(&fakeTags{byName: map[string]probe.Tags{}})
	scanAndWait(t, sc, lib)

	it := trackByPath(t, st, lib.ID, "03 Strangers.flac")
	if it.Series == nil || *it.Series != "Dummy" {
		t.Errorf("album = %v, want Dummy from the folder in the second location", it.Series)
	}
}
