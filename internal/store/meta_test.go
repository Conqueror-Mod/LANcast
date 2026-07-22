package store

import (
	"context"
	"testing"
	"time"
)

func seedItem(t *testing.T, st *Store, lib *Library, path string) int64 {
	t.Helper()
	if _, err := st.UpsertItem(context.Background(), file(lib.ID, path, "Seed")); err != nil {
		t.Fatal(err)
	}
	known, _ := st.KnownFiles(context.Background(), lib.ID)
	return known[path].ID
}

func TestSchemaVersionIsCurrent(t *testing.T) {
	st := newStore(t)
	v, err := st.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v != CurrentSchemaVersion {
		t.Errorf("schema version = %d, want %d", v, CurrentSchemaVersion)
	}
}

// Migrations must be safe to re-run, since Open applies them on every start.
func TestMigrationIsIdempotent(t *testing.T) {
	path := t.TempDir() + "/m.db"
	for i := 0; i < 3; i++ {
		st, err := Open(path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		v, _ := st.SchemaVersion()
		if v != CurrentSchemaVersion {
			t.Errorf("version after open #%d = %d, want %d", i, v, CurrentSchemaVersion)
		}
		st.Close()
	}
}

func TestLockRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\a.mkv`)

	if got, _ := st.LockedFields(ctx, id); len(got) != 0 {
		t.Errorf("locks = %v, want none initially", got)
	}

	if err := st.LockField(ctx, id, "title"); err != nil {
		t.Fatal(err)
	}
	// Locking twice must not error.
	if err := st.LockField(ctx, id, "title"); err != nil {
		t.Fatalf("second LockField: %v", err)
	}
	st.LockField(ctx, id, "year")

	got, _ := st.LockedFields(ctx, id)
	if len(got) != 2 || got[0] != "title" || got[1] != "year" {
		t.Errorf("locks = %v, want [title year]", got)
	}

	if err := st.UnlockField(ctx, id, "title"); err != nil {
		t.Fatal(err)
	}
	got, _ = st.LockedFields(ctx, id)
	if len(got) != 1 || got[0] != "year" {
		t.Errorf("locks after unlock = %v, want [year]", got)
	}
}

func TestUpdateItemMetadataPartial(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\a.mkv`)

	title, overview := "Arrival", "A linguist makes contact."
	if err := st.UpdateItemMetadata(ctx, id, ItemMetadata{Title: &title, Overview: &overview}); err != nil {
		t.Fatal(err)
	}

	it, err := st.GetItem(ctx, id, "local")
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "Arrival" {
		t.Errorf("Title = %q, want Arrival", it.Title)
	}
	if it.Overview == nil || *it.Overview != overview {
		t.Errorf("Overview = %v, want the synopsis", it.Overview)
	}
	// A nil field must be left alone, not blanked.
	if it.Container == nil || *it.Container != "mkv" {
		t.Errorf("Container = %v, want mkv untouched", it.Container)
	}
}

// Writing metadata stamps metadata_updated_at, which is what removes the item
// from the enrichment queue.
func TestEnrichmentQueue(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	a := seedItem(t, st, lib, `C:\m\a.mkv`)
	seedItem(t, st, lib, `C:\m\b.mkv`)

	pending, err := st.PendingEnrichment(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending = %d, want 2", len(pending))
	}

	title := "Done"
	st.UpdateItemMetadata(ctx, a, ItemMetadata{Title: &title})

	pending, _ = st.PendingEnrichment(ctx, 10)
	if len(pending) != 1 {
		t.Errorf("pending after enrichment = %d, want 1", len(pending))
	}

	// Requeue for a manual refresh.
	if err := st.ClearMetadataStamp(ctx, 0, a); err != nil {
		t.Fatal(err)
	}
	pending, _ = st.PendingEnrichment(ctx, 10)
	if len(pending) != 2 {
		t.Errorf("pending after requeue = %d, want 2", len(pending))
	}
}

