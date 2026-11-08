package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

/*
 * Pinned shelves (ADR 0119 §2, revision 91): kept per account, in a dense
 * order, at most twelve, and gone with their library or their account.
 */

func shelfTitles(t *testing.T, st *Store, user string) []string {
	t.Helper()
	hs, err := st.HomeShelves(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for i, h := range hs {
		if h.Position != i {
			t.Errorf("shelf %q at position %d, want %d: positions must stay dense", h.Title, h.Position, i)
		}
		out = append(out, h.Title)
	}
	return out
}

func pinFixture(t *testing.T) (*Store, string, int64) {
	t.Helper()
	st, me, film := viewingFixture(t)
	it, err := st.GetItem(context.Background(), film, me)
	if err != nil {
		t.Fatal(err)
	}
	return st, me, it.LibraryID
}

func TestPinnedShelvesKeepTheirOrder(t *testing.T) {
	st, me, lib := pinFixture(t)
	ctx := context.Background()
	var ids []int64
	for _, title := range []string{"A", "B", "C", "D"} {
		h, err := st.AddHomeShelf(ctx, me, lib, title, "genre=Drama")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, h.ID)
	}
	if got := fmt.Sprint(shelfTitles(t, st, me)); got != "[A B C D]" {
		t.Fatalf("order = %s", got)
	}

	if err := st.MoveHomeShelf(ctx, me, ids[3], 0); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(shelfTitles(t, st, me)); got != "[D A B C]" {
		t.Errorf("after moving D to the top = %s", got)
	}
	// Past the end clamps to the end rather than leaving a gap.
	if err := st.MoveHomeShelf(ctx, me, ids[3], 99); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(shelfTitles(t, st, me)); got != "[A B C D]" {
		t.Errorf("after moving D past the end = %s", got)
	}

	if err := st.DeleteHomeShelf(ctx, me, ids[1]); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(shelfTitles(t, st, me)); got != "[A C D]" {
		t.Errorf("after unpinning B = %s", got)
	}
	if err := st.RenameHomeShelf(ctx, me, ids[0], "Films I keep meaning to see"); err != nil {
		t.Fatal(err)
	}
	if got := shelfTitles(t, st, me)[0]; got != "Films I keep meaning to see" {
		t.Errorf("renamed shelf = %q", got)
	}
}

func TestPinnedShelvesStopAtTwelve(t *testing.T) {
	st, me, lib := pinFixture(t)
	ctx := context.Background()
	for i := 0; i < MaxHomeShelves; i++ {
		if _, err := st.AddHomeShelf(ctx, me, lib, fmt.Sprint(i), ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.AddHomeShelf(ctx, me, lib, "one too many", ""); !errors.Is(err, ErrHomeShelfLimit) {
		t.Errorf("thirteenth pin: err = %v, want ErrHomeShelfLimit", err)
	}
}

// One account cannot see, rename, move or unpin another's shelves.
func TestPinnedShelvesArePerAccount(t *testing.T) {
	st, me, lib := pinFixture(t)
	ctx := context.Background()
	h, err := st.AddHomeShelf(ctx, me, lib, "Mine", "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateUser(ctx, "", "eli", "hash", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if got := shelfTitles(t, st, other.ID); len(got) != 0 {
		t.Errorf("another account sees %v", got)
	}
	if err := st.RenameHomeShelf(ctx, other.ID, h.ID, "Theirs"); !errors.Is(err, ErrHomeShelfNotFound) {
		t.Errorf("rename by another account: err = %v", err)
	}
	if err := st.MoveHomeShelf(ctx, other.ID, h.ID, 0); !errors.Is(err, ErrHomeShelfNotFound) {
		t.Errorf("move by another account: err = %v", err)
	}
	if err := st.DeleteHomeShelf(ctx, other.ID, h.ID); !errors.Is(err, ErrHomeShelfNotFound) {
		t.Errorf("unpin by another account: err = %v", err)
	}
	if got := fmt.Sprint(shelfTitles(t, st, me)); got != "[Mine]" {
		t.Errorf("owner's shelves after another account tried = %s", got)
	}
}

func TestPinnedShelvesGoWithTheirLibraryAndAccount(t *testing.T) {
	st, me, lib := pinFixture(t)
	ctx := context.Background()
	if _, err := st.AddHomeShelf(ctx, me, lib, "Doomed", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if got := shelfTitles(t, st, me); len(got) != 0 {
		t.Errorf("shelves over a deleted library survived: %v", got)
	}

	other, err := st.CreateUser(ctx, "", "eli", "hash", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	films, err := st.CreateLibrary(ctx, "Films 2", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddHomeShelf(ctx, other.ID, films.ID, "Theirs", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteUser(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM home_shelf WHERE user_id = ?`, other.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d shelves outlived their account", n)
	}
}
