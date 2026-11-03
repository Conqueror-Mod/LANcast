package store

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

/*
 * Music genres and the Albums/Tracks levels of a music library (ADR 0114).
 *
 * Reported as a music library with no genre or year filter. Nothing in one had
 * a genre: the tag was read from every file and dropped. The years were there
 * on albums and tracks, but the facets only ever looked at artists, which have
 * none, so every filter hid itself.
 */

type musicFixture struct {
	st                 *Store
	lib                int64
	artist, album      int64
	other, otherArtist int64
	t1, t2, t3         int64
}

func newMusicFixture(t *testing.T) musicFixture {
	t.Helper()
	ctx := context.Background()
	st := queueStore(t)
	lib, err := st.CreateLibrary(ctx, "Music", "music", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := musicFixture{st: st, lib: lib.ID}
	mk := func(kind, key, title string, parent *int64) int64 {
		id, err := st.EnsureDerivedContainer(ctx, lib.ID, kind, key, title, title, parent)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.artist = mk("artist", "k::artist=Band", "Band", nil)
	f.album = mk("album", "k::album=Record", "Record", &f.artist)
	f.otherArtist = mk("artist", "k::artist=Other", "Other", nil)
	f.other = mk("album", "k::album=Later", "Later", &f.otherArtist)
	track := func(name string, parent int64, tags TrackTags) int64 {
		id := seedPending(t, st, lib.ID, "track", filepath.Join("m", name), name)
		if err := st.SetParent(ctx, id, &parent); err != nil {
			t.Fatal(err)
		}
		if err := st.ApplyTrackTags(ctx, id, tags); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.t1 = track("one.flac", f.album, TrackTags{Title: "One", SortTitle: "one", Year: 1994, Genres: []string{"Jazz"}})
	f.t2 = track("two.flac", f.album, TrackTags{Title: "Two", SortTitle: "two", Year: 1994, Genres: []string{"Jazz", "Soul"}})
	f.t3 = track("three.flac", f.other, TrackTags{Title: "Three", SortTitle: "three", Year: 2003, Genres: []string{"Rock"}})
	if _, err := st.FillAlbumMetadata(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.RollUpMusicGenres(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	return f
}

func genresOf(t *testing.T, st *Store, id int64) []string {
	t.Helper()
	g, err := st.Genres(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestATracksGenreTagIsKept(t *testing.T) {
	f := newMusicFixture(t)
	if got := genresOf(t, f.st, f.t2); !slices.Equal(got, []string{"Jazz", "Soul"}) {
		t.Errorf("track genres = %v, want [Jazz Soul]", got)
	}
}

// An empty tag leaves the genre alone, as an empty title does.
func TestAnEmptyGenreTagDoesNotEraseOne(t *testing.T) {
	f := newMusicFixture(t)
	if err := f.st.ApplyTrackTags(context.Background(), f.t1, TrackTags{Title: "One"}); err != nil {
		t.Fatal(err)
	}
	if got := genresOf(t, f.st, f.t1); !slices.Equal(got, []string{"Jazz"}) {
		t.Errorf("genres = %v, want Jazz kept", got)
	}
}

// An edited genre is locked, and a rescan reading the file must not undo it.
func TestALockedGenreIsNotRetagged(t *testing.T) {
	f := newMusicFixture(t)
	ctx := context.Background()
	if err := f.st.ReplaceGenres(ctx, f.t1, []string{"Bebop"}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.LockField(ctx, f.t1, "genres"); err != nil {
		t.Fatal(err)
	}
	if err := f.st.ApplyTrackTags(ctx, f.t1, TrackTags{Genres: []string{"Jazz"}}); err != nil {
		t.Fatal(err)
	}
	if got := genresOf(t, f.st, f.t1); !slices.Equal(got, []string{"Bebop"}) {
		t.Errorf("genres = %v, want the locked Bebop", got)
	}
}

// "rock" on one record and "Rock" on another is one genre, not two chips.
func TestATagsGenreJoinsTheSameNameInAnyCase(t *testing.T) {
	f := newMusicFixture(t)
	if err := f.st.ApplyTrackTags(context.Background(), f.t1, TrackTags{Genres: []string{"rock"}}); err != nil {
		t.Fatal(err)
	}
	if got := genresOf(t, f.st, f.t1); !slices.Equal(got, []string{"Rock"}) {
		t.Errorf("genres = %v, want the existing spelling Rock", got)
	}
}

func TestAlbumsAndArtistsGetTheirTracksGenres(t *testing.T) {
	f := newMusicFixture(t)
	if got := genresOf(t, f.st, f.album); !slices.Equal(got, []string{"Jazz", "Soul"}) {
		t.Errorf("album genres = %v, want [Jazz Soul]", got)
	}
	if got := genresOf(t, f.st, f.artist); !slices.Equal(got, []string{"Jazz", "Soul"}) {
		t.Errorf("artist genres = %v, want [Jazz Soul]", got)
	}
	if got := genresOf(t, f.st, f.otherArtist); !slices.Equal(got, []string{"Rock"}) {
		t.Errorf("other artist genres = %v, want [Rock]", got)
	}
}

// A retag reaches the album on the next pass, and an album edited by hand does not.
func TestTheRollUpFollowsARetagAndLeavesALockedAlbum(t *testing.T) {
	f := newMusicFixture(t)
	ctx := context.Background()
	if err := f.st.ReplaceGenres(ctx, f.other, []string{"Mine"}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.LockField(ctx, f.other, "genres"); err != nil {
		t.Fatal(err)
	}
	if err := f.st.ApplyTrackTags(ctx, f.t1, TrackTags{Genres: []string{"Blues"}}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.ApplyTrackTags(ctx, f.t2, TrackTags{Genres: []string{"Blues"}}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.RollUpMusicGenres(ctx, f.lib); err != nil {
		t.Fatal(err)
	}
	if got := genresOf(t, f.st, f.album); !slices.Equal(got, []string{"Blues"}) {
		t.Errorf("album genres = %v, want the retag Blues", got)
	}
	if got := genresOf(t, f.st, f.other); !slices.Equal(got, []string{"Mine"}) {
		t.Errorf("locked album genres = %v, want Mine kept", got)
	}
	// Its artist is built from the album as it stands, edit and all.
	if got := genresOf(t, f.st, f.otherArtist); !slices.Equal(got, []string{"Mine"}) {
		t.Errorf("other artist genres = %v, want Mine", got)
	}
}

func TestFacetsAtEachLevelOfAMusicLibrary(t *testing.T) {
	f := newMusicFixture(t)
	ctx := context.Background()

	top, err := f.st.LibraryFacetsAt(ctx, f.lib, "u1", "")
	if err != nil {
		t.Fatal(err)
	}
	// Artists: genres from the roll-up, still no years (an artist has none).
	if !slices.Equal(top.Genres, []string{"Jazz", "Rock", "Soul"}) || len(top.Years) != 0 {
		t.Errorf("artist level: genres %v years %v", top.Genres, top.Years)
	}

	albums, err := f.st.LibraryFacetsAt(ctx, f.lib, "u1", "album")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(albums.Years, []int{2003, 1994}) || !slices.Equal(albums.Decades, []int{2000, 1990}) {
		t.Errorf("album level: years %v decades %v", albums.Years, albums.Decades)
	}
	if !slices.Equal(albums.Genres, []string{"Jazz", "Rock", "Soul"}) {
		t.Errorf("album level: genres %v", albums.Genres)
	}
	if !slices.Equal(albums.Initials, []string{"L", "R"}) {
		t.Errorf("album level: initials %v, want the albums' own", albums.Initials)
	}

	tracks, err := f.st.LibraryFacetsAt(ctx, f.lib, "u1", "track")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tracks.Initials, []string{"O", "T"}) || !slices.Equal(tracks.Years, []int{2003, 1994}) {
		t.Errorf("track level: initials %v years %v", tracks.Initials, tracks.Years)
	}
}

// The listing the facets describe finds what they offer.
func TestAnAlbumsListingFiltersByGenreAndYear(t *testing.T) {
	f := newMusicFixture(t)
	ctx := context.Background()
	items, _, err := f.st.ListItems(ctx, ItemFilter{LibraryID: f.lib, Kind: "album", Genres: []string{"Soul"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != f.album {
		t.Errorf("Soul albums = %+v, want Record", items)
	}
	items, _, err = f.st.ListItems(ctx, ItemFilter{LibraryID: f.lib, Kind: "track", Years: []int{2003}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != f.t3 {
		t.Errorf("2003 tracks = %+v, want Three", items)
	}
}

func TestFacetsRefuseAnUnknownLevel(t *testing.T) {
	f := newMusicFixture(t)
	if _, err := f.st.LibraryFacetsAt(context.Background(), f.lib, "u1", "movie' OR 1=1 --"); !errors.Is(err, ErrFacetKind) {
		t.Errorf("err = %v, want ErrFacetKind", err)
	}
}