// A locked match is never re-enriched: rescans reconcile files, they do not
// re-litigate identity.
func TestLockedMatchIsExcludedFromEnrichment(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\a.mkv`)

	if err := st.SetMatch(ctx, id, "tmdb", "335984", "locked", 1.0); err != nil {
		t.Fatal(err)
	}
	pending, _ := st.PendingEnrichment(ctx, 10)
	for _, p := range pending {
		if p.ID == id {
			t.Fatal("a locked item appeared in the enrichment queue")
		}
	}
}

func TestSetMatchAndReviewQueue(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	good := seedItem(t, st, lib, `C:\m\good.mkv`)
	iffy := seedItem(t, st, lib, `C:\m\iffy.mkv`)
	none := seedItem(t, st, lib, `C:\m\none.mkv`)

	st.SetMatch(ctx, good, "tmdb", "1", "matched", 0.95)
	st.SetMatch(ctx, iffy, "tmdb", "2", "review", 0.62)
	st.SetMatch(ctx, none, "tmdb", "", "unmatched", 0.1)

	// Only enriched items qualify; stamp the ones that were "looked at".
	for _, id := range []int64{good, iffy, none} {
		if err := st.UpdateItemMetadata(ctx, id, ItemMetadata{}); err != nil {
			t.Fatal(err)
		}
	}

	queue, err := st.ReviewQueue(ctx, lib.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 2 {
		t.Fatalf("review queue = %d items, want 2 (review + unmatched)", len(queue))
	}
	for _, it := range queue {
		if it.ID == good {
			t.Error("a confidently matched item appeared in the review queue")
		}
	}

	it, _ := st.GetItem(ctx, good, "local")
	if it.MatchState != "matched" || it.MatchScore == nil || *it.MatchScore != 0.95 {
		t.Errorf("match = %s/%v, want matched/0.95", it.MatchState, it.MatchScore)
	}
	if it.Provider == nil || *it.Provider != "tmdb" {
		t.Errorf("Provider = %v, want tmdb", it.Provider)
	}
}

func TestNewItemsDefaultToUnmatched(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\a.mkv`)

	it, _ := st.GetItem(ctx, id, "local")
	if it.MatchState != "unmatched" {
		t.Errorf("MatchState = %q, want unmatched", it.MatchState)
	}
}

// A freshly scanned library must not report every title as needing review.
// Nothing has looked at them yet, and "not attempted" is not "no match found".
func TestUnenrichedItemsAreNotInReviewQueue(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	seedItem(t, st, lib, `C:\m\a.mkv`)
	seedItem(t, st, lib, `C:\m\b.mkv`)

	queue, err := st.ReviewQueue(ctx, lib.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 0 {
		t.Errorf("review queue = %d, want 0 before any enrichment has run", len(queue))
	}
}

func TestReplaceGenres(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\a.mkv`)

	if err := st.ReplaceGenres(ctx, id, []string{"Science Fiction", "Drama"}); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Genres(ctx, id)
	if len(got) != 2 || got[0] != "Drama" || got[1] != "Science Fiction" {
		t.Errorf("genres = %v, want [Drama, Science Fiction]", got)
	}

	// Replacing must not accumulate.
	st.ReplaceGenres(ctx, id, []string{"Horror"})
	got, _ = st.Genres(ctx, id)
	if len(got) != 1 || got[0] != "Horror" {
		t.Errorf("genres after replace = %v, want [Horror]", got)
	}

	// A shared genre name must be reused, not duplicated.
	other := seedItem(t, st, lib, `C:\m\b.mkv`)
	st.ReplaceGenres(ctx, other, []string{"Horror"})
	var n int
	st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM genre WHERE name = 'Horror'`).Scan(&n)
	if n != 1 {
		t.Errorf("genre rows for Horror = %d, want 1", n)
	}
}

