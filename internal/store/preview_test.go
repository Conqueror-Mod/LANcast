package store

import (
	"context"
	"testing"
)

// Pending: probed films and episodes with no previews key, or one for a file
// that has since changed, or made at an older rev (ADR 0105).
func TestPreviewsAreKeyedToTheFile(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	film := seedMarkerFilm(t, st, "Probed", 6_000_000, true)
	seedMarkerFilm(t, st, "Unprobed", 6_000_000, false)

	pending := func(rev int) []int64 {
		t.Helper()
		items, err := st.PendingPreviews(ctx, rev, 10)
		if err != nil {
			t.Fatal(err)
		}
		n, err := st.PendingPreviewsCount(ctx, rev)
		if err != nil || n != len(items) {
			t.Fatalf("count %d (%v) against %d listed", n, err, len(items))
		}
		var ids []int64
		for _, it := range items {
			ids = append(ids, it.ID)
		}
		return ids
	}
	if got := pending(1); len(got) != 1 || got[0] != film {
		t.Fatalf("pending = %v, want only the probed film %d", got, film)
	}

	it, err := st.GetItem(ctx, film, "")
	if err != nil {
		t.Fatal(err)
	}
	key := PreviewKey(*it, 1)
	if key != "1:1:1" {
		t.Fatalf("key = %q", key)
	}
	if err := st.SetPreviewKey(ctx, film, key); err != nil {
		t.Fatal(err)
	}
	if got := pending(1); len(got) != 0 {
		t.Fatalf("still pending after its pass: %v", got)
	}
	// A new rev sends it back.
	if got := pending(2); len(got) != 1 {
		t.Fatalf("a new rev did not send it back: %v", got)
	}
	// So does a replaced file.
	if _, err := st.db.ExecContext(ctx, `UPDATE media_item SET size_bytes = 2 WHERE id = ?`, film); err != nil {
		t.Fatal(err)
	}
	if got := pending(1); len(got) != 1 {
		t.Fatalf("a changed file did not send it back: %v", got)
	}
}

func TestExistingItemsIsWhatIsThere(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	a := seedMarkerFilm(t, st, "A", 1000, true)
	got, err := st.ExistingItems(ctx, []int64{a, a + 1000})
	if err != nil {
		t.Fatal(err)
	}
	if !got[a] || got[a+1000] {
		t.Fatalf("%v", got)
	}
}
