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
