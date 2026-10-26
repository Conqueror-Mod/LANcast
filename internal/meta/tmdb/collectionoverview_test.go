package tmdb

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"lancast/internal/meta"
)

/*
 * A collection's description comes from the collection endpoint, on the
 * request its name already made. The block embedded in the movie carries none,
 * and the movie's own overview must never stand in for it — that would describe
 * the box set as its first film.
 */
func TestTheCollectionOverviewComesFromTheCollectionEndpoint(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/collection/") {
			_, _ = w.Write([]byte(`{"id":133352,"name":"Hulk Collection",` +
				`"overview":"  The Hulk films, in order.  "}`))
			return
		}
		_, _ = w.Write([]byte(movieWithTurkishCollectionJSON))
	})
	rec, err := newClient(t, srv).Fetch(context.Background(), meta.Ref{Kind: meta.KindMovie, ExternalID: "1927"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Collection == nil {
		t.Fatal("no collection on the record")
	}
	if rec.Collection.Overview != "The Hulk films, in order." {
		t.Errorf("collection overview = %q, want the collection's own, trimmed", rec.Collection.Overview)
	}
}

/*
 * The collection's whole film list, in release order (ADR 0093), with an
 * undated, announced film last. Parts with no id or title are dropped rather
 * than stored as rows nobody can name.
 */
func TestTheCollectionCarriesItsFilmsInReleaseOrder(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/collection/") {
			_, _ = w.Write([]byte(`{"id":133352,"name":"Hulk Collection","parts":[
				{"id":3,"title":"The Next One","release_date":""},
				{"id":2,"title":"The Incredible Hulk","release_date":"2008-06-12","poster_path":"/b.jpg"},
				{"id":0,"title":"Broken"},
				{"id":1,"title":"Hulk","release_date":"2003-06-20","poster_path":"/a.jpg"}]}`))
			return
		}
		_, _ = w.Write([]byte(movieWithTurkishCollectionJSON))
	})
	rec, err := newClient(t, srv).Fetch(context.Background(), meta.Ref{Kind: meta.KindMovie, ExternalID: "1927"})
	if err != nil {
		t.Fatal(err)
	}
	films := rec.Collection.Films
	var got []string
	for _, f := range films {
		got = append(got, f.ExternalID)
	}
	if strings.Join(got, ",") != "1,2,3" {
		t.Fatalf("films = %v, want release order 1,2,3 with the undated one last", got)
	}
	if films[0].Year != 2003 || films[0].ReleasedAt == 0 || !strings.HasSuffix(films[0].PosterURL, "/a.jpg") {
		t.Errorf("first film = %+v", films[0])
	}
	if films[2].ReleasedAt != 0 || films[2].PosterURL != "" {
		t.Errorf("the undated film = %+v, want no date and no poster", films[2])
	}
}

// Looked up by its own id, for a collection that predates film lists.
func TestACollectionCanBeLookedUpByItsOwnID(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/collection/133352" {
			_, _ = w.Write([]byte(`{"id":133352,"name":"Hulk Collection","overview":"Both.",
				"parts":[{"id":1,"title":"Hulk","release_date":"2003-06-20"}]}`))
			return
		}
		http.NotFound(w, r)
	})
	ref, err := newClient(t, srv).Collection(context.Background(), "133352")
	if err != nil || ref == nil {
		t.Fatalf("Collection = %+v, %v", ref, err)
	}
	if ref.Name != "Hulk Collection" || ref.Overview != "Both." || len(ref.Films) != 1 {
		t.Errorf("ref = %+v", ref)
	}
	// A keyword grouping is not a TMDB collection, and is not asked about.
	if ref, err := newClient(t, srv).Collection(context.Background(), "keyword:180547"); ref != nil || err != nil {
		t.Errorf("keyword grouping = %+v, %v; want nothing asked", ref, err)
	}
}

// A failed lookup leaves no description rather than borrowing the film's.
func TestAFailedCollectionLookupLeavesNoOverview(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/collection/") {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(movieWithTurkishCollectionJSON))
	})
	rec, err := newClient(t, srv).Fetch(context.Background(), meta.Ref{Kind: meta.KindMovie, ExternalID: "1927"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Collection == nil {
		t.Fatal("the collection was dropped")
	}
	if rec.Collection.Overview != "" {
		t.Errorf("overview = %q, want none when the lookup failed", rec.Collection.Overview)
	}
	// Nil, not empty: "did not say" must never be stored as "has no films".
	if rec.Collection.Films != nil {
		t.Errorf("films = %v, want nil when the lookup failed", rec.Collection.Films)
	}
}
