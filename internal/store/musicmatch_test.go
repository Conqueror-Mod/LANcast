package store

import (
	"context"
	"slices"
	"testing"
)

/*
 * MusicBrainz matches on albums and artists (ADR 0115): they wait in the
 * queue only while matching is on, a match adds an identity and fills only
 * what is empty, and an artist is identified by its albums' credits.
 */

func itemRow(t *testing.T, st *Store, id int64) Item {
	t.Helper()
	it, err := st.GetItem(context.Background(), id, "")
	if err != nil {
		t.Fatal(err)
	}
	return *it
}

// Albums and artists used to be born "matched" and stamped, so nothing ever
// matched them. Now they are born waiting; a gallery still is not.
func TestAlbumsAreBornWaiting(t *testing.T) {
	f := newMusicFixture(t)
	al := itemRow(t, f.st, f.album)
	if al.MatchState != "unmatched" || stamped(t, f.st, f.album) {
		t.Errorf("album born %s, stamped %v; want unmatched and waiting", al.MatchState, stamped(t, f.st, f.album))
	}
	ctx := context.Background()
	lib, err := f.st.CreateLibrary(ctx, "Pictures", "picture", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := f.st.EnsureDerivedContainer(ctx, lib.ID, "gallery", "k::gallery=Trip", "Trip", "trip", nil)
	if err != nil {
		t.Fatal(err)
	}
	if itemRow(t, f.st, g).MatchState != "matched" || !stamped(t, f.st, g) {
		t.Error("a gallery should still be born settled")
	}
}

// Off means out of the queue and out of its count, never "pending forever".
func TestTheQueueFollowsTheMusicSwitch(t *testing.T) {
	f := newMusicFixture(t)
	ctx := context.Background()
	count := func() int {
		n, err := f.st.PendingCount(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	off := count()
	f.st.SetMusicMatching(true)
	on := count()
	// Two artists and two albums join; the three tracks never do.
	if on-off != 4 {
		t.Errorf("pending off %d, on %d; want 4 more with matching on", off, on)
	}
	items, err := f.st.PendingEnrichment(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Kind == "track" {
			t.Errorf("track %d queued", it.ID)
		}
	}
}

func TestAMatchFillsOnlyWhatIsEmpty(t *testing.T) {
	f := newMusicFixture(t)
	ctx := context.Background()
	// The album has 1994 from its tracks and Jazz/Soul from their tags.
	if err := f.st.ApplyAlbumMatch(ctx, f.album, AlbumMatch{
		Provider: "musicbrainz", ExternalID: "rg-1", State: "matched", Score: 0.97,
		ReleaseType: "Album", ArtistID: "a-1", Year: 1993, Genres: []string{"Hip Hop"},
	}); err != nil {
		t.Fatal(err)
	}
	al := itemRow(t, f.st, f.album)
	if al.Year == nil || *al.Year != 1994 {
		t.Errorf("year = %v; the tags' 1994 must stand", al.Year)
	}
	if g := genresOf(t, f.st, f.album); !slices.Equal(g, []string{"Jazz", "Soul"}) {
		t.Errorf("genres = %v; the tags' must stand", g)
	}
	if al.MatchState != "matched" || al.Provider == nil || *al.Provider != "musicbrainz" || !stamped(t, f.st, f.album) {
		t.Errorf("identity not written: %+v", al)
	}
	var rt, aid string
	f.st.db.QueryRow(`SELECT release_type, artist_external_id FROM media_item WHERE id = ?`, f.album).Scan(&rt, &aid)
	if rt != "Album" || aid != "a-1" {
		t.Errorf("release type %q, artist %q", rt, aid)
	}

	// An album with no year and no genres gets both.
	bare, err := f.st.EnsureDerivedContainer(ctx, f.lib, "album", "k::album=Bare", "Bare", "bare", &f.artist)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.ApplyAlbumMatch(ctx, bare, AlbumMatch{Provider: "musicbrainz", ExternalID: "rg-2",
		State: "review", Score: 0.7, Year: 1998, Genres: []string{"Hip Hop", "Turntablism"}}); err != nil {
		t.Fatal(err)
	}
	b := itemRow(t, f.st, bare)
	if b.Year == nil || *b.Year != 1998 || b.MatchState != "review" {
		t.Errorf("bare album: year %v, state %s", b.Year, b.MatchState)
	}
	if g := genresOf(t, f.st, bare); !slices.Equal(g, []string{"Hip Hop", "Turntablism"}) {
		t.Errorf("bare album genres = %v", g)
	}
}

func TestAMatchLeavesLocksAlone(t *testing.T) {
	f := newMusicFixture(t)
	ctx := context.Background()
	bare, _ := f.st.EnsureDerivedContainer(ctx, f.lib, "album", "k::album=Bare", "Bare", "bare", &f.artist)
	f.st.LockField(ctx, bare, "year")
	f.st.LockField(ctx, bare, "genres")
	f.st.ApplyAlbumMatch(ctx, bare, AlbumMatch{Provider: "musicbrainz", ExternalID: "rg-2", State: "matched",
		Score: 1, Year: 1998, Genres: []string{"Hip Hop"}})
	b := itemRow(t, f.st, bare)
	if b.Year != nil && *b.Year != 0 {
		t.Errorf("locked year filled: %v", *b.Year)
	}
	if g := genresOf(t, f.st, bare); len(g) != 0 {
		t.Errorf("locked genres filled: %v", g)
	}

	// A locked album is not touched at all.
	f.st.db.Exec(`UPDATE media_item SET match_state = 'locked' WHERE id = ?`, f.other)
	f.st.ApplyAlbumMatch(ctx, f.other, AlbumMatch{Provider: "musicbrainz", ExternalID: "rg-x", State: "matched", Score: 1})
	f.st.MarkMusicUnmatched(ctx, f.other, 0.1)
	if o := itemRow(t, f.st, f.other); o.MatchState != "locked" || o.Provider != nil && *o.Provider == "musicbrainz" {
		t.Errorf("locked album changed: %s %v", o.MatchState, o.Provider)
	}
}

// An artist is the one most of its confidently matched albums credit; a
// review-band album does not vote, and unseen albums are counted as pending.
func TestArtistCreditVotes(t *testing.T) {
	f := newMusicFixture(t)
	ctx := context.Background()
	third, _ := f.st.EnsureDerivedContainer(ctx, f.lib, "album", "k::album=Third", "Third", "third", &f.artist)
	fourth, _ := f.st.EnsureDerivedContainer(ctx, f.lib, "album", "k::album=Fourth", "Fourth", "fourth", &f.artist)
	f.st.ApplyAlbumMatch(ctx, f.album, AlbumMatch{Provider: "musicbrainz", ExternalID: "rg-1", State: "matched", Score: 1, ArtistID: "band"})
	f.st.ApplyAlbumMatch(ctx, third, AlbumMatch{Provider: "musicbrainz", ExternalID: "rg-3", State: "review", Score: 0.6, ArtistID: "other"})
	votes, pending, err := f.st.ArtistCreditVotes(ctx, f.artist)
	if err != nil {
		t.Fatal(err)
	}
	if votes["band"] != 1 || votes["other"] != 0 || pending != 1 {
		t.Errorf("votes %v pending %d; want band 1, pending 1 (%d)", votes, pending, fourth)
	}
}

// Genres MusicBrainz filled survive a scan's roll-up when the tracks carry
// none, and give way the moment a track is tagged.
func TestTheRollUpKeepsFilledGenresUntilTheTagsSpeak(t *testing.T) {
	f := newMusicFixture(t)
	ctx := context.Background()
	bare, _ := f.st.EnsureDerivedContainer(ctx, f.lib, "album", "k::album=Bare", "Bare", "bare", &f.artist)
	tr := seedPending(t, f.st, f.lib, "track", "m/bare.flac", "Bare Track")
	f.st.SetParent(ctx, tr, &bare)
	f.st.ApplyAlbumMatch(ctx, bare, AlbumMatch{Provider: "musicbrainz", ExternalID: "rg-2", State: "matched", Score: 1, Genres: []string{"Hip Hop"}})
	if err := f.st.RollUpMusicGenres(ctx, f.lib); err != nil {
		t.Fatal(err)
	}
	if g := genresOf(t, f.st, bare); !slices.Equal(g, []string{"Hip Hop"}) {
		t.Errorf("after roll-up = %v; the filled genre was erased", g)
	}
	f.st.ApplyTrackTags(ctx, tr, TrackTags{Genres: []string{"Trip Hop"}})
	f.st.RollUpMusicGenres(ctx, f.lib)
	if g := genresOf(t, f.st, bare); !slices.Equal(g, []string{"Trip Hop"}) {
		t.Errorf("after tagging = %v; the tag should take over", g)
	}
}

// Until Fix match learns albums (step 3), an album is neither reviewed nor
// re-parsed: the one would search films for it, the other guess a video name.
func TestAlbumsStayOutOfReviewAndReparse(t *testing.T) {
	f := newMusicFixture(t)
	ctx := context.Background()
	f.st.ApplyAlbumMatch(ctx, f.album, AlbumMatch{Provider: "musicbrainz", ExternalID: "rg-1", State: "review", Score: 0.6})
	f.st.MarkMusicUnmatched(ctx, f.other, 0.2)
	q, err := f.st.ReviewQueue(ctx, f.lib, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(q) != 0 {
		t.Errorf("review queue holds %d music items", len(q))
	}
	r, err := f.st.ReparseTargets(ctx, f.lib, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range r {
		t.Errorf("re-parse target %+v", x)
	}
}
