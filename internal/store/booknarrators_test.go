package store

import (
	"context"
	"reflect"
	"testing"
)

// Books narrow by narrator, author and series, and the facets offer the
// authors and series an audiobooks library holds (ADR 0116 step 3).
func TestBookFiltersAndFacets(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib, err := st.CreateLibrary(ctx, "Books", "audiobooks", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	book := func(key, title, author, series string, narrators ...string) int64 {
		t.Helper()
		id, err := st.EnsureDerivedContainer(ctx, lib.ID, "book", key, title, title, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetBookDetails(ctx, id, BookDetails{
			Title: title, SortTitle: title, Author: author, Series: series,
		}); err != nil {
			t.Fatal(err)
		}
		if err := st.SetBookNarrators(ctx, id, narrators); err != nil {
			t.Fatal(err)
		}
		return id
	}
	qsq := book("k1", "Q-Squared", "Peter David", "The Next Generation", "John de Lancie", "Leonard Nimoy")
	vendetta := book("k2", "Vendetta", "Peter David", "The Next Generation", "John de Lancie")
	warped := book("k3", "Warped", "K. W. Jeter", "Deep Space Nine", "Armin Shimerman")

	credits, err := st.Credits(ctx, qsq)
	if err != nil {
		t.Fatal(err)
	}
	var nimoy, delancie int64
	for _, c := range credits {
		switch c.Name {
		case "Leonard Nimoy":
			nimoy = c.PersonID
		case "John de Lancie":
			delancie = c.PersonID
		}
		if c.Role != RoleNarrator {
			t.Errorf("%s credited as %q", c.Name, c.Role)
		}
	}

	ids := func(f ItemFilter) []int64 {
		t.Helper()
		f.LibraryID, f.TopLevel, f.Sort = lib.ID, true, "title"
		items, _, err := st.ListItems(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		out := []int64{}
		for _, it := range items {
			out = append(out, it.ID)
		}
		return out
	}
	// One reader across two books is one person, found in both.
	if got := ids(ItemFilter{NarratorIDs: []int64{delancie}}); !reflect.DeepEqual(got, []int64{qsq, vendetta}) {
		t.Errorf("narrated by de Lancie = %v, want Q-Squared and Vendetta", got)
	}
	// The second voice of a full cast finds the book too.
	if got := ids(ItemFilter{NarratorIDs: []int64{nimoy}}); !reflect.DeepEqual(got, []int64{qsq}) {
		t.Errorf("narrated by Nimoy = %v, want Q-Squared", got)
	}
	if got := ids(ItemFilter{Authors: []string{"K. W. Jeter"}}); !reflect.DeepEqual(got, []int64{warped}) {
		t.Errorf("by Jeter = %v, want Warped", got)
	}
	if got := ids(ItemFilter{Series: []string{"The Next Generation"}, Authors: []string{"Peter David"}}); len(got) != 2 {
		t.Errorf("TNG by David = %v, want two", got)
	}

	f, err := st.LibraryFacets(ctx, lib.ID, "local")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.Authors, []string{"K. W. Jeter", "Peter David"}) {
		t.Errorf("authors facet = %q", f.Authors)
	}
	if !reflect.DeepEqual(f.Series, []string{"Deep Space Nine", "The Next Generation"}) {
		t.Errorf("series facet = %q", f.Series)
	}

	// Setting the same narrators again writes nothing, and nil clears them.
	if err := st.SetBookNarrators(ctx, warped, nil); err != nil {
		t.Fatal(err)
	}
	if got := ids(ItemFilter{NarratorIDs: []int64{nimoy, delancie}}); len(got) != 2 {
		t.Errorf("after clearing Warped, narrator filter = %v", got)
	}
}
