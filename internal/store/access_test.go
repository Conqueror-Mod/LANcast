package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

/*
 * A library kept from an account (ADR 0084), asserted where it is enforced.
 *
 * As with the ceiling, the failure is silent: a listing that forgets the rule
 * shows what the household said this person should not see, and nothing about
 * the response looks wrong. So every chokepoint is tested on its own, with a
 * positive control — the same item, the same call, for somebody not
 * restricted — so a test cannot pass because the item was never there.
 */

type accessFixture struct {
	st            *Store
	open, private *Library
	openFilm      int64
	privateFilm   int64
	member, other *User
	admin         *User
}

func seedForAccess(t *testing.T) accessFixture {
	t.Helper()
	ctx := context.Background()
	st := newStore(t)
	f := accessFixture{st: st}
	mk := func(name string) *Library {
		lib, err := st.CreateLibrary(ctx, name, "movie", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return lib
	}
	f.open, f.private = mk("Films"), mk("Private")
	add := func(lib *Library, title string) int64 {
		id, err := st.UpsertItem(ctx, file(lib.ID, filepath.Join(lib.Path, title+".mkv"), title))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.openFilm = add(f.open, "Paddington")
	f.privateFilm = add(f.private, "Home Video")
	user := func(name, role string) *User {
		u, err := st.CreateUser(ctx, "", name, "hash", role)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	f.member, f.other, f.admin = user("kid", RoleMember), user("guest", RoleMember), user("host", RoleAdmin)
	if err := st.SetHiddenLibraries(ctx, f.member.ID, []int64{f.private.ID}); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestNothingIsHiddenUntilAnAdministratorSaysSo(t *testing.T) {
	f := seedForAccess(t)
	ctx := context.Background()
	if h, _ := f.st.HiddenLibraries(ctx, f.other.ID); len(h) != 0 {
		t.Errorf("an account never narrowed has hidden libraries %v", h)
	}
	// A library added later is visible to the narrowed account too: a deny
	// list, so the default for anything new is visible.
	later, err := f.st.CreateLibrary(ctx, "Music", "music", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := f.st.MayOpenLibrary(ctx, f.member.ID, later.ID); !ok {
		t.Error("a new library is hidden from an account narrowed before it existed")
	}
}

func TestAHiddenLibraryRefusesItsItemsByID(t *testing.T) {
	f := seedForAccess(t)
	ctx := context.Background()
	if ok, err := f.st.MayPlay(ctx, Account(f.member.ID), f.privateFilm); err != nil || ok {
		t.Errorf("MayPlay(hidden) = %v, %v; want refused", ok, err)
	}
	if _, err := f.st.GetItem(ctx, f.privateFilm, f.member.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetItem(hidden) err = %v, want ErrNotFound — refusal must look like absence", err)
	}
	// Controls: the same account's other library, and the same item for others.
	if ok, _ := f.st.MayPlay(ctx, Account(f.member.ID), f.openFilm); !ok {
		t.Error("the restriction reached a library it does not name")
	}
	if ok, _ := f.st.MayPlay(ctx, Account(f.other.ID), f.privateFilm); !ok {
		t.Error("one account's restriction reached another account")
	}
}

func TestAnAdministratorIsNeverRestricted(t *testing.T) {
	f := seedForAccess(t)
	ctx := context.Background()
	if err := f.st.SetHiddenLibraries(ctx, f.admin.ID, []int64{f.private.ID}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := f.st.MayPlay(ctx, Account(f.admin.ID), f.privateFilm); !ok {
		t.Error("an administrator was refused a library")
	}
	// The rows are kept, not deleted, so a demotion puts them back.
	if rows, _ := f.st.StoredHiddenLibraries(ctx, f.admin.ID); len(rows) != 1 {
		t.Errorf("stored rows for the admin = %v, want kept", rows)
	}
}

func TestAListingLeavesOutAHiddenLibrary(t *testing.T) {
	f := seedForAccess(t)
	ctx := context.Background()
	titles := func(hidden []int64) map[string]bool {
		items, _, err := f.st.ListItems(ctx, ItemFilter{HiddenLibraries: hidden, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, it := range items {
			out[it.Title] = true
		}
		return out
	}
	hidden, _ := f.st.HiddenLibraries(ctx, f.member.ID)
	if got := titles(hidden); got["Home Video"] || !got["Paddington"] {
		t.Errorf("restricted listing = %v", got)
	}
	if got := titles(nil); !got["Home Video"] {
		t.Errorf("unrestricted listing lost the private film: %v", got)
	}
}

func TestPermittedItemsLeavesOutAHiddenLibrary(t *testing.T) {
	f := seedForAccess(t)
	ctx := context.Background()
	both := []Item{{ID: f.privateFilm}, {ID: f.openFilm}}
	got, err := f.st.PermittedItems(ctx, f.member.ID, both)
	if err != nil || len(got) != 1 || got[0].ID != f.openFilm {
		t.Errorf("PermittedItems(member) = %+v, %v", got, err)
	}
	if got, _ := f.st.PermittedItems(ctx, f.other.ID, both); len(got) != 2 {
		t.Errorf("PermittedItems(other) = %+v, want both", got)
	}
}

func TestHiddenLibraryRowsGoWithTheLibraryAndTheAccount(t *testing.T) {
	f := seedForAccess(t)
	ctx := context.Background()
	if err := f.st.SetHiddenLibraries(ctx, f.member.ID, []int64{999}); !errors.Is(err, ErrNotFound) {
		t.Errorf("hiding a library that does not exist err = %v, want ErrNotFound", err)
	}
	// The failed call must not have cleared the existing row.
	if rows, _ := f.st.StoredHiddenLibraries(ctx, f.member.ID); len(rows) != 1 {
		t.Fatalf("a refused change left rows %v", rows)
	}
	if err := f.st.DeleteUser(ctx, f.member.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM library_hidden`).Scan(&n); err != nil || n != 0 {
		t.Errorf("rows after deleting the account = %d, %v", n, err)
	}
}
