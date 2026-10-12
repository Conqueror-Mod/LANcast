package store

import (
	"context"
	"errors"
	"testing"
)

func pk(userID string, cred byte) Passkey {
	return Passkey{UserID: userID, CredentialID: []byte{cred, 1, 2, 3}, PublicKey: []byte{0xa1},
		Algorithm: -7, SignCount: 0, Name: "Phone", RPID: "example.dynv6.net"}
}

func TestPasskeyRoundTrip(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "", "Chris", "h", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.AddPasskey(ctx, pk(u.ID, 1))
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.PasskeyByCredentialID(ctx, []byte{1, 1, 2, 3})
	if err != nil || got.ID != id || got.UserID != u.ID || got.LastUsedAt != 0 {
		t.Fatalf("lookup: %+v, %v", got, err)
	}
	if err := st.RecordPasskeyUse(ctx, id, 7, true); err != nil {
		t.Fatal(err)
	}
	got, _ = st.PasskeyByCredentialID(ctx, []byte{1, 1, 2, 3})
	if got.SignCount != 7 || !got.BackupState || got.LastUsedAt == 0 {
		t.Fatalf("after use: %+v", got)
	}
}

// One credential, one account: registering the same credential id twice is
// refused whoever asks.
func TestAPasskeyBelongsToOneAccount(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	a, _ := st.CreateUser(ctx, "", "A", "h", RoleMember)
	b, _ := st.CreateUser(ctx, "", "B", "h", RoleMember)
	if _, err := st.AddPasskey(ctx, pk(a.ID, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddPasskey(ctx, pk(b.ID, 1)); !errors.Is(err, ErrPasskeyExists) {
		t.Fatalf("second registration of one credential: %v", err)
	}
}

// Renaming and removing are scoped to the owner: another account gets
// ErrNotFound, the same answer as a passkey that does not exist.
func TestPasskeysAreScopedToTheirOwner(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	a, _ := st.CreateUser(ctx, "", "A", "h", RoleMember)
	b, _ := st.CreateUser(ctx, "", "B", "h", RoleAdmin)
	id, _ := st.AddPasskey(ctx, pk(a.ID, 1))
	if err := st.RenamePasskey(ctx, b.ID, id, "mine now"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename by another account: %v", err)
	}
	if err := st.DeletePasskey(ctx, b.ID, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete by another account: %v", err)
	}
	if list, _ := st.PasskeysByUser(ctx, b.ID); len(list) != 0 {
		t.Fatalf("another account lists %d passkeys", len(list))
	}
	if err := st.RenamePasskey(ctx, a.ID, id, "Laptop"); err != nil {
		t.Fatal(err)
	}
	list, _ := st.PasskeysByUser(ctx, a.ID)
	if len(list) != 1 || list[0].Name != "Laptop" {
		t.Fatalf("owner's list %+v", list)
	}
	if err := st.DeletePasskey(ctx, a.ID, id); err != nil {
		t.Fatal(err)
	}
}

func TestDeletingAnAccountDeletesItsPasskeys(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "", "Gone", "h", RoleMember)
	if _, err := st.AddPasskey(ctx, pk(u.ID, 9)); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM passkey`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d passkey rows survived the account (%v)", n, err)
	}
	if _, err := st.PasskeyByCredentialID(ctx, []byte{9, 1, 2, 3}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("lookup after deletion: %v", err)
	}
}
