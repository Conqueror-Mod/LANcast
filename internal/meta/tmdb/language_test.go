package tmdb

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"lancast/internal/meta"
)

/*
 * Metadata in another language (ADR 0122): every request asks for it, a cached
 * answer in one language is never served for another, English fills the gaps,
 * and a file named in English still matches a title searched in German.
 */

// The same film, as TMDB answers in each language.
const (
	godfatherSearchDE = `{"results":[{"id":238,"title":"Der Pate","original_title":"The Godfather","release_date":"1972-03-14","popularity":90}]}`
	godfatherSearchEN = `{"results":[{"id":238,"title":"The Godfather","original_title":"The Godfather","release_date":"1972-03-14","popularity":90}]}`
	spiritedSearchDE  = `{"results":[{"id":129,"title":"Chihiros Reise ins Zauberland","original_title":"千と千尋の神隠し","release_date":"2001-07-20","popularity":80}]}`
	spiritedSearchEN  = `{"results":[{"id":129,"title":"Spirited Away","original_title":"千と千尋の神隠し","release_date":"2001-07-20","popularity":80}]}`
)

type seen struct {
	mu   sync.Mutex
	reqs []string
}

func (s *seen) add(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, r.URL.Path+"?language="+r.URL.Query().Get("language"))
}

func (s *seen) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.reqs...)
}

func TestEveryRequestAsksForTheChosenLanguage(t *testing.T) {
	var got seen
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		got.add(r)
		w.Write([]byte(`{"id":1,"title":"Der Pate","overview":"Ein Film."}`))
	})
	c := newClient(t, srv, WithLanguage("de-DE"))
	if _, err := c.Fetch(context.Background(), meta.Ref{Kind: meta.KindMovie, ExternalID: "1"}); err != nil {
		t.Fatal(err)
	}
	reqs := got.all()
	if len(reqs) != 1 || reqs[0] != "/movie/1?language=de-DE" {
		t.Errorf("requests = %v, want one for de-DE (nothing was missing, so no English fill)", reqs)
	}
}

func TestAnUnknownLanguageFetchesInEnglish(t *testing.T) {
	var got seen
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		got.add(r)
		w.Write([]byte(`{"id":1,"title":"The Godfather","overview":"A film."}`))
	})
	c := newClient(t, srv, WithLanguage("xx-YY"))
	if _, err := c.Fetch(context.Background(), meta.Ref{Kind: meta.KindMovie, ExternalID: "1"}); err != nil {
		t.Fatal(err)
	}
	if reqs := got.all(); len(reqs) != 1 || reqs[0] != "/movie/1?language=en-US" {
		t.Errorf("requests = %v, want one for en-US", reqs)
	}
}

