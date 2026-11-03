package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"lancast/internal/meta"
	"lancast/internal/meta/musicbrainz"
	"lancast/internal/store"
)

// The match_music switch decides what the enrichment queue holds (ADR 0115):
// off takes albums and artists out at once, on puts them back and starts a
// pass rather than waiting for the next scan.
func TestMatchMusicSwitchMovesTheQueue(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	lib, err := h.st.CreateLibrary(ctx, "Music", "music", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	artist, err := h.st.EnsureDerivedContainer(ctx, lib.ID, "artist", "k::artist=A", "A", "A", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.EnsureDerivedContainer(ctx, lib.ID, "album", "k::album=B", "B", "B", &artist); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	decode(t, h.do(t, "GET", "/api/settings", nil), &got)
	if got["match_music"] != true {
		t.Fatalf("match_music default = %v, want true", got["match_music"])
	}

	decode(t, h.do(t, "PUT", "/api/settings", map[string]any{"match_music": false}), &got)
	if n, _ := h.st.PendingCount(ctx); n != 0 {
		t.Errorf("off: %d pending, want 0", n)
	}
	before := h.enriched
	decode(t, h.do(t, "PUT", "/api/settings", map[string]any{"match_music": true}), &got)
	if n, _ := h.st.PendingCount(ctx); n != 2 {
		t.Errorf("on: %d pending, want the album and its artist", n)
	}
	if h.enriched == before {
		t.Error("switching it on did not start a pass")
	}
	if !h.settings.Get().MatchMusic {
		t.Error("the setting was not stored")
	}
}

// A stand-in for MusicBrainz: albums by title, records by id.
type fakeMB struct {
	byTitle map[string][]meta.Candidate
	records map[string]*meta.Record
	queries []meta.Query
}

func (f *fakeMB) ID() string      { return "musicbrainz" }
func (f *fakeMB) Caps() meta.Caps { return meta.Caps{Music: true} }
func (f *fakeMB) Search(_ context.Context, q meta.Query) ([]meta.Candidate, error) {
	f.queries = append(f.queries, q)
	return append([]meta.Candidate(nil), f.byTitle[q.Title]...), nil
}
func (f *fakeMB) Fetch(_ context.Context, ref meta.Ref) (*meta.Record, error) {
	if r, ok := f.records[ref.ExternalID]; ok {
		return r, nil
	}
	return nil, musicbrainz.ErrNotFound
}

const (
	rgRight = "11111111-1111-4111-8111-111111111111"
	rgWrong = "22222222-2222-4222-8222-222222222222"
	arBand  = "33333333-3333-4333-8333-333333333333"
	arOther = "44444444-4444-4444-8444-444444444444"
)

type musicHarness struct {
	*harness
	mb            *fakeMB
	artist, album int64
}

func newMusicHarness(t *testing.T) musicHarness {
	t.Helper()
	h := newHarness(t)
	ctx := context.Background()
	lib, err := h.st.CreateLibrary(ctx, "Music", "music", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := musicHarness{harness: h}
	if m.artist, err = h.st.EnsureDerivedContainer(ctx, lib.ID, "artist", "k::artist=Band", "Band", "Band", nil); err != nil {
		t.Fatal(err)
	}
	if m.album, err = h.st.EnsureDerivedContainer(ctx, lib.ID, "album", "k::album=Record (Deluxe Edition)", "Record (Deluxe Edition)", "Record (Deluxe Edition)", &m.artist); err != nil {
		t.Fatal(err)
	}
	album := func(id, artist string, year int) *meta.Record {
		return &meta.Record{Source: "musicbrainz", ExternalID: id, Kind: meta.KindAlbum,
			Fields: meta.Fields{Title: meta.S("Record"), Year: meta.I(year)}, Genres: []string{"Jazz"},
			Music: &meta.MusicInfo{ReleaseType: "Album", Artists: []meta.MusicArtist{{ID: artist, Name: "Band"}}}}
	}
	m.mb = &fakeMB{
		byTitle: map[string][]meta.Candidate{
			"Record": {
				{ExternalID: rgRight, Kind: meta.KindAlbum, Title: "Record", Artist: "Band", Year: 2001, ReleaseType: "Album"},
				{ExternalID: rgWrong, Kind: meta.KindAlbum, Title: "Record", Artist: "Someone Else", Year: 1980, ReleaseType: "Single"},
			},
		},
		records: map[string]*meta.Record{
			rgRight: album(rgRight, arBand, 2001),
			rgWrong: album(rgWrong, arOther, 1980),
			arBand:  {Source: "musicbrainz", ExternalID: arBand, Kind: meta.KindArtist, Fields: meta.Fields{Title: meta.S("Band")}},
			arOther: {Source: "musicbrainz", ExternalID: arOther, Kind: meta.KindArtist, Fields: meta.Fields{Title: meta.S("Other Band")}},
		},
	}
	h.reg.AddProvider(m.mb)
	return m
}

func (m musicHarness) candidates(t *testing.T, id int64, query string) []meta.Candidate {
	t.Helper()
	resp := m.do(t, "GET", fmt.Sprintf("/api/items/%d/candidates%s", id, query), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("candidates: %d", resp.StatusCode)
	}
	var got []meta.Candidate
	decode(t, resp, &got)
	return got
}

// The dialog shows what the background pass would see: the album searched by
// its title without the edition note and by its artist, scored on both.
func TestAlbumCandidatesAreSearchedByTitleAndArtist(t *testing.T) {
	m := newMusicHarness(t)
	got := m.candidates(t, m.album, "")
	if len(got) != 2 || got[0].ExternalID != rgRight {
		t.Fatalf("candidates = %+v, want the album by Band first", got)
	}
	q := m.mb.queries[0]
	if q.Title != "Record" || q.Artist != "Band" {
		t.Errorf("searched %q by %q", q.Title, q.Artist)
	}
	if got[0].Artist != "Band" || got[0].ReleaseType != "Album" || got[0].Breakdown.Artist < 0.99 {
		t.Errorf("best = %+v; the artist, its agreement and the release type should be on the wire", got[0])
	}
	if got[1].Breakdown.Artist > 0.5 || got[1].Score >= got[0].Score {
		t.Errorf("the other artist's record scored %.2f (artist %.2f)", got[1].Score, got[1].Breakdown.Artist)
	}
	// A pasted address is that record exactly.
	exact := m.candidates(t, m.album, "?q=https://musicbrainz.org/release-group/"+rgWrong)
	if len(exact) != 1 || exact[0].ExternalID != rgWrong || exact[0].Artist != "Band" {
		t.Errorf("pasted id: %+v", exact)
	}
}

func TestAlbumFixMatchLocksKeepsTagsAndRequeuesTheArtist(t *testing.T) {
	m := newMusicHarness(t)
	ctx := context.Background()
	m.st.SetMusicMatching(true)
	m.st.ApplyArtistMatch(ctx, m.artist, store.ArtistMatch{Provider: "musicbrainz", ExternalID: arOther, State: "matched", Score: 1})

	resp := m.do(t, "POST", fmt.Sprintf("/api/items/%d/match", m.album),
		map[string]any{"provider": "musicbrainz", "external_id": rgRight})
	var it map[string]any
	decode(t, resp, &it)
	if resp.StatusCode != http.StatusOK || it["match_state"] != "locked" || it["external_id"] != rgRight {
		t.Fatalf("%d %v %v", resp.StatusCode, it["match_state"], it["external_id"])
	}
	if it["title"] != "Record (Deluxe Edition)" {
		t.Errorf("title = %v; the tagged title should win", it["title"])
	}
	// The artist goes back to be voted on, and the vote is now Band's.
	if n, _ := m.st.PendingCount(ctx); n != 1 {
		t.Errorf("%d pending, want the artist", n)
	}
	cands := m.candidates(t, m.artist, "")
	if len(cands) != 2 || cands[0].ExternalID != arBand || cands[0].Score != 1 || cands[0].Title != "Band" {
		t.Errorf("artist candidates = %+v; Band with every vote, then the current answer", cands)
	}
}

func TestMusicFixMatchRefusals(t *testing.T) {
	m := newMusicHarness(t)
	wantError(t, m.do(t, "POST", fmt.Sprintf("/api/items/%d/match", m.album),
		map[string]any{"provider": "tmdb", "external_id": "603"}), http.StatusBadRequest, "bad_request")
	wantError(t, m.do(t, "POST", fmt.Sprintf("/api/items/%d/match", m.album),
		map[string]any{"provider": "musicbrainz", "external_id": "55555555-5555-4555-8555-555555555555"}),
		http.StatusBadRequest, "not_found")
	// Off means nothing is asked of MusicBrainz, a dialog included.
	m.do(t, "PUT", "/api/settings", map[string]any{"match_music": false}).Body.Close()
	before := len(m.mb.queries)
	wantError(t, m.do(t, "GET", fmt.Sprintf("/api/items/%d/candidates", m.album), nil),
		http.StatusConflict, "music_matching_off")
	if len(m.mb.queries) != before {
		t.Error("MusicBrainz was asked with matching off")
	}
}
