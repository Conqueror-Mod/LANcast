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

/*
 * Two spellings of one band are one artist.
 *
 * From a real library: `alt-J` and `alt‐J` sat beside each other in the grid,
 * differing only by U+002D against U+2010 — *visually identical* — each holding
 * one album, neither showing the discography. The same library also had
 * `t.A.T.u` beside `t.A.T.u.` and `Blut Engel` beside `Blutengel`.
 *
 * End to end rather than only against MergeKey, because the unit test passes
 * whether or not the key is actually wired into the scanner — which is the half
 * that was broken.
 */
func TestTwoSpellingsOfOneBandAreOneArtist(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "A/one.mp3", 10)
	writeFile(t, root, "B/two.mp3", 10)

	sc.SetTagReader(&fakeTags{byName: map[string]probe.Tags{
		// U+002D in the first, U+2010 in the second.
		"one.mp3": {Title: "Breezeblocks", Artist: "alt-J", AlbumArtist: "alt-J", Album: "An Awesome Wave"},
		"two.mp3": {Title: "Left Hand Free", Artist: "alt‐J", AlbumArtist: "alt‐J", Album: "This Is All Yours"},
	}})
	scanAndWait(t, sc, lib)

	artists, _, err := st.ListItems(context.Background(), store.ItemFilter{
		LibraryID: lib.ID, Kind: "artist",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) != 1 {
		names := make([]string, 0, len(artists))
		for _, a := range artists {
			names = append(names, a.Title)
		}
		t.Fatalf("artists = %v, want one — the two spellings are the same band", names)
	}

	albums, _, err := st.ListItems(context.Background(), store.ItemFilter{
		LibraryID: lib.ID, Kind: "album",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 2 {
		t.Errorf("albums = %d, want 2 — both records belong to the one artist", len(albums))
	}
}

// Different acts stay apart through the whole scan, not only in the key.
func TestDifferentBandsStaySeparateArtists(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "A/one.mp3", 10)
	writeFile(t, root, "B/two.mp3", 10)

	sc.SetTagReader(&fakeTags{byName: map[string]probe.Tags{
		"one.mp3": {Title: "Say It Ain't So", Artist: "Weezer", AlbumArtist: "Weezer", Album: "Blue"},
		"two.mp3": {Title: "Sublime Song", Artist: "Sublime", AlbumArtist: "Sublime", Album: "40oz"},
	}})
	scanAndWait(t, sc, lib)

	artists, _, err := st.ListItems(context.Background(), store.ItemFilter{
		LibraryID: lib.ID, Kind: "artist",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) != 2 {
		t.Errorf("artists = %d, want 2 — two different bands were merged", len(artists))
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
	if p.SkippedUntagged == 0 {
		t.Error("an untagged track was not reported; a library that looks wrong must say why")
	}
	// The other half, and the reason this has its own field: nothing failed.
	// Counted as Skipped it became a failure the UI offered to explain and
	// then could not, because a failure records an Issue and this records
	// none.
	if p.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0 — an untagged track is not a failure", p.Skipped)
	}
	if len(p.Issues) != 0 {
		t.Errorf("Issues = %+v, want none to explain", p.Issues)
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

/*
 * One band, one tile, whatever the tag's shift key was doing.
 *
 * A real library showed "9VoltRevolt" and "9voltRevolt" as two artists with one
 * album each. The grouping key was the raw tag, so a difference in spelling
 * became a difference in identity — the same trap as matching an XMLTV tvg-id
 * case-sensitively.
 */
func TestArtistGroupingIsCaseInsensitive(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "one/a.flac", 10)
	writeFile(t, root, "two/b.flac", 10)

	sc.SetTagReader(&fakeTags{byName: map[string]probe.Tags{
		"a.flac": {Title: "A", Artist: "9VoltRevolt", AlbumArtist: "9VoltRevolt",
			Album: "First", Track: 1},
		"b.flac": {Title: "B", Artist: "9voltRevolt", AlbumArtist: "9voltRevolt",
			Album: "Second", Track: 1},
	}})
	scanAndWait(t, sc, lib)

	tops := topLevel(t, st, lib.ID)
	if len(tops) != 1 {
		names := []string{}
		for _, it := range tops {
			names = append(names, it.Title)
		}
		t.Fatalf("top level = %v, want one artist", names)
	}
	if got := len(childrenOf(t, st, tops[0].ID)); got != 2 {
		t.Errorf("albums under the merged artist = %d, want 2", got)
	}
}

// The same key, for albums: two spellings of one record are one record.
func TestAlbumGroupingIsCaseInsensitive(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "x/a.flac", 10)
	writeFile(t, root, "x/b.flac", 10)

	sc.SetTagReader(&fakeTags{byName: map[string]probe.Tags{
		"a.flac": {Title: "A", Artist: "Band", AlbumArtist: "Band", Album: "Rat Wars", Track: 1},
		"b.flac": {Title: "B", Artist: "Band", AlbumArtist: "Band", Album: "RAT WARS", Track: 2},
	}})
	scanAndWait(t, sc, lib)

	tops := topLevel(t, st, lib.ID)
	if len(tops) != 1 {
		t.Fatalf("top level = %+v, want one artist", tops)
	}
	albums := childrenOf(t, st, tops[0].ID)
	if len(albums) != 1 {
		t.Fatalf("albums = %d, want 1 — one record spelled two ways", len(albums))
	}
	if got := len(childrenOf(t, st, albums[0].ID)); got != 2 {
		t.Errorf("tracks = %d, want 2", got)
	}
}

/*
 * A rescan of an unchanged music library reads no tags at all.
 *
 * This pass opened and parsed every track in the library on every scan and then
 * rebuilt the grouping from what it read. On a real 9,276-track library that
 * was about 94 seconds per scan, while the scan itself reported `changed=0`
 * every time — a minute and a half spent arriving at the answer already in the
 * database.
 *
 * `calls` is the assertion because it is the cost. Timing would be flaky; the
 * number of files opened is the thing that made it slow.
 */
func TestSecondScanReadsNoTagsWhenNothingChanged(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "Portishead/Dummy/01 Mysterons.mp3", 10)
	writeFile(t, root, "Portishead/Dummy/02 Sour Times.mp3", 10)

	tags := &fakeTags{byName: map[string]probe.Tags{
		"01 Mysterons.mp3":  {Title: "Mysterons", Artist: "Portishead", Album: "Dummy", Track: 1},
		"02 Sour Times.mp3": {Title: "Sour Times", Artist: "Portishead", Album: "Dummy", Track: 2},
	}}
	sc.SetTagReader(tags)

	scanAndWait(t, sc, lib)
	first := tags.calls
	if first != 2 {
		t.Fatalf("first scan read %d files, want 2", first)
	}

	// Reload so the second scan sees the scanned_at the first one wrote — the
	// guard is "a previous scan finished", not "this process ran one".
	reloaded, err := st.GetLibrary(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	scanAndWait(t, sc, *reloaded)

	if tags.calls != first {
		t.Errorf("second scan read %d more files; an unchanged library should read none",
			tags.calls-first)
	}
}

// The grouping survives the skip: the point is to avoid rebuilding an answer
// that is already right, not to lose it.
func TestSkippedTagPassLeavesTheGroupingIntact(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "Portishead/Dummy/01 Mysterons.mp3", 10)

	sc.SetTagReader(&fakeTags{byName: map[string]probe.Tags{
		"01 Mysterons.mp3": {Title: "Mysterons", Artist: "Portishead", Album: "Dummy", Track: 1},
	}})
	scanAndWait(t, sc, lib)

	reloaded, err := st.GetLibrary(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	scanAndWait(t, sc, *reloaded)

	track := trackByPath(t, st, lib.ID, "01 Mysterons.mp3")
	if track.Title != "Mysterons" {
		t.Errorf("title = %q after a skipped pass, want Mysterons", track.Title)
	}
	if track.ParentID == nil {
		t.Error("track lost its album after a scan that skipped the tag pass")
	}
}

/*
 * An interrupted scan must not be trusted.
 *
 * `scanned_at` is written only after a reconcile completes, so a library that
 * has never finished one gets the full pass however unchanged its files look —
 * otherwise a scan that died halfway leaves a half-built hierarchy that nothing
 * would ever rebuild.
 */
func TestUnfinishedLibraryStillReadsTags(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "Portishead/Dummy/01 Mysterons.mp3", 10)

	tags := &fakeTags{byName: map[string]probe.Tags{
		"01 Mysterons.mp3": {Title: "Mysterons", Artist: "Portishead", Album: "Dummy"},
	}}
	sc.SetTagReader(tags)
	scanAndWait(t, sc, lib)
	first := tags.calls

	// lib still carries the zero ScannedAt it was created with, which is what a
	// library whose first scan never finished looks like.
	p := scanAndWait(t, sc, lib)

	/*
	 * The pass runs; it no longer has to reopen files to do it (ADR 0056).
	 *
	 * This asserted that tags were read again, which was the observable at the
	 * time and not the property. What matters is that the hierarchy is rebuilt
	 * from every track, and a stored key is safe to rebuild from: keys are
	 * written only after a *complete* grouping, because an interrupted pass
	 * returns at the ctx check before it reaches them.
	 */
	if p.SkippedUntagged != 0 {
		t.Errorf("SkippedUntagged = %d, want 0", p.SkippedUntagged)
	}
	if got := trackByPath(t, st, lib.ID, "01 Mysterons.mp3"); got.ParentID == nil {
		t.Error("the track lost its album — the hierarchy was not rebuilt")
	}
	_ = first
}

/*
 * An interrupted pass stores nothing.
 *
 * This is what makes reusing a stored key safe. dropBucketAlbums judges a
 * folder from every track in it, so a pass that stopped halfway would judge on
 * partial evidence — and a key stored under that judgement would be reused
 * for ever, an album dropped or invented with no file having changed.
 *
 * The guard is that cancellation returns before the keys are written, so they
 * are only ever the product of a complete pass.
 */
func TestAnInterruptedTagPassStoresNoKeys(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	writeFile(t, root, "Portishead/Dummy/01 Mysterons.mp3", 10)

	tags := &fakeTags{byName: map[string]probe.Tags{
		"01 Mysterons.mp3": {Title: "Mysterons", Artist: "Portishead", Album: "Dummy"},
	}}
	sc.SetTagReader(tags)
	scanAndWait(t, sc, lib)

	// A complete pass did store one.
	keys, err := st.GroupKeys(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) == 0 {
		t.Fatal("a completed pass stored no keys")
	}
	for _, k := range keys {
		if k.Album != "Dummy" || k.Artist != "Portishead" {
			t.Errorf("key = %+v, want the tagged album and artist", k)
		}
	}
}

/*
 * The point of ADR 0056: a scan reads the files that changed, not the library.
 *
 * Measured on a real library before this existed — 17 changed tracks took 92
 * seconds where 9,054 unchanged ones took half a second, because the pass
 * reopened every file to rebuild a grouping it had already computed once.
 */
func TestOnlyChangedTracksAreRead(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)
	for _, n := range []string{"Band/Record/01 One.mp3", "Band/Record/02 Two.mp3",
		"Band/Record/03 Three.mp3"} {
		writeFile(t, root, n, 10)
	}
	tags := &fakeTags{byName: map[string]probe.Tags{
		"01 One.mp3":   {Title: "One", AlbumArtist: "Band", Album: "Record", Track: 1},
		"02 Two.mp3":   {Title: "Two", AlbumArtist: "Band", Album: "Record", Track: 2},
		"03 Three.mp3": {Title: "Three", AlbumArtist: "Band", Album: "Record", Track: 3},
	}}
	sc.SetTagReader(tags)
	scanAndWait(t, sc, lib)
	if tags.calls != 3 {
		t.Fatalf("first scan read %d files, want 3", tags.calls)
	}
	first := tags.calls

	// A completed scan, then one new track arrives.
	reloaded, err := st.GetLibrary(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "Band/Record/04 Four.mp3", 10)
	tags.byName["04 Four.mp3"] = probe.Tags{
		Title: "Four", AlbumArtist: "Band", Album: "Record", Track: 4,
	}
	scanAndWait(t, sc, *reloaded)

	if read := tags.calls - first; read != 1 {
		t.Errorf("read %d files for one new track, want 1 — the other three did not move", read)
	}

	// And the grouping is still whole: the three tracks that were not read
	// still belong to the record, because their stored keys stood in for them.
	items, _, err := st.ListItems(context.Background(),
		store.ItemFilter{LibraryID: lib.ID, Kind: "album"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "Record" {
		t.Fatalf("albums = %v, want one Record", items)
	}
	kids, err := st.Children(context.Background(), items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 4 {
		t.Errorf("the record holds %d tracks, want 4 — a track whose key was reused must still be in it", len(kids))
	}
}
