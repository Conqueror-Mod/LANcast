package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateUserAndLookup(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	u, err := st.CreateUser(ctx, "", "Dana", "hash", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == "" {
		t.Error("expected a generated id")
	}

	// Name lookup is case-insensitive (COLLATE NOCASE) so login does not depend
	// on capitalisation.
	got, err := st.UserByName(ctx, "dana")
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
	if _, err := st.CreateUser(ctx, "", "Dana", "h", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	// Different case, same name — must collide.
	_, err := st.CreateUser(ctx, "", "DANA", "h", RoleMember)
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

	u, err := st.CreateUser(ctx, "", "Dana", "h", RoleMember)
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
	if sess.Name != "Dana" || sess.Role != RoleMember {
		t.Errorf("session = %+v, want Dana/member from the joined user", sess)
	}

	// Deleting the user must make their session stop resolving immediately.
	if err := st.DeleteUser(ctx, u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := st.LookupSession(ctx, "tok-hash"); !errors.Is(err, ErrNotFound) {
		t.Errorf("LookupSession after delete err = %v, want ErrNotFound", err)
	}
}

// An expired session refuses requests at once, but its row stays until
// something sweeps it; the sweep takes the dead rows and leaves the live.
func TestPurgeExpiredSessionsLeavesLiveOnes(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "", "Dana", "h", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, "dead", u.ID, -time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, "live", u.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := st.PurgeExpiredSessions(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM session`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d session rows after the sweep, want 1", n)
	}
	if _, err := st.LookupSession(ctx, "live"); err != nil {
		t.Errorf("the live session was swept: %v", err)
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
	u, err := st.CreateUser(ctx, "", "Dana", "h", RoleMember)
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

	owner, err := st.CreateUser(ctx, LocalUserID, "Dana", "h", RoleAdmin)
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
	if _, err := st.CreateUser(ctx, LocalUserID, "Dana", "h2", RoleAdmin); err != nil {
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
	if _, err := st.CreateUser(ctx, LocalUserID, "Dana", "h", RoleAdmin); err != nil {
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

/*
 * R04-2: reset-auth is how an operator gets their server back, including from
 * somebody who took the admin account. The first admin created afterwards
 * takes LocalUserID, the id the old one had, so every credential keyed on that
 * id must go with the accounts -- or the new administrator inherits whatever
 * the intruder left behind.
 */
func TestDeleteAllUsersClearsEveryCredential(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	if _, err := st.CreateUser(ctx, LocalUserID, "owner", "h1", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateUser(ctx, "", "member", "h1", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddPasskey(ctx, Passkey{UserID: LocalUserID, CredentialID: []byte("cred-old"),
		PublicKey: []byte("pk"), Algorithm: -7, Name: "someone's phone", RPID: "example.test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAPIKey(ctx, "key-hash", LocalUserID, "script"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAPIKey(ctx, "member-key-hash", member.ID, "script"); err != nil {
		t.Fatal(err)
	}
	if err := st.EnableTwoFactor(ctx, LocalUserID, []byte("sealed"), []string{"rec-hash"}, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateInvite(ctx, Invite{CreatedBy: LocalUserID, ExpiresAt: time.Now().Add(time.Hour).Unix()},
		[]byte("invite-hash")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRetroAccount(ctx, RetroAccount{UserID: LocalUserID, RAUser: "ra",
		RAULID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", APIKeySealed: []byte("sealed")}); err != nil {
		t.Fatal(err)
	}
	if err := st.QueueRetroUnlock(ctx, LocalUserID, `{"a":1}`); err != nil {
		t.Fatal(err)
	}

	if _, _, err := st.DeleteAllUsers(ctx); err != nil {
		t.Fatal(err)
	}
	// First-run setup, which always uses LocalUserID.
	if _, err := st.CreateUser(ctx, LocalUserID, "newadmin", "h2", RoleAdmin); err != nil {
		t.Fatal(err)
	}

	if p, err := st.PasskeyByCredentialID(ctx, []byte("cred-old")); err == nil {
		t.Errorf("the pre-reset passkey %q signs in as %q", p.Name, p.UserID)
	}
	for _, h := range []string{"key-hash", "member-key-hash"} {
		if _, _, err := st.LookupAPIKey(ctx, h); err == nil {
			t.Errorf("the pre-reset API key %q still resolves", h)
		}
	}
	if tf, err := st.TwoFactorFor(ctx, LocalUserID); err != nil || tf.Enabled || len(tf.Recovery) > 0 {
		t.Errorf("two-factor after reset = %+v, %v; want none", tf, err)
	}
	if inv, err := st.InviteByToken(ctx, []byte("invite-hash"), time.Now()); err == nil {
		t.Errorf("the pre-reset invite %q is still redeemable", inv.ID)
	}
	if invs, err := st.Invites(ctx, time.Now()); err != nil || len(invs) != 0 {
		t.Errorf("invites after reset = %v, %v; want none", invs, err)
	}
	if a, err := st.GetRetroAccount(ctx, LocalUserID); err != nil || a != nil {
		t.Errorf("the old RetroAchievements account came with the new admin: %+v, %v", a, err)
	}
	if n, err := st.CountRetroUnlocks(ctx, LocalUserID); err != nil || n != 0 {
		t.Errorf("queued unlocks after reset = %d, %v; want 0", n, err)
	}
}

// The last administrator cannot be deleted, and the refusal is the store's:
// two administrators deleting each other at the same moment leave one, not
// none (R01-11, R04-5). Before, the count lived in the handler and both
// deletes of a pair that each counted two went through.
func TestTwoAdminsDeletingEachOtherLeaveOne(t *testing.T) {
	for round := 0; round < 20; round++ {
		st := newStore(t)
		ctx := context.Background()
		a, err := st.CreateUser(ctx, "", "A", "h", RoleAdmin)
		if err != nil {
			t.Fatal(err)
		}
		b, err := st.CreateUser(ctx, "", "B", "h", RoleAdmin)
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		errs := make(chan error, 2)
		for _, id := range []string{a.ID, b.ID} {
			go func(id string) {
				<-start
				errs <- st.DeleteUser(ctx, id)
			}(id)
		}
		close(start)
		e1, e2 := <-errs, <-errs
		lost := 0
		for _, e := range []error{e1, e2} {
			switch {
			case e == nil:
			case errors.Is(e, ErrLastAdmin):
				lost++
			default:
				t.Fatalf("round %d: %v", round, e)
			}
		}
		if lost != 1 {
			t.Fatalf("round %d: %d deletes refused, want exactly 1", round, lost)
		}
		if n, err := st.CountAdmins(ctx); err != nil || n != 1 {
			t.Fatalf("round %d: %d administrators left (%v)", round, n, err)
		}
	}
}

func TestTheOnlyAdminCannotBeDeletedByTheStore(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	a, _ := st.CreateUser(ctx, "", "A", "h", RoleAdmin)
	m, _ := st.CreateUser(ctx, "", "M", "h", RoleMember)
	if err := st.DeleteUser(ctx, a.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("deleting the only admin: %v, want ErrLastAdmin", err)
	}
	if err := st.DeleteUser(ctx, m.ID); err != nil {
		t.Fatalf("deleting a member: %v", err)
	}
	if err := st.DeleteUser(ctx, "u_nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting nobody: %v, want ErrNotFound", err)
	}
}

/*
 * Deleting an account leaves nothing keyed to it, in any table (R04-6).
 *
 * Most user-keyed tables have no foreign key to user, so DeleteUser removes
 * their rows by hand, and the list has been forgotten twice: user_rating (a
 * written review) and profile_totals (banked watch time) outlived the account
 * in every backup since. This walks sqlite_master rather than a list, so the
 * next table with a user_id column is covered the day it is created: it puts
 * one row for the account in every such table, deletes the account, and asks
 * every table again.
 *
 * Rows are written with foreign keys off on one connection, since a row in
 * playback_state must name an item and the point here is the user_id, not
 * the item. Columns that are NOT NULL with no default get a placeholder of
 * their type.
 */
func TestDeletingAnAccountLeavesNoRowInAnyUserTable(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	if _, err := st.CreateUser(ctx, "", "Keeper", "h", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	gone, err := st.CreateUser(ctx, "", "Gone", "h", RoleMember)
	if err != nil {
		t.Fatal(err)
	}

	tables := userTables(t, st)
	if len(tables) < 10 {
		t.Fatalf("found only %d tables with a user_id column; the walk is broken", len(tables))
	}

	conn, err := st.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		cols, vals := placeholderRow(t, conn, table, gone.ID)
		q := `INSERT INTO "` + table + `" (` + strings.Join(cols, ", ") + `) VALUES (?` +
			strings.Repeat(", ?", len(cols)-1) + `)`
		if _, err := conn.ExecContext(ctx, q, vals...); err != nil {
			t.Fatalf("seed %s: %v (teach placeholderRow about this table)", table, err)
		}
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	conn.Close()

	if err := st.DeleteUser(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		var n int
		if err := st.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM "`+table+`" WHERE user_id = ?`, gone.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s keeps %d row(s) for a deleted account", table, n)
		}
	}
}

// userTables lists every table with a column named user_id.
func userTables(t *testing.T, st *Store) []string {
	t.Helper()
	rows, err := st.db.QueryContext(context.Background(), `
		SELECT DISTINCT m.name FROM sqlite_master m JOIN pragma_table_info(m.name) p
		WHERE m.type = 'table' AND p.name = 'user_id' ORDER BY m.name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		out = append(out, name)
	}
	return out
}

// placeholderRow is a row for table naming userID, filling every column that
// must be filled with a value of its declared type.
func placeholderRow(t *testing.T, conn *sql.Conn, table, userID string) ([]string, []any) {
	t.Helper()
	rows, err := conn.QueryContext(context.Background(),
		`SELECT name, type, "notnull", dflt_value IS NOT NULL, pk FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var cols []string
	var vals []any
	for rows.Next() {
		var name, typ string
		var notNull, hasDefault bool
		var pk int
		if err := rows.Scan(&name, &typ, &notNull, &hasDefault, &pk); err != nil {
			t.Fatal(err)
		}
		switch {
		case name == "user_id":
			cols, vals = append(cols, name), append(vals, userID)
		case (notNull || pk > 0) && !hasDefault:
			var v any = "x"
			switch u := strings.ToUpper(typ); {
			case strings.Contains(u, "INT"):
				v = 1
			case strings.Contains(u, "REAL"), strings.Contains(u, "FLOA"):
				v = 1.0
			case strings.Contains(u, "BLOB"):
				v = []byte{1}
			}
			cols, vals = append(cols, `"`+name+`"`), append(vals, v)
		}
	}
	return cols, vals
}
