package enrich

import (
	"context"
	"testing"

	"lancast/internal/meta"
)

/*
 * A collection's film list (ADR 0093) arrives with a member's enrichment, and
 * a poster is fetched for the missing film only: the owned one has its own.
 */
func TestEnrichStoresTheCollectionsFilmsAndMissingPosters(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	id := addItem(t, st, lib, `C:\m\lotr1.mkv`, "Fellowship", 2001)

	p := &fakeProvider{
		id: "fake",
		record: &meta.Record{
			Source: "fake", ExternalID: "120", Kind: meta.KindMovie,
			Fields: meta.Fields{Title: meta.S("Fellowship"), Year: meta.I(2001)},
			Collection: &meta.CollectionRef{
				ExternalID: "119", Name: "The Lord of the Rings Collection", Overview: "Three films.",
				Films: []meta.CollectionFilm{
					{ExternalID: "120", Title: "Fellowship", Year: 2001, ReleasedAt: 1008633600, PosterURL: "https://img/120.jpg"},
					{ExternalID: "121", Title: "Two Towers", Year: 2002, ReleasedAt: 1040169600, PosterURL: "https://img/121.jpg"},
				},
			},
		},
		cands: []meta.Candidate{{Provider: "fake", ExternalID: "120", Kind: meta.KindMovie, Title: "Fellowship", Year: 2001, Popularity: 40}},
	}
	reg := meta.NewRegistry()
	reg.AddProvider(p)
	art := &fakeArt{}
	if err := New(st, reg, art, quietLog()).Run(ctx); err != nil {
		t.Fatal(err)
	}

	cols, err := st.CollectionsOf(ctx, id)
	if err != nil || len(cols) != 1 {
		t.Fatalf("CollectionsOf = %+v, %v", cols, err)
	}
	films, err := st.CollectionFilmsFor(ctx, cols[0].ID, "local")
	if err != nil {
		t.Fatal(err)
	}
	if len(films) != 2 || films[0].ItemID != id || films[1].ItemID != 0 {
		t.Fatalf("films = %+v, want Fellowship owned and Two Towers missing", films)
	}
	if films[1].Poster == "" {
		t.Error("the missing film has no poster")
	}
	for _, u := range art.downloaded {
		if u == "https://img/120.jpg" {
			t.Error("a poster was fetched for a film the library already has")
		}
	}
}

// fakeCollections is a provider that can look a collection up by its id.
type fakeCollections struct {
	fakeProvider
	asked []string
}

func (f *fakeCollections) Collection(ctx context.Context, externalID string) (*meta.CollectionRef, error) {
	f.asked = append(f.asked, externalID)
	return &meta.CollectionRef{
		ExternalID: externalID, Name: "Old", Overview: "Described at last.",
		Films: []meta.CollectionFilm{{ExternalID: "7", Title: "First", ReleasedAt: 1}},
	}, nil
}

/*
 * A collection made before film lists existed is given one at the end of an
 * enrichment pass, by asking about the collection itself — not by enriching
 * its members again.
 */
func TestAnOldCollectionIsBackfilled(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	coll, _, err := st.EnsureCollection(ctx, lib.ID, "fake", "999", "Old", "old")
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeCollections{fakeProvider: fakeProvider{id: "fake"}}
	reg := meta.NewRegistry()
	reg.AddProvider(p)
	if err := New(st, reg, &fakeArt{}, quietLog()).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(p.asked) != 1 || p.asked[0] != "999" {
		t.Fatalf("asked about %v, want the old collection once", p.asked)
	}
	if p.fetchN != 0 {
		t.Errorf("fetched %d films; the backfill must not re-enrich members", p.fetchN)
	}
	films, _ := st.CollectionFilmsFor(ctx, coll, "local")
	if len(films) != 1 || films[0].Title != "First" {
		t.Errorf("films = %+v", films)
	}
	it, _ := st.GetItem(ctx, coll, "local")
	if it.Overview == nil || *it.Overview != "Described at last." {
		t.Errorf("overview = %v, want the collection's description", it.Overview)
	}

	// Once listed it is not asked about again.
	if err := New(st, reg, &fakeArt{}, quietLog()).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(p.asked) != 1 {
		t.Errorf("asked again: %v", p.asked)
	}
}
