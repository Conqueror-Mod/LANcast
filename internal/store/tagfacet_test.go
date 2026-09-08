package store

import (
	"context"
	"testing"
)

/*
 * Tags in the facets (ADR 0062), and the trap underneath them.
 *
 * The tag query joins media_item **unaliased**, because topLevelPredicate
 * embeds collectionIsReal, which refers to `media_item.id` by name in a
 * correlated subquery. Aliased, that name is unbound and SQLite refuses the
 * statement — and because the tag query runs first, the function returned early
 * and *every other facet came back empty*. A change about tags removed the
 * initials rail and the content ratings.
 *
 * The existing suite caught it, which is the system working. This test is here
 * so the next person to touch the query finds out from a name rather than from
 * two unrelated failures.
 */
func TestTagFacetsDoNotBreakTheOtherFacets(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "", "owner", "hash", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.UpsertItem(ctx, ScanFile{
		LibraryID: lib.ID, Path: "alien.mkv", Kind: "movie",
		Title: "Alien", SortTitle: "Alien", Container: "mkv", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddTag(ctx, u.ID, id, "rewatch"); err != nil {
		t.Fatal(err)
	}

	f, err := st.LibraryFacets(ctx, lib.ID, u.ID)
	if err != nil {
		t.Fatalf("facets failed: %v", err)
	}
	if len(f.Tags) != 1 || f.Tags[0].Name != "rewatch" {
		t.Errorf("tags = %v, want the one tag", f.Tags)
	}
	// The half that actually broke: everything downstream of the tag query.
	if len(f.Initials) == 0 {
		t.Error("the initials rail is empty — the tag query failed and took the " +
			"rest of the facets with it")
	}
}

// Another account's tags are not in the facets, which is where a filter row
// would otherwise disclose the names.
func TestTagFacetsAreScopedToTheCaller(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	mine, _ := st.CreateUser(ctx, "", "mine", "hash", RoleAdmin)
	yours, _ := st.CreateUser(ctx, "", "yours", "hash", RoleMember)
	lib, _ := st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	id, err := st.UpsertItem(ctx, ScanFile{
		LibraryID: lib.ID, Path: "a.mkv", Kind: "movie",
		Title: "A", SortTitle: "A", Container: "mkv", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddTag(ctx, mine.ID, id, "sell these"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetFavourite(ctx, mine.ID, id, true); err != nil {
		t.Fatal(err)
	}

	f, err := st.LibraryFacets(ctx, lib.ID, yours.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Tags) != 0 {
		t.Errorf("the filter row would offer %v to another account", f.Tags)
	}
	if f.HasFavourites {
		t.Error("another account's favourites were offered as a filter")
	}
}
