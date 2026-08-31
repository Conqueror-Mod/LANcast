package tmdb

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"lancast/internal/meta"
)

/*
 * The collection's name, in the language everything else came back in.
 *
 * Reported: a Hulk collection displaying as "Hulk Koleksiyonu" — Turkish — on a
 * server whose film titles, overviews and genres were all English. Nothing was
 * wrong with the match or the request. `belongs_to_collection`, embedded in the
 * movie response, is not a translated field: it carries whatever name the
 * collection is stored under, and no language parameter changes it.
 *
 * The dedicated /collection/{id} endpoint is translated, so it is asked.
 */

const movieWithTurkishCollectionJSON = `{
 "id":1927,"title":"Hulk","release_date":"2003-06-20","overview":"Bruce Banner.",
 "belongs_to_collection":{"id":133352,"name":"Hulk Koleksiyonu","poster_path":"/hp.jpg","backdrop_path":"/hb.jpg"}}`

const englishCollectionJSON = `{"id":133352,"name":"Hulk Collection","poster_path":"/hp.jpg"}`

// The reported fault.
func TestTheCollectionNameComesFromTheCollectionEndpoint(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/collection/"):
			_, _ = w.Write([]byte(englishCollectionJSON))
		default:
			_, _ = w.Write([]byte(movieWithTurkishCollectionJSON))
		}
	})
	rec, err := newClient(t, srv).Fetch(context.Background(), meta.Ref{Kind: meta.KindMovie, ExternalID: "1927"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Collection == nil {
		t.Fatal("no collection on the record")
	}
	if rec.Collection.Name != "Hulk Collection" {
		t.Errorf("collection name = %q, want the translated one from /collection",
			rec.Collection.Name)
	}
	// The id and artwork still come from the embedded block — only the name was
	// ever in question, and re-deriving the rest would be change for its own sake.
	if rec.Collection.ExternalID != "133352" {
		t.Errorf("external id = %q", rec.Collection.ExternalID)
	}
}

/*
 * And the fallback, which is the half that keeps this safe.
 *
 * A franchise grouping is worth a wrong-language name and is not worth failing
 * an enrichment over — the film is matched by the time this runs. So a
 * collection endpoint that errors leaves the embedded name in place rather than
 * losing the collection entirely.
 */
func TestAFailedCollectionLookupKeepsTheEmbeddedName(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/collection/") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(movieWithTurkishCollectionJSON))
	})
	rec, err := newClient(t, srv).Fetch(context.Background(), meta.Ref{Kind: meta.KindMovie, ExternalID: "1927"})
	if err != nil {
		t.Fatalf("a collection lookup failure must not fail the fetch: %v", err)
	}
	if rec.Collection == nil {
		t.Fatal("the collection was dropped when its name lookup failed")
	}
	if rec.Collection.Name != "Hulk Koleksiyonu" {
		t.Errorf("name = %q, want the embedded name as a fallback", rec.Collection.Name)
	}
}

// An empty name is not an answer either.
func TestAnEmptyCollectionNameFallsBack(t *testing.T) {
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/collection/") {
			_, _ = w.Write([]byte(`{"id":133352,"name":""}`))
			return
		}
		_, _ = w.Write([]byte(movieWithTurkishCollectionJSON))
	})
	rec, err := newClient(t, srv).Fetch(context.Background(), meta.Ref{Kind: meta.KindMovie, ExternalID: "1927"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Collection.Name != "Hulk Koleksiyonu" {
		t.Errorf("name = %q, want the embedded name", rec.Collection.Name)
	}
}

/*
 * Every request states its language.
 *
 * It was previously omitted entirely, so the behaviour was TMDB's default
 * rather than LANcast's choice — a thing nobody asked for and nobody can see,
 * which is the kind that changes underneath you.
 */
func TestEveryRequestAsksForALanguage(t *testing.T) {
	var paths []string
	srv := fakeTMDB(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("language") == "" {
			t.Errorf("%s was requested with no language", r.URL.Path)
		}
		paths = append(paths, r.URL.Path)
		if strings.HasPrefix(r.URL.Path, "/collection/") {
			_, _ = w.Write([]byte(englishCollectionJSON))
			return
		}
		_, _ = w.Write([]byte(movieWithTurkishCollectionJSON))
	})
	if _, err := newClient(t, srv).Fetch(context.Background(), meta.Ref{Kind: meta.KindMovie, ExternalID: "1927"}); err != nil {
		t.Fatal(err)
	}
	if len(paths) < 2 {
		t.Fatalf("expected the movie and its collection to be fetched, got %v", paths)
	}
}
