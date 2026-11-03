package enrich

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"

	"lancast/internal/meta"
	"lancast/internal/store"
)

/*
 * Matching albums and artists in the background (ADR 0115), against a real
 * store and a stand-in for MusicBrainz.
 */

type fakeMusic struct {
	byTitle map[string][]meta.Candidate // album title -> candidates
	records map[string]*meta.Record     // external id -> record
	err     error                       // every call fails with this (busy)
	calls   atomic.Int32
}

func (f *fakeMusic) ID() string      { return "musicbrainz" }
func (f *fakeMusic) Caps() meta.Caps { return meta.Caps{Music: true} }
func (f *fakeMusic) Search(_ context.Context, q meta.Query) ([]meta.Candidate, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	return append([]meta.Candidate(nil), f.byTitle[q.Title]...), nil
}
func (f *fakeMusic) Fetch(_ context.Context, ref meta.Ref) (*meta.Record, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	return f.records[ref.ExternalID], nil
}

type musicLib struct {
	st                   *store.Store
	artist, a1, a2, lone int64
}

// One artist with two albums, and a second album artist with one.
func newMusicLib(t *testing.T) musicLib {
	t.Helper()
	ctx := context.Background()
	st, _ := harness(t)
	lib, err := st.CreateLibrary(ctx, "Music", "music", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mk := func(kind, key, title string, parent *int64) int64 {
		id, err := st.EnsureDerivedContainer(ctx, lib.ID, kind, key, title, title, parent)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	m := musicLib{st: st}
	m.artist = mk("artist", "k::artist=Beastie Boys", "Beastie Boys", nil)
	m.a1 = mk("album", "k::album=Hello Nasty", "Hello Nasty", &m.artist)
	m.a2 = mk("album", "k::album=Ill Communication (Remastered)", "Ill Communication (Remastered)", &m.artist)
	other := mk("artist", "k::artist=Nobody", "Nobody", nil)
	m.lone = mk("album", "k::album=Unfindable", "Unfindable", &other)
	return m
}

func beastie() *fakeMusic {
	cand := func(id, title string) meta.Candidate {
		return meta.Candidate{Provider: "musicbrainz", ExternalID: id, Kind: meta.KindAlbum,
			Title: title, Artist: "Beastie Boys", Year: 1998, ReleaseType: "Album"}
	}
	album := func(id string, year int) *meta.Record {
		return &meta.Record{Source: "musicbrainz", ExternalID: id, Kind: meta.KindAlbum,
			Fields: meta.Fields{Year: meta.I(year)}, Genres: []string{"Hip Hop"},
			Music: &meta.MusicInfo{ReleaseType: "Album", Artists: []meta.MusicArtist{{ID: "bb", Name: "Beastie Boys"}}}}
	}
	return &fakeMusic{
		byTitle: map[string][]meta.Candidate{
			"Hello Nasty":       {cand("rg-hn", "Hello Nasty")},
			"Ill Communication": {cand("rg-ic", "Ill Communication")},
		},
		records: map[string]*meta.Record{
			"rg-hn": album("rg-hn", 1998),
			"rg-ic": album("rg-ic", 1994),
			"bb": {Source: "musicbrainz", ExternalID: "bb", Kind: meta.KindArtist,
				Genres: []string{"Hip Hop", "Rap Rock"}},
		},
	}
}

func run(t *testing.T, st *store.Store, p meta.Provider) {
	t.Helper()
	reg := meta.NewRegistry()
	if p != nil {
		reg.AddProvider(p)
	}
	if err := New(st, reg, &fakeArt{}, quietLog()).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, st *store.Store, id int64) store.Item {
	t.Helper()
	it, err := st.GetItem(context.Background(), id, "")
	if err != nil {
		t.Fatal(err)
	}
	return *it
}

func TestAlbumsAndTheirArtistAreMatched(t *testing.T) {
	m := newMusicLib(t)
	m.st.SetMusicMatching(true)
	run(t, m.st, beastie())
	ctx := context.Background()

	for _, id := range []int64{m.a1, m.a2} {
		al := get(t, m.st, id)
		if al.MatchState != meta.StateMatched || al.ExternalID == nil {
			t.Errorf("album %q: %s %v", al.Title, al.MatchState, al.ExternalID)
		}
	}
	// "(Remastered)" was searched without its note, and its own title kept.
	if al := get(t, m.st, m.a2); al.Title != "Ill Communication (Remastered)" || *al.ExternalID != "rg-ic" {
		t.Errorf("remaster: %q %v", al.Title, *al.ExternalID)
	}
	ar := get(t, m.st, m.artist)
	if ar.MatchState != meta.StateMatched || ar.ExternalID == nil || *ar.ExternalID != "bb" {
		t.Errorf("artist: %s %v", ar.MatchState, ar.ExternalID)
	}
	if g, _ := m.st.Genres(ctx, m.artist); !slices.Equal(g, []string{"Hip Hop", "Rap Rock"}) {
		t.Errorf("artist genres = %v", g)
	}
	// Nothing found is a real answer: unmatched, and out of the queue.
	if lone := get(t, m.st, m.lone); lone.MatchState != meta.StateUnmatched {
		t.Errorf("lone album: %s", lone.MatchState)
	}
	if n, _ := m.st.PendingCount(ctx); n != 0 {
		t.Errorf("%d left pending", n)
	}
}

// Busy is no answer. Everything stays pending for the next pass.
func TestABusyProviderLeavesMusicPending(t *testing.T) {
	m := newMusicLib(t)
	m.st.SetMusicMatching(true)
	p := beastie()
	p.err = errors.New("musicbrainz: busy, try later")
	run(t, m.st, p)
	if n, _ := m.st.PendingCount(context.Background()); n != 5 {
		t.Errorf("pending = %d, want all 5 (3 albums, 2 artists)", n)
	}
	// And the next pass, with MusicBrainz back, matches them.
	p.err = nil
	run(t, m.st, p)
	if get(t, m.st, m.a1).MatchState != meta.StateMatched {
		t.Error("not matched once MusicBrainz answered")
	}
}

// Off asks nothing; on with no music provider marks nothing.
func TestMusicWaitsForTheSwitchAndTheProvider(t *testing.T) {
	m := newMusicLib(t)
	p := beastie()
	run(t, m.st, p)
	if p.calls.Load() != 0 {
		t.Errorf("asked MusicBrainz %d times with matching off", p.calls.Load())
	}
	m.st.SetMusicMatching(true)
	run(t, m.st, nil)
	if n, _ := m.st.PendingCount(context.Background()); n != 5 {
		t.Errorf("pending = %d after a pass with no provider; nothing should be marked", n)
	}
}

// An artist whose albums name two different artists equally is not guessed.
func TestAnArtistSplitBetweenTwoIsNotGuessed(t *testing.T) {
	m := newMusicLib(t)
	m.st.SetMusicMatching(true)
	p := beastie()
	other := *p.records["rg-ic"]
	other.Music = &meta.MusicInfo{ReleaseType: "Album", Artists: []meta.MusicArtist{{ID: "someone-else"}}}
	p.records["rg-ic"] = &other
	run(t, m.st, p)
	if ar := get(t, m.st, m.artist); ar.MatchState != meta.StateUnmatched || ar.ExternalID != nil && *ar.ExternalID != "" {
		t.Errorf("artist: %s %v", ar.MatchState, ar.ExternalID)
	}
}

// urlArt stores each URL as its own image, so one cover can replace another.
type urlArt struct{ downloaded []string }

func (a *urlArt) Download(_ context.Context, url string) (string, int, int, int64, error) {
	a.downloaded = append(a.downloaded, url)
	return fmt.Sprintf("%064x", len(a.downloaded)), 500, 500, 1000, nil
}
func (a *urlArt) Stored(string) bool { return false }

func withCovers(p *fakeMusic) *fakeMusic {
	for id, r := range p.records {
		if r.Kind == meta.KindAlbum {
			r.Artwork = []meta.ArtRef{{Kind: meta.ArtPoster, URL: "https://coverartarchive.org/release-group/" + id + "/front-500"}}
		}
	}
	return p
}

func runWith(t *testing.T, st *store.Store, p meta.Provider, art ArtCache) {
	t.Helper()
	reg := meta.NewRegistry()
	reg.AddProvider(p)
	if err := New(st, reg, art, quietLog()).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func posterSource(t *testing.T, st *store.Store, id int64) (string, bool) {
	t.Helper()
	src, ok, err := st.SelectedArtworkSource(context.Background(), id, "poster")
	if err != nil {
		t.Fatal(err)
	}
	return src, ok
}

// Step 4: a confidently matched album with no cover gets its record's front
// cover; one with art of its own keeps it.
func TestAMatchedAlbumWithoutArtGetsTheArchiveCover(t *testing.T) {
	m := newMusicLib(t)
	m.st.SetMusicMatching(true)
	ctx := context.Background()
	// Hello Nasty has a cover from its files: no source URL.
	if err := m.st.PutArtwork(ctx, m.a1, fmt.Sprintf("%064x", 999), "poster", "", 600, 600, 1); err != nil {
		t.Fatal(err)
	}
	art := &urlArt{}
	runWith(t, m.st, withCovers(beastie()), art)

	if src, _ := posterSource(t, m.st, m.a1); src != "" {
		t.Errorf("Hello Nasty's own cover was replaced by %q", src)
	}
	src, ok := posterSource(t, m.st, m.a2)
	if !ok || src != "https://coverartarchive.org/release-group/rg-ic/front-500" {
		t.Errorf("Ill Communication cover = %q %v, want the archive's", src, ok)
	}
	if len(art.downloaded) != 1 {
		t.Errorf("downloaded %v, want only the album without art", art.downloaded)
	}
}

// A guess brings no picture: a wrong cover is the most visible mistake there
// is. Two records of one name, neither the album, is a guess (Decide).
func TestAReviewMatchBringsNoCover(t *testing.T) {
	m := newMusicLib(t)
	m.st.SetMusicMatching(true)
	p := withCovers(beastie())
	single := func(id string) meta.Candidate {
		return meta.Candidate{Provider: "musicbrainz", ExternalID: id, Kind: meta.KindAlbum,
			Title: "Hello Nasty", Artist: "Beastie Boys", Year: 1998, ReleaseType: "Single"}
	}
	p.byTitle["Hello Nasty"] = []meta.Candidate{single("rg-hn"), single("rg-ic")}
	art := &urlArt{}
	runWith(t, m.st, p, art)
	if al := get(t, m.st, m.a1); al.MatchState != meta.StateReview {
		t.Fatalf("fixture: Hello Nasty is %s (%.2f), want review", al.MatchState, deref(al.MatchScore))
	}
	if _, ok := posterSource(t, m.st, m.a1); ok {
		t.Error("a review-band match brought a cover")
	}
}

// Fixing a match replaces a cover that came with the wrong one, and never a
// cover of the album's own or a locked one.
func TestFixMatchReplacesOnlyTheArchiveCover(t *testing.T) {
	m := newMusicLib(t)
	m.st.SetMusicMatching(true)
	p := withCovers(beastie())
	art := &urlArt{}
	runWith(t, m.st, p, art)
	ctx := context.Background()
	reg := meta.NewRegistry()
	reg.AddProvider(p)
	w := New(m.st, reg, art, quietLog())

	// Ill Communication's archive cover is the wrong record's: fixing it
	// brings the right one.
	if err := w.ApplyMusicMatch(ctx, get(t, m.st, m.a2), "musicbrainz", "rg-hn"); err != nil {
		t.Fatal(err)
	}
	if src, _ := posterSource(t, m.st, m.a2); src != "https://coverartarchive.org/release-group/rg-hn/front-500" {
		t.Errorf("after the fix the cover is %q", src)
	}

	// A cover the person locked stays, whatever the match.
	if err := m.st.LockField(ctx, m.a1, meta.FieldArtwork); err != nil {
		t.Fatal(err)
	}
	before, _ := posterSource(t, m.st, m.a1)
	if err := w.ApplyMusicMatch(ctx, get(t, m.st, m.a1), "musicbrainz", "rg-ic"); err != nil {
		t.Fatal(err)
	}
	if after, _ := posterSource(t, m.st, m.a1); after != before {
		t.Errorf("a locked cover changed from %q to %q", before, after)
	}
}

func deref(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}
