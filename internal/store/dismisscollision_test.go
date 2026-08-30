package store

import (
	"context"
	"testing"
)

/*
 * Answering the collision report.
 *
 * ADR 0042 decided a shared identity is reported and never resolved, because a
 * server that resolved these would hide real problems. It holds — nothing here
 * merges, ranks or deletes, and both files stay exactly as they are.
 *
 * What it left out is that a person who has looked and decided the pair is fine
 * had no way to say so, so a film in two parts was listed again every time the
 * page opened. A report that cannot be answered is one people stop reading, and
 * that cost falls on the entries that do want attention.
 */

func TestDismissKeyDoesNotDependOnOrder(t *testing.T) {
	// The listing orders members by size and then id, so a file replaced by a
	// differently sized copy of itself would reorder them — and a key that
	// changed with the order would silently un-dismiss.
	if dismissKey([]int64{9, 3, 7}) != dismissKey([]int64{3, 7, 9}) {
		t.Error("the key depends on the order the members arrived in")
	}
}

func TestASetOfOneCannotBeDismissed(t *testing.T) {
	// It would leave a key nothing can ever match: a row that outlives its own
	// meaning and can never be restored because it can never be shown.
	if err := (&Store{}).DismissCollision(context.Background(), []int64{1}, 0); err == nil {
		t.Error("a single row was accepted as a collision")
	}
}

func TestDismissingIsRecordedAndReversible(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)

	if err := st.DismissCollision(ctx, []int64{4, 2}, 1_700_000_000); err != nil {
		t.Fatal(err)
	}
	keys, err := st.dismissedKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if keys["2,4"] != 1_700_000_000 {
		t.Errorf("dismissal not recorded under the sorted key: %v", keys)
	}

	// Reversible, and by naming the same members rather than an opaque handle:
	// the caller has the members and should not have to have kept a key.
	if err := st.RestoreCollision(ctx, []int64{2, 4}); err != nil {
		t.Fatal(err)
	}
	keys, err = st.dismissedKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Errorf("restore left the dismissal behind: %v", keys)
	}
}

/*
 * The assertion that makes this safe to ship.
 *
 * A dismissal describes the set somebody saw. Add a third copy of that film and
 * it must come back, because what is being reported is new — and keying on the
 * work rather than the members would have hidden exactly that.
 */
func TestAChangedMemberSetIsNotDismissed(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)

	if err := st.DismissCollision(ctx, []int64{2, 4}, 1); err != nil {
		t.Fatal(err)
	}
	keys, err := st.dismissedKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := keys[dismissKey([]int64{2, 4, 9})]; ok {
		t.Error("a third copy joined the work and the collision stayed hidden")
	}
	if _, ok := keys[dismissKey([]int64{2})]; ok {
		t.Error("a member left the work and the collision stayed hidden")
	}
}

// Dismissing twice is not an error: two people looking at the same pair on two
// screens is ordinary, and the second press must not fail.
func TestDismissingTwiceJustUpdatesWhen(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)

	if err := st.DismissCollision(ctx, []int64{2, 4}, 100); err != nil {
		t.Fatal(err)
	}
	if err := st.DismissCollision(ctx, []int64{4, 2}, 200); err != nil {
		t.Fatalf("dismissing an already-dismissed collision failed: %v", err)
	}
	keys, _ := st.dismissedKeys(ctx)
	if keys["2,4"] != 200 {
		t.Errorf("the second dismissal did not update the time: %v", keys)
	}
}
