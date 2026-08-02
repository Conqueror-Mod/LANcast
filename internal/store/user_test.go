package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestCreateUserAndLookup(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	u, err := st.CreateUser(ctx, "", "Chris", "hash", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == "" {
		t.Error("expected a generated id")
	}

	// Name lookup is case-insensitive (COLLATE NOCASE) so login does not depend
	// on capitalisation.
	got, err := st.UserByName(ctx, "chris")
	if err != nil {
		t.Fatalf("UserByName: %v", err)
	}
	if got.ID != u.ID || got.Role != RoleAdmin {
		t.Errorf("UserByName = %+v, want id %s admin", got, u.ID)
	}
}

func TestCreateUserDuplicateName(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	if _, err := st.CreateUser(ctx, "", "Chris", "h", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	// Different case, same name — must collide.
	_, err := st.CreateUser(ctx, "", "CHRIS", "h", RoleMember)
	if !errors.Is(err, ErrDuplicate) {
		t.Errorf("second CreateUser err = %v, want ErrDuplicate", err)
	}
}

func TestSeededLocalIDPreservesUserID(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	// The migrated owner keeps the 'local' id so pre-existing sessions and
	// playback rows keep resolving.
	if _, err := st.CreateUser(ctx, LocalUserID, "admin", "h", RoleAdmin); err != nil {
		t.Fatalf("seed local: %v", err)
	}
	got, err := st.UserByID(ctx, LocalUserID)
	if err != nil || got.ID != LocalUserID {
		t.Fatalf("UserByID(local) = %+v, %v", got, err)
	}
}

func TestSessionRequiresLiveUser(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	u, err := st.CreateUser(ctx, "", "Chris", "h", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, "tok-hash", u.ID, time.Hour); err != nil {
		t.Fatal(err)
	}

	sess, err := st.LookupSession(ctx, "tok-hash")
	if err != nil {
		t.Fatalf("LookupSession: %v", err)
	}
	if sess.Name != "Chris" || sess.Role != RoleMember {
		t.Errorf("session = %+v, want Chris/member from the joined user", sess)
	}

	// Deleting the user must make their session stop resolving immediately.
	if err := st.DeleteUser(ctx, u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := st.LookupSession(ctx, "tok-hash"); !errors.Is(err, ErrNotFound) {
		t.Errorf("LookupSession after delete err = %v, want ErrNotFound", err)
	}
}

func TestCountUsersAndAdmins(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	if n, _ := st.CountUsers(ctx); n != 0 {
		t.Errorf("fresh CountUsers = %d, want 0", n)
	}
	if _, err := st.CreateUser(ctx, "", "a", "h", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, "", "b", "h", RoleMember); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.CountUsers(ctx); n != 2 {
		t.Errorf("CountUsers = %d, want 2", n)
	}
	if n, _ := st.CountAdmins(ctx); n != 1 {
		t.Errorf("CountAdmins = %d, want 1", n)
	}
}

func TestDeleteUserCleansPlaybackAndSessions(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	lib := mustLibrary(t, st)
	id, err := st.UpsertItem(ctx, file(lib.ID, filepath.Join(lib.Path, "m.mkv"), "M"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUser(ctx, "", "Chris", "h", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProgress(ctx, id, u.ID, 1000, false); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, "tok", u.ID, time.Hour); err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteUser(ctx, u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	// Progress rows for the deleted user must be gone, not orphaned.
	var n int
	if err := st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM playback_state WHERE user_id = ?`, u.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("playback rows after delete = %d, want 0", n)
	}
}

// The lockout recovery path. Every account and session goes; watch history
// stays, because it is the library's data rather than the account's — and the
// first admin created afterwards takes LocalUserID, the id those rows already
// carry, so the history reconnects instead of being orphaned.
func TestDeleteAllUsersKeepsWatchHistory(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	lib := mustLibrary(t, st)
	item, err := st.UpsertItem(ctx, file(lib.ID, filepath.Join(lib.Path, "m.mkv"), "M"))
	if err != nil {
		t.Fatal(err)
	}

	owner, err := st.CreateUser(ctx, LocalUserID, "Chris", "h", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateUser(ctx, "", "Guest", "h", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProgress(ctx, item, owner.ID, 4200, false); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, "tok-a", owner.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, "tok-b", other.ID, time.Hour); err != nil {
		t.Fatal(err)
	}

	users, sessions, err := st.DeleteAllUsers(ctx)
	if err != nil {
		t.Fatalf("DeleteAllUsers: %v", err)
	}
	if users != 2 || sessions != 2 {
		t.Errorf("removed %d users / %d sessions, want 2 / 2", users, sessions)
	}

	if n, _ := st.CountUsers(ctx); n != 0 {
		t.Errorf("CountUsers = %d after reset, want 0", n)
	}
	if n, _ := st.CountSessions(ctx); n != 0 {
		t.Errorf("CountSessions = %d after reset, want 0", n)
	}

	// The point of the whole thing: history is still there.
	var n int
	if err := st.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM playback_state WHERE user_id = ?`, LocalUserID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("playback rows for %s = %d, want 1 — a reset must not erase watch history", LocalUserID, n)
	}

	// And it reconnects: the replacement admin takes the same id.
	if _, err := st.CreateUser(ctx, LocalUserID, "Chris", "h2", RoleAdmin); err != nil {
		t.Fatalf("recreate admin: %v", err)
	}
	items, _, err := st.ListItems(ctx, ItemFilter{UserID: LocalUserID, LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AttachProgress(ctx, items, LocalUserID); err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 || items[0].Progress == nil || items[0].Progress.PositionMS != 4200 {
		t.Errorf("resume point did not reconnect to the new account: %+v", items)
	}
}

// The library itself is not account data and must survive untouched.
func TestDeleteAllUsersLeavesTheLibraryAlone(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	lib := mustLibrary(t, st)
	if _, err := st.UpsertItem(ctx, file(lib.ID, filepath.Join(lib.Path, "m.mkv"), "M")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, LocalUserID, "Chris", "h", RoleAdmin); err != nil {
		t.Fatal(err)
	}

	if _, _, err := st.DeleteAllUsers(ctx); err != nil {
		t.Fatal(err)
	}

	libs, err := st.ListLibraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 1 || libs[0].ItemCount != 1 {
		t.Errorf("libraries after reset = %+v, want the one library with its item", libs)
	}
}

// Running it on an instance with no accounts is a no-op, not an error — that is
// the state a fresh install is in, and the command reports it rather than
// failing.
func TestDeleteAllUsersOnFreshInstall(t *testing.T) {
	st := newStore(t)
	users, sessions, err := st.DeleteAllUsers(context.Background())
	if err != nil {
		t.Fatalf("DeleteAllUsers on a fresh install: %v", err)
	}
	if users != 0 || sessions != 0 {
		t.Errorf("removed %d users / %d sessions from an empty database", users, sessions)
	}
}
