package store

import (
	"context"
	"errors"
	"testing"
)

/*
 * Tags and favourites (ADR 0062).
 *
 * The plumbing — add, remove, list — is worth a test each and would be caught
 * by using the feature once. The privacy would not. A tag is a note somebody
 * wrote to themselves, and a leak of one does not fail: it renders, in
 * somebody else's list, looking exactly like a feature working.
 *
 * So the cases below are mostly about what one account *cannot* see, and one of
 * them is about the tag's existence rather than its items — because a filter row
 * that lists "sell these" has disclosed the interesting part even if every item
 * under it is unreachable.
 */

func tagFixture(t *testing.T) (*Store, string, string, int64) {
	t.Helper()
	st := openTestStore(t)
	ctx := context.Background()
	mine, err := st.CreateUser(ctx, "", "mine", "hash", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	yours, err := st.CreateUser(ctx, "", "yours", "hash", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.UpsertItem(ctx, ScanFile{
		LibraryID: lib.ID, Path: "a.mkv", Kind: "movie",
		Title: "A", SortTitle: "A", Container: "mkv", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return st, mine.ID, yours.ID, id
}

/*
 * The whole decision, as one assertion: your tags are not merely hidden from
 * somebody else's view of the item, they are not selected.
 */
func TestATagIsInvisibleToAnotherAccount(t *testing.T) {
	st, mine, yours, item := tagFixture(t)
	ctx := context.Background()

	if _, err := st.AddTag(ctx, mine, item, "sell these"); err != nil {
		t.Fatal(err)
	}

	got, err := st.ItemTags(ctx, yours, item)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("another account sees %v on the same item", got)
	}
}

/*
 * And the *existence* of the name is invisible, which is the reason the
 * vocabulary is per-account rather than a shared table with a per-user join.
 *
 * A shared name table would leak here even with every item unreachable, and it
 * would leak through the one call a filter row makes.
 */
func TestAnotherAccountCannotSeeThatTheTagExists(t *testing.T) {
	st, mine, yours, item := tagFixture(t)
	ctx := context.Background()

	if _, err := st.AddTag(ctx, mine, item, "sell these"); err != nil {
		t.Fatal(err)
	}

	tags, err := st.ListTags(ctx, yours)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 0 {
		t.Errorf("the tag list leaked %v — the existence is the private part", tags)
	}
}

// Nor can another account remove one.
func TestATagCannotBeRemovedByAnotherAccount(t *testing.T) {
	st, mine, yours, item := tagFixture(t)
	ctx := context.Background()

	tag, err := st.AddTag(ctx, mine, item, "keep")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveTag(ctx, yours, item, tag.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	got, _ := st.ItemTags(ctx, mine, item)
	if len(got) != 1 {
		t.Error("somebody else's removal took the tag away")
	}
}

/*
 * Two accounts using one word are two tags.
 *
 * The duplication a shared vocabulary would avoid, kept on purpose: it is what
 * makes the privacy structural rather than a filter everything has to remember.
 */
func TestTwoAccountsUsingOneWordGetTwoTags(t *testing.T) {
	st, mine, yours, item := tagFixture(t)
	ctx := context.Background()

	a, err := st.AddTag(ctx, mine, item, "christmas")
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.AddTag(ctx, yours, item, "christmas")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatal("two accounts share one tag row; removing one would remove the other")
	}
	for _, c := range []struct {
		user string
		id   int64
	}{{mine, a.ID}, {yours, b.ID}} {
		got, _ := st.ItemTags(ctx, c.user, item)
		if len(got) != 1 || got[0].ID != c.id {
			t.Errorf("%s sees %v", c.user, got)
		}
	}
}

// Case and spacing fold together, and the first spelling is the one kept.
func TestCaseAndSpacingFoldToOneTag(t *testing.T) {
	st, mine, _, item := tagFixture(t)
	ctx := context.Background()

	first, err := st.AddTag(ctx, mine, item, "Christmas")
	if err != nil {
		t.Fatal(err)
	}
	again, err := st.AddTag(ctx, mine, item, "  christmas  ")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID {
		t.Fatal("a differently-cased tag made a second row")
	}
	if again.Name != "Christmas" {
		t.Errorf("display name became %q; the first spelling should stand", again.Name)
	}
}

// A tag nothing carries stops existing, or the filter row fills with typos.
func TestATagWithNothingTaggedIsRemoved(t *testing.T) {
	st, mine, _, item := tagFixture(t)
	ctx := context.Background()

	tag, err := st.AddTag(ctx, mine, item, "temporary")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveTag(ctx, mine, item, tag.ID); err != nil {
		t.Fatal(err)
	}
	tags, err := st.ListTags(ctx, mine)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 0 {
		t.Errorf("the empty tag survived: %v", tags)
	}
}

// A name that folds to nothing is refused: a tag nobody can type is one nobody
// can remove.
func TestAnEmptyTagIsRefused(t *testing.T) {
	st, mine, _, item := tagFixture(t)
	if _, err := st.AddTag(context.Background(), mine, item, "   "); !errors.Is(err, ErrEmptyTag) {
		t.Errorf("err = %v, want ErrEmptyTag", err)
	}
}

// Favourites are per-account in the same way.
func TestAFavouriteIsOneAccountsOpinion(t *testing.T) {
	st, mine, yours, item := tagFixture(t)
	ctx := context.Background()

	if err := st.SetFavourite(ctx, mine, item, true); err != nil {
		t.Fatal(err)
	}
	if on, _ := st.IsFavourite(ctx, mine, item); !on {
		t.Error("the favourite did not stick")
	}
	if on, _ := st.IsFavourite(ctx, yours, item); on {
		t.Error("another account's favourite showed as theirs")
	}
}

/*
 * Filtering by tag is scoped to the caller too.
 *
 * The filter is where a leak would be least visible: a grid quietly containing
 * somebody else's selection looks like a grid.
 */
func TestFilteringByTagIsScopedToTheCaller(t *testing.T) {
	st, mine, yours, item := tagFixture(t)
	ctx := context.Background()

	tag, err := st.AddTag(ctx, mine, item, "christmas")
	if err != nil {
		t.Fatal(err)
	}

	items, _, err := st.ListItems(ctx, ItemFilter{
		UserID: yours, TagIDs: []int64{tag.ID}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("another account's tag id returned %d items", len(items))
	}

	items, _, err = st.ListItems(ctx, ItemFilter{
		UserID: mine, TagIDs: []int64{tag.ID}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != item {
		t.Errorf("the owner's own filter returned %d items", len(items))
	}
}
