package store

import (
	"context"
	"testing"
)

/*
 * A collection can be renamed by a refresh, and cannot be renamed over a lock.
 *
 * Reported: a Hulk collection displaying as "Hulk Koleksiyonu" — Turkish, on a
 * server whose every other field was English. Half of that was the provider
 * (see internal/meta/tmdb) and half was here: EnsureCollection did nothing on
 * conflict, so a collection was named once, at first sight, for ever. Correcting
 * the provider would have changed nothing on an existing library, which is the
 * only kind anybody has.
 */

func makeCollection(t *testing.T, s *Store, name string) (int64, int64) {
	t.Helper()
	ctx := context.Background()
	lib, err := s.CreateLibrary(ctx, "Films", "movie", `C:\films`)
	if err != nil {
		t.Fatal(err)
	}
	id, created, err := s.EnsureCollection(ctx, lib.ID, "tmdb", "133352", name, name)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("the first EnsureCollection did not report creating the row")
	}
	return lib.ID, id
}

func titleOf(t *testing.T, s *Store, id int64) string {
	t.Helper()
	var title string
	if err := s.db.QueryRow(`SELECT title FROM media_item WHERE id = ?`, id).Scan(&title); err != nil {
		t.Fatal(err)
	}
	return title
}

// The reported fault: a corrected name from the provider must land.
func TestARefreshCorrectsACollectionName(t *testing.T) {
	s := openTestStore(t)
	lib, id := makeCollection(t, s, "Hulk Koleksiyonu")

	again, created, err := s.EnsureCollection(
		context.Background(), lib, "tmdb", "133352", "Hulk Collection", "hulk collection")
	if err != nil {
		t.Fatal(err)
	}
	if again != id {
		t.Errorf("the refresh created a second collection (%d vs %d)", again, id)
	}
	if created {
		t.Error("a rename reported itself as a creation, so the artwork would be " +
			"re-downloaded once per member film")
	}
	if got := titleOf(t, s, id); got != "Hulk Collection" {
		t.Errorf("title = %q, want the corrected name", got)
	}
}

/*
 * And it cannot rename over a person.
 *
 * The locked-fields rule, applied here: a rescan reconciles files, it does not
 * re-litigate decisions. Somebody who renames a collection has made one.
 */
func TestARefreshCannotRenameALockedCollection(t *testing.T) {
	s := openTestStore(t)
	lib, id := makeCollection(t, s, "Hulk Koleksiyonu")
	ctx := context.Background()

	if err := s.LockField(ctx, id, "title"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.EnsureCollection(ctx, lib, "tmdb", "133352",
		"Something The Provider Prefers", "something"); err != nil {
		t.Fatal(err)
	}
	if got := titleOf(t, s, id); got != "Hulk Koleksiyonu" {
		t.Errorf("title = %q — a provider overwrote a locked name", got)
	}
}

// An empty name is not a correction. A provider that returns nothing must not
// blank the collection somebody is looking at.
func TestAnEmptyNameDoesNotBlankACollection(t *testing.T) {
	s := openTestStore(t)
	lib, id := makeCollection(t, s, "Hulk Collection")

	if _, _, err := s.EnsureCollection(
		context.Background(), lib, "tmdb", "133352", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := titleOf(t, s, id); got != "Hulk Collection" {
		t.Errorf("title = %q, want the name to survive an empty update", got)
	}
}

// Re-running with the same name is a no-op that still reports the same row, so
// every member film of a twelve-film franchise does not fight over it.
func TestReEnsuringIsIdempotent(t *testing.T) {
	s := openTestStore(t)
	lib, id := makeCollection(t, s, "Hulk Collection")
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		again, created, err := s.EnsureCollection(
			ctx, lib, "tmdb", "133352", "Hulk Collection", "hulk collection")
		if err != nil {
			t.Fatal(err)
		}
		if again != id || created {
			t.Fatalf("re-ensure %d returned id=%d created=%v; want %d and false",
				i, again, created, id)
		}
	}
}
