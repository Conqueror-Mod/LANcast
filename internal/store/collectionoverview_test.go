package store

import (
	"context"
	"testing"
)

func overviewOf(t *testing.T, s *Store, id int64) string {
	t.Helper()
	var v *string
	if err := s.db.QueryRow(`SELECT overview FROM media_item WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v == nil {
		return ""
	}
	return *v
}

// A provider's description lands on a collection, and a newer one replaces it.
func TestACollectionTakesItsProviderDescription(t *testing.T) {
	s := openTestStore(t)
	_, id := makeCollection(t, s, "Hulk Collection")
	ctx := context.Background()

	if err := s.SetCollectionOverview(ctx, id, "The Hulk films."); err != nil {
		t.Fatal(err)
	}
	if got := overviewOf(t, s, id); got != "The Hulk films." {
		t.Errorf("overview = %q", got)
	}
	if err := s.SetCollectionOverview(ctx, id, "Both Hulk films."); err != nil {
		t.Fatal(err)
	}
	if got := overviewOf(t, s, id); got != "Both Hulk films." {
		t.Errorf("a refreshed description did not land: %q", got)
	}
	// An empty answer is "the provider has none", not "clear it".
	if err := s.SetCollectionOverview(ctx, id, ""); err != nil {
		t.Fatal(err)
	}
	if got := overviewOf(t, s, id); got != "Both Hulk films." {
		t.Errorf("an empty provider answer cleared the description: %q", got)
	}
}

// The locked-fields rule: a description somebody wrote is never overwritten.
func TestAProviderCannotOverwriteAWrittenCollectionDescription(t *testing.T) {
	s := openTestStore(t)
	_, id := makeCollection(t, s, "Hulk Collection")
	ctx := context.Background()

	if _, err := s.db.Exec(`UPDATE media_item SET overview = 'Mine.' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.LockField(ctx, id, "overview"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCollectionOverview(ctx, id, "The provider's."); err != nil {
		t.Fatal(err)
	}
	if got := overviewOf(t, s, id); got != "Mine." {
		t.Errorf("a locked description was overwritten: %q", got)
	}
}

// Only a collection row: a stray id must not rewrite a film's overview.
func TestSetCollectionOverviewTouchesOnlyCollections(t *testing.T) {
	s := openTestStore(t)
	lib, _ := makeCollection(t, s, "Hulk Collection")
	ctx := context.Background()

	res, err := s.db.Exec(`
		INSERT INTO media_item (library_id, kind, path, title, sort_title, overview,
		                        added_at, updated_at, missing)
		VALUES (?, 'movie', 'C:\films\hulk.mkv', 'Hulk', 'hulk', 'A film.', 0, 0, 0)`, lib)
	if err != nil {
		t.Fatal(err)
	}
	film, _ := res.LastInsertId()
	if err := s.SetCollectionOverview(ctx, film, "Not a film's."); err != nil {
		t.Fatal(err)
	}
	if got := overviewOf(t, s, film); got != "A film." {
		t.Errorf("a film's overview was rewritten: %q", got)
	}
}