func TestACachedAnswerIsNeverServedForAnotherLanguage(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("language") == "de-DE" {
			w.Write([]byte(`{"id":1,"title":"Der Pate","overview":"Ein Film."}`))
			return
		}
		w.Write([]byte(`{"id":1,"title":"The Godfather","overview":"A film."}`))
	})
	cache := newMemCache()
	ref := meta.Ref{Kind: meta.KindMovie, ExternalID: "1"}
	en, err := newClient(t, srv, WithCache(cache)).Fetch(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	de, err := newClient(t, srv, WithCache(cache), WithLanguage("de-DE")).Fetch(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if *en.Fields.Title != "The Godfather" || *de.Fields.Title != "Der Pate" {
		t.Errorf("titles = %q and %q: one language's cached answer was served for the other",
			*en.Fields.Title, *de.Fields.Title)
	}
	// English keeps the key it had before languages existed, so an upgrade
	// keeps its cache.
	for k := range cache.data {
		if strings.Contains(k, "language=en-US") {
			t.Errorf("an English answer is keyed with its language (%q); the old cache would be lost", k)
		}
	}
}

func TestEnglishFillsWhatTheChosenLanguageLeftEmpty(t *testing.T) {
	var got seen
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		got.add(r)
		if r.URL.Query().Get("language") == "de-DE" {
			// No German synopsis: TMDB answers with an empty one.
			w.Write([]byte(`{"id":1,"title":"Der Pate","overview":""}`))
			return
		}
		w.Write([]byte(`{"id":1,"title":"The Godfather","overview":"A film."}`))
	})
	rec, err := newClient(t, srv, WithLanguage("de-DE")).Fetch(context.Background(), meta.Ref{Kind: meta.KindMovie, ExternalID: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if *rec.Fields.Title != "Der Pate" {
		t.Errorf("title = %q, want the German one kept", *rec.Fields.Title)
	}
	if rec.Fields.Overview == nil || *rec.Fields.Overview != "A film." {
		t.Errorf("overview = %v, want the English one filling the gap", rec.Fields.Overview)
	}
	if reqs := got.all(); len(reqs) != 2 || reqs[1] != "/movie/1?language=en-US" {
		t.Errorf("requests = %v, want the German detail and then an English fill", reqs)
	}
}

func TestEnglishNeedsNoFill(t *testing.T) {
	var got seen
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		got.add(r)
		w.Write([]byte(`{"id":1,"name":"Some Episode","overview":""}`))
	})
	c := newClient(t, srv)
	if _, err := c.Fetch(context.Background(), meta.Ref{Kind: meta.KindEpisode, ExternalID: "5", Season: 1, Episode: 2}); err != nil {
		t.Fatal(err)
	}
	if reqs := got.all(); len(reqs) != 1 {
		t.Errorf("requests = %v: an English server asked twice for an empty synopsis", reqs)
	}
}

// The case this design exists for: German titles, English filenames.
func TestAnEnglishFilenameMatchesATitleSearchedInGerman(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		de := r.URL.Query().Get("language") == "de-DE"
		spirited := strings.Contains(r.URL.Query().Get("query"), "Spirited")
		switch {
		case spirited && de:
			w.Write([]byte(spiritedSearchDE))
		case spirited:
			w.Write([]byte(spiritedSearchEN))
		case de:
			w.Write([]byte(godfatherSearchDE))
		default:
			w.Write([]byte(godfatherSearchEN))
		}
	})
	c := newClient(t, srv, WithLanguage("de-DE"))

	for _, q := range []meta.Query{
		// The original title is English: matched through original_title.
		{Kind: meta.KindMovie, Title: "The Godfather", Year: 1972},
		// The original title is Japanese: matched only through the English search.
		{Kind: meta.KindMovie, Title: "Spirited Away", Year: 2001},
		// A German filename still matches the German title.
		{Kind: meta.KindMovie, Title: "Der Pate", Year: 1972},
	} {
		cands, err := c.Search(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		if len(cands) != 1 {
			t.Fatalf("%q: %d candidates", q.Title, len(cands))
		}
		b := meta.ScoreBreakdown(q, cands[0])
		if b.Title != 1 {
			t.Errorf("%q against %q (%v): title score %.2f, want 1", q.Title, cands[0].Title, cands[0].OtherTitles, b.Title)
		}
		if cands[0].Title == "The Godfather" || cands[0].Title == "Spirited Away" {
			t.Errorf("%q: shown as %q, want the German title shown", q.Title, cands[0].Title)
		}
	}
}

func TestAnEnglishServerSearchesOnce(t *testing.T) {
	var got seen
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		got.add(r)
		w.Write([]byte(godfatherSearchEN))
	})
	if _, err := newClient(t, srv).Search(context.Background(), meta.Query{Kind: meta.KindMovie, Title: "The Godfather"}); err != nil {
		t.Fatal(err)
	}
	if reqs := got.all(); len(reqs) != 1 {
		t.Errorf("requests = %v, want one search", reqs)
	}
}

func TestTrailersAreNotLimitedToTheChosenLanguage(t *testing.T) {
	var asked string
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Query().Get("include_video_language")
		w.Write([]byte(`{"results":[]}`))
	})
	if _, err := newClient(t, srv, WithLanguage("de-DE")).Trailer(context.Background(), meta.Ref{Kind: meta.KindMovie, ExternalID: "1"}); err != nil {
		t.Fatal(err)
	}
	if asked != "de,en,null" {
		t.Errorf("include_video_language = %q, want de,en,null", asked)
	}
}
