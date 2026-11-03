package musicbrainz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"lancast/internal/meta"
)

/*
 * Everything here answers from responses recorded from musicbrainz.org on
 * 2026-11-03 (testdata/), including a real 503: the first request of the
 * recording session got one, which is how ordinary "busy" is.
 */

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// server answers each request with the first route whose key the request's
// path and query contain, and records what it was asked.
type server struct {
	t      *testing.T
	routes []route
	hits   atomic.Int32
	last   atomic.Value // *http.Request
}

type route struct {
	match  string
	status int
	file   string
}

func newClient(t *testing.T, routes ...route) (*Client, *server) {
	t.Helper()
	s := &server{t: t, routes: routes}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		s.last.Store(r)
		target := r.URL.Path + "?" + r.URL.RawQuery
		for _, rt := range s.routes {
			if strings.Contains(target, rt.match) {
				w.WriteHeader(rt.status)
				if rt.file != "" {
					w.Write(fixture(t, rt.file))
				}
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(ts.Close)
	c := New("v0.10.3",
		WithBaseURL(ts.URL),
		WithLimiter(meta.NewLimiter(1000, 1000)),
		WithRetryBase(time.Millisecond),
	)
	return c, s
}

func (s *server) request() *http.Request { return s.last.Load().(*http.Request) }

func search(t *testing.T, c *Client, title, artist string, year int) []meta.Candidate {
	t.Helper()
	q := meta.Query{Kind: meta.KindAlbum, Title: title, Artist: artist, Year: year}
	cands, err := c.Search(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	meta.Rank(q, cands)
	return cands
}

func TestASearchAsksForTheAlbumByTitleAndArtist(t *testing.T) {
	c, s := newClient(t, route{"/release-group", 200, "search-hello-nasty.json"})
	cands := search(t, c, "Hello Nasty", "Beastie Boys", 1998)

	r := s.request()
	if got := r.URL.Query().Get("query"); got != `releasegroup:"Hello Nasty" AND artist:"Beastie Boys"` {
		t.Errorf("query = %q", got)
	}
	if r.URL.Query().Get("fmt") != "json" {
		t.Errorf("fmt = %q, want json", r.URL.Query().Get("fmt"))
	}
	if ua := r.Header.Get("User-Agent"); ua != "LANcast/0.10.3 ( https://github.com/Conqueror-Mod/LANcast )" {
		t.Errorf("User-Agent = %q", ua)
	}
	if len(cands) != 3 || cands[0].ExternalID != "96c996ab-2a78-3abd-8974-c96b5724ae28" {
		t.Fatalf("best = %+v", cands)
	}
	if best, state := meta.Decide(cands); state != meta.StateMatched || best.Artist != "Beastie Boys" {
		t.Errorf("decided %q, %s", best.Title, state)
	}
}

// Queen has three release groups called Greatest Hits. Without a year they
// are a coin toss, which is review, not matched; with one, the year decides.
func TestThreeGreatestHitsNeedAYear(t *testing.T) {
	c, _ := newClient(t, route{"/release-group", 200, "search-greatest-hits-queen.json"})

	if best, state := meta.Decide(search(t, c, "Greatest Hits", "Queen", 0)); state != meta.StateReview {
		t.Errorf("no year: %s (%s), want review", state, best.ExternalID)
	}
	best, state := meta.Decide(search(t, c, "Greatest Hits", "Queen", 1981))
	if state != meta.StateMatched || best.ExternalID != "69ce61c8-127f-3809-95d8-62fdf3ae1347" {
		t.Errorf("1981: %s %s %d, want the 1981 record matched", state, best.ExternalID, best.Year)
	}
}

// The right title by the wrong artist is someone else's record, and is not
// offered even for review.
func TestTheWrongArtistIsNotAMatch(t *testing.T) {
	c, _ := newClient(t, route{"/release-group", 200, "search-hello-nasty.json"})
	best, state := meta.Decide(search(t, c, "Hello Nasty", "Nirvana", 1998))
	if state != meta.StateUnmatched {
		t.Errorf("state = %s at %.2f, want unmatched", state, best.Score)
	}
}

// A tag's year is often a reissue's. The record is still the record.
func TestAReissueYearStillMatches(t *testing.T) {
	c, _ := newClient(t, route{"/release-group", 200, "search-dreaming-neon-black.json"})
	if best, state := meta.Decide(search(t, c, "Dreaming Neon Black", "Nevermore", 2006)); state != meta.StateMatched {
		t.Errorf("state = %s at %.2f, want matched", state, best.Score)
	}
}

func TestAnAlbumRecord(t *testing.T) {
	c, s := newClient(t, route{"/release-group/96c996ab", 200, "releasegroup-hello-nasty.json"})
	rec, err := c.Fetch(context.Background(), meta.Ref{Kind: meta.KindAlbum, ExternalID: "96c996ab-2a78-3abd-8974-c96b5724ae28"})
	if err != nil {
		t.Fatal(err)
	}
	if inc := s.request().URL.Query().Get("inc"); inc != "artist-credits genres" {
		t.Errorf("inc = %q", inc)
	}
	if rec.Fields.Year == nil || *rec.Fields.Year != 1998 {
		t.Errorf("year = %v", rec.Fields.Year)
	}
	// Eleven genres, five of them one vote; the agreed ones survive.
	if want := []string{"Hip Hop", "East Coast Hip Hop", "Abstract Hip Hop", "Turntablism"}; !slices.Equal(rec.Genres, want) {
		t.Errorf("genres = %q, want %q", rec.Genres, want)
	}
	if rec.Music == nil || rec.Music.ReleaseType != "Album" {
		t.Fatalf("music = %+v", rec.Music)
	}
	if len(rec.Music.Artists) != 1 || rec.Music.Artists[0].ID != "9beb62b2-88db-4cea-801e-162cd344ee53" {
		t.Errorf("artists = %+v", rec.Music.Artists)
	}
}

func TestAnArtistRecord(t *testing.T) {
	c, _ := newClient(t, route{"/artist/9beb62b2", 200, "artist-beastie-boys.json"})
	rec, err := c.Fetch(context.Background(), meta.Ref{Kind: meta.KindArtist, ExternalID: "9beb62b2-88db-4cea-801e-162cd344ee53"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Fields.Title == nil || *rec.Fields.Title != "Beastie Boys" || rec.Kind != meta.KindArtist {
		t.Errorf("record = %+v", rec)
	}
	if want := []string{"Hip Hop", "Rap Rock", "East Coast Hip Hop"}; !slices.Equal(rec.Genres, want) {
		t.Errorf("genres = %q, want %q", rec.Genres, want)
	}
}

// Busy is retried, and busy for good is ErrBusy: a reason to come back, never
// a reason to call the album unmatched.
func TestBusyIsRetriedAndThenSaidSo(t *testing.T) {
	c, s := newClient(t, route{"/release-group", 503, "busy-503.json"})
	_, err := c.Search(context.Background(), meta.Query{Kind: meta.KindAlbum, Title: "Hello Nasty"})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if got, want := s.hits.Load(), int32(c.MaxRetries+1); got != want {
		t.Errorf("asked %d times, want %d", got, want)
	}

	// Busy once, then an answer: the answer is used.
	var n atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write(fixture(t, "search-hello-nasty.json"))
	}))
	defer ts.Close()
	c2 := New("1", WithBaseURL(ts.URL), WithLimiter(meta.NewLimiter(1000, 1000)), WithRetryBase(time.Millisecond))
	cands, err := c2.Search(context.Background(), meta.Query{Kind: meta.KindAlbum, Title: "Hello Nasty"})
	if err != nil || len(cands) != 3 {
		t.Errorf("after one busy answer: %d candidates, %v", len(cands), err)
	}
}

func TestAnUnknownIDIsNotFoundAndABadOneIsNeverSent(t *testing.T) {
	c, s := newClient(t)
	_, err := c.Fetch(context.Background(), meta.Ref{Kind: meta.KindAlbum, ExternalID: "00000000-0000-0000-0000-000000000000"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	before := s.hits.Load()
	for _, id := range []string{"", "../artist/x", "96C996AB-2A78-3ABD-8974-C96B5724AE28", "96c996ab-2a78-3abd-8974-c96b5724ae2?"} {
		if _, err := c.Fetch(context.Background(), meta.Ref{Kind: meta.KindAlbum, ExternalID: id}); err == nil {
			t.Errorf("id %q accepted", id)
		}
	}
	if s.hits.Load() != before {
		t.Error("a malformed id reached the server")
	}
}

// Only albums are searched. An artist is never looked up by name.
func TestOnlyAlbumsAreSearched(t *testing.T) {
	c, s := newClient(t, route{"/", 200, "search-hello-nasty.json"})
	for _, k := range []meta.Kind{meta.KindArtist, meta.KindMovie} {
		if cands, err := c.Search(context.Background(), meta.Query{Kind: k, Title: "Nirvana"}); err != nil || cands != nil {
			t.Errorf("%s: %v %v", k, cands, err)
		}
	}
	if s.hits.Load() != 0 {
		t.Error("a non-album search reached the server")
	}
}

func TestSearchQueryKeepsTitlesOutOfTheSyntax(t *testing.T) {
	cases := map[[2]string]string{
		{"Back in Black", "AC/DC"}:                `releasegroup:"Back in Black" AND artist:"AC/DC"`,
		{"Help!", ""}:                             `releasegroup:"Help!"`,
		{`The "Blue" Album`, `C:\Band`}:           `releasegroup:"The \"Blue\" Album" AND artist:"C:\\Band"`,
		{"  ", "Anyone"}:                          ``,
		{"(What's the Story) Morning Glory?", ""}: `releasegroup:"(What's the Story) Morning Glory?"`,
	}
	for in, want := range cases {
		if got := SearchQuery(in[0], in[1]); got != want {
			t.Errorf("SearchQuery(%q, %q) = %s, want %s", in[0], in[1], got, want)
		}
	}
}

func TestSmallRules(t *testing.T) {
	if got := releaseType("Album", []string{"Live"}); got != "Live" {
		t.Errorf("live album = %q", got)
	}
	if got := releaseType("EP", nil); got != "EP" {
		t.Errorf("EP = %q", got)
	}
	for in, want := range map[string]int{"1998-06-29": 1998, "2003": 2003, "1981-11": 1981, "": 0, "19": 0, "abcd": 0} {
		if got := yearOf(in); got != want {
			t.Errorf("yearOf(%q) = %d", in, got)
		}
	}
	for in, want := range map[string]string{"east coast hip hop": "East Coast Hip Hop", "hip-hop": "Hip-Hop", "drum and bass": "Drum And Bass", "r&b": "R&b"} {
		if got := genreName(in); got != want {
			t.Errorf("genreName(%q) = %q", in, got)
		}
	}
	if got := topGenres([]genre{{"obscure", 1}, {"thing", 1}}); !slices.Equal(got, []string{"Obscure", "Thing"}) {
		t.Errorf("one-vote record = %q", got)
	}
	if UserAgent("") != "LANcast/dev ( https://github.com/Conqueror-Mod/LANcast )" {
		t.Errorf("dev UA = %q", UserAgent(""))
	}
}

// A same-named single, EP or compilation does not make the album a coin toss:
// Prince's "1999" is a single and an album a month apart, and Motörhead's
// "Ace of Spades" an album among two singles and three compilations.
func TestTheAlbumWinsATieWithItsSingles(t *testing.T) {
	c, _ := newClient(t, route{"/release-group", 200, "search-1999-prince.json"})
	best, state := meta.Decide(search(t, c, "1999", "Prince", 1982))
	if state != meta.StateMatched || best.ReleaseType != "Album" {
		t.Errorf("1999: %s %s %q", state, best.ReleaseType, best.ExternalID)
	}

	c, _ = newClient(t, route{"/release-group", 200, "search-ace-of-spades.json"})
	for _, year := range []int{0, 1980} {
		best, state := meta.Decide(search(t, c, "Ace of Spades", "Motörhead", year))
		if state != meta.StateMatched || !strings.HasPrefix(best.ExternalID, "51d51de2") {
			t.Errorf("year %d: %s %s %s %d", year, state, best.ReleaseType, best.ExternalID, best.Year)
		}
	}
}

// The phrase finds nothing for "4x4=12" (MusicBrainz writes 4×4=12); the
// words are tried next, escaped, and only then the title alone.
func TestALooserSearchOnlyWhenTheExactOneFindsNothing(t *testing.T) {
	var asked []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		asked = append(asked, q)
		if strings.HasPrefix(q, "releasegroup:(") && !strings.Contains(q, "artist:") {
			w.Write(fixture(t, "search-hello-nasty.json"))
			return
		}
		w.Write([]byte(`{"count":0,"release-groups":[]}`))
	}))
	defer ts.Close()
	c := New("1", WithBaseURL(ts.URL), WithLimiter(meta.NewLimiter(1000, 1000)))
	cands, err := c.Search(context.Background(), meta.Query{Kind: meta.KindAlbum, Title: "4x4=12", Artist: "dead/mau5"})
	if err != nil || len(cands) != 3 {
		t.Fatalf("%d candidates, %v", len(cands), err)
	}
	want := []string{
		`releasegroup:"4x4=12" AND artist:"dead/mau5"`,
		`releasegroup:(4x4=12) AND artist:(dead\/mau5)`,
		`releasegroup:(4x4=12)`,
	}
	if !slices.Equal(asked, want) {
		t.Errorf("asked\n%q\nwant\n%q", asked, want)
	}

	// Found the first time: asked once.
	asked = nil
	c2, s := newClient(t, route{"/release-group", 200, "search-hello-nasty.json"})
	search(t, c2, "Hello Nasty", "Beastie Boys", 0)
	if s.hits.Load() != 1 {
		t.Errorf("asked %d times for a phrase that found it", s.hits.Load())
	}
}

func TestAnAlbumQueryLeavesTheEditionBehind(t *testing.T) {
	q := meta.AlbumQuery("2001 (censored version)", " Dr. Dre ", 1999)
	if q.Title != "2001" || q.Artist != "Dr. Dre" || q.Kind != meta.KindAlbum || q.Year != 1999 {
		t.Errorf("query = %+v", q)
	}
}
