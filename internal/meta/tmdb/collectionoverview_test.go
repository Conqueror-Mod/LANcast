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
}