func TestReplaceCredits(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\a.mkv`)

	credits := []Credit{
		{Name: "Amy Adams", Role: "actor", Character: "Louise Banks"},
		{Name: "Denis Villeneuve", Role: "director"},
	}
	if err := st.ReplaceCredits(ctx, id, "tmdb", credits); err != nil {
		t.Fatal(err)
	}

	got, _ := st.Credits(ctx, id)
	if len(got) != 2 {
		t.Fatalf("credits = %d, want 2", len(got))
	}
	// Billing order must be preserved.
	if got[0].Name != "Amy Adams" || got[0].Character != "Louise Banks" {
		t.Errorf("first credit = %+v, want Amy Adams as Louise Banks", got[0])
	}
	if got[1].Role != "director" {
		t.Errorf("second credit role = %q, want director", got[1].Role)
	}

	st.ReplaceCredits(ctx, id, "tmdb", []Credit{{Name: "Someone", Role: "actor"}})
	got, _ = st.Credits(ctx, id)
	if len(got) != 1 {
		t.Errorf("credits after replace = %d, want 1", len(got))
	}
}

func TestArtworkRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\a.mkv`)

	if got, _ := st.ItemArtwork(ctx, id); got != nil {
		t.Errorf("artwork = %+v, want nil before any is stored", got)
	}

	if err := st.PutArtwork(ctx, id, "abc123", "poster", "https://x/p.jpg", 342, 513, 1024); err != nil {
		t.Fatal(err)
	}
	st.PutArtwork(ctx, id, "def456", "fanart", "https://x/f.jpg", 1280, 720, 4096)

	art, _ := st.ItemArtwork(ctx, id)
	if art == nil || art.Poster != "abc123" || art.Fanart != "def456" {
		t.Errorf("artwork = %+v, want poster abc123 and fanart def456", art)
	}

	// Content addressing: the same bytes stored twice is one row.
	other := seedItem(t, st, lib, `C:\m\b.mkv`)
	if err := st.PutArtwork(ctx, other, "abc123", "poster", "https://x/p.jpg", 342, 513, 1024); err != nil {
		t.Fatal(err)
	}
	var n int
	st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artwork WHERE hash = 'abc123'`).Scan(&n)
	if n != 1 {
		t.Errorf("artwork rows for one hash = %d, want 1 — shared images store once", n)
	}

	exists, _ := st.ArtworkExists(ctx, "abc123")
	if !exists {
		t.Error("ArtworkExists = false for a stored hash")
	}
	if exists, _ := st.ArtworkExists(ctx, "nope"); exists {
		t.Error("ArtworkExists = true for an unknown hash")
	}
}

// The grid renders from the list endpoint, so artwork has to arrive in bulk
// with the page. Without this, posters are downloaded, stored, and never seen.
func TestAttachArtwork(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	a := seedItem(t, st, lib, `C:\m\a.mkv`)
	b := seedItem(t, st, lib, `C:\m\b.mkv`)
	seedItem(t, st, lib, `C:\m\c.mkv`) // no artwork

	st.PutArtwork(ctx, a, "hash-poster-a", "poster", "u", 342, 513, 1)
	st.PutArtwork(ctx, a, "hash-fanart-a", "fanart", "u", 1280, 720, 1)
	st.PutArtwork(ctx, b, "hash-poster-b", "poster", "u", 342, 513, 1)

	items, _, err := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Artwork != nil {
			t.Fatal("ListItems attached artwork on its own; the test would not prove anything")
		}
	}

	if err := st.AttachArtwork(ctx, items); err != nil {
		t.Fatalf("AttachArtwork: %v", err)
	}

	got := map[int64]*Artwork{}
	for i := range items {
		got[items[i].ID] = items[i].Artwork
	}
	if got[a] == nil || got[a].Poster != "hash-poster-a" || got[a].Fanart != "hash-fanart-a" {
		t.Errorf("item a artwork = %+v", got[a])
	}
	if got[b] == nil || got[b].Poster != "hash-poster-b" || got[b].Fanart != "" {
		t.Errorf("item b artwork = %+v", got[b])
	}
	for id, art := range got {
		if id != a && id != b && art != nil {
			t.Errorf("item %d got artwork it should not have: %+v", id, art)
		}
	}

	if err := st.AttachArtwork(ctx, nil); err != nil {
		t.Errorf("AttachArtwork(nil) = %v, want nil", err)
	}
}

func TestProviderCache(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)

	if _, ok, _ := st.CachedResponse(ctx, "tmdb", "search:arrival", time.Hour); ok {
		t.Error("cache hit before anything was stored")
	}

	payload := []byte(`{"results":[]}`)
	if err := st.CacheResponse(ctx, "tmdb", "search:arrival", payload); err != nil {
		t.Fatal(err)
	}

	got, ok, err := st.CachedResponse(ctx, "tmdb", "search:arrival", time.Hour)
	if err != nil || !ok {
		t.Fatalf("cache miss after store: ok=%v err=%v", ok, err)
	}
	if string(got) != string(payload) {
		t.Errorf("payload = %s, want %s", got, payload)
	}

	// A zero maxAge means "any age is acceptable".
	if _, ok, _ := st.CachedResponse(ctx, "tmdb", "search:arrival", 0); !ok {
		t.Error("zero maxAge should accept any cached entry")
	}
	// An expired entry is a miss.
	if _, ok, _ := st.CachedResponse(ctx, "tmdb", "search:arrival", time.Nanosecond); ok {
		t.Error("expired entry returned as a hit")
	}

	st.CacheResponse(ctx, "tmdb", "search:arrival", []byte("updated"))
	got, _, _ = st.CachedResponse(ctx, "tmdb", "search:arrival", time.Hour)
	if string(got) != "updated" {
		t.Errorf("payload = %s, want the updated value", got)
	}
}

func TestLoadDetail(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\a.mkv`)

	st.ReplaceGenres(ctx, id, []string{"Drama"})
	st.ReplaceCredits(ctx, id, "tmdb", []Credit{{Name: "Someone", Role: "actor"}})
	st.PutArtwork(ctx, id, "hash1", "poster", "u", 1, 1, 1)
	st.LockField(ctx, id, "title")

	it, _ := st.GetItem(ctx, id, "local")
	if err := st.LoadDetail(ctx, it); err != nil {
		t.Fatal(err)
	}
	if len(it.Genres) != 1 || len(it.Credits) != 1 || it.Artwork == nil || len(it.LockedFields) != 1 {
		t.Errorf("detail incomplete: genres=%v credits=%v art=%v locks=%v",
			it.Genres, it.Credits, it.Artwork, it.LockedFields)
	}
}

// Deleting an item must not orphan its metadata.
func TestCascadeDeleteCleansMetadata(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `C:\m\a.mkv`)

	st.LockField(ctx, id, "title")
	st.ReplaceGenres(ctx, id, []string{"Drama"})
	st.PutArtwork(ctx, id, "h", "poster", "u", 1, 1, 1)

	if _, err := st.db.ExecContext(ctx, `DELETE FROM media_item WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"item_lock", "item_genre", "item_artwork"} {
		var n int
		st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE item_id = ?`, id).Scan(&n)
		if n != 0 {
			t.Errorf("%s still has %d rows after the item was deleted", table, n)
		}
	}
}
