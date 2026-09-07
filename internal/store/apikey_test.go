package store

import (
	"context"
	"errors"
	"testing"
)

/*
 * API key storage (ADR 0061).
 *
 * The two worth having are ownership and revocation, because both are the kind
 * that fail open: a scoping mistake reads as "it worked", and so does a delete
 * that deleted nothing.
 */

func keyUser(t *testing.T, st *Store, name string) string {
	t.Helper()
	u, err := st.CreateUser(context.Background(), "", name, "hash", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// A key resolves to its owner's account, with the role the account has.
func TestAKeyResolvesToItsOwner(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	uid := keyUser(t, st, "owner")

	if _, err := st.CreateAPIKey(ctx, "hash-a", uid, "script"); err != nil {
		t.Fatal(err)
	}
	sess, id, err := st.LookupAPIKey(ctx, "hash-a")
	if err != nil {
		t.Fatal(err)
	}
	if sess.UserID != uid {
		t.Errorf("resolved to %q, want %q", sess.UserID, uid)
	}
	if id == "" {
		t.Error("no key id returned, so nothing can record that it was used")
	}
	if sess.ExpiresAt != 0 {
		t.Error("a key was given an expiry; the first code to treat it as a " +
			"session is the one that logs every integration out on a password change")
	}
}

/*
 * One person's key cannot be revoked by another.
 *
 * The owner is in the WHERE clause rather than checked in the handler, so this
 * is testing the thing that actually protects it. A handler-side check is one a
 * second handler can be written without.
 */
func TestAKeyCannotBeRevokedByAnotherAccount(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	mine := keyUser(t, st, "mine")
	yours := keyUser(t, st, "yours")

	key, err := st.CreateAPIKey(ctx, "hash-b", mine, "script")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteAPIKey(ctx, key.ID, yours); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound — somebody else's key was revoked", err)
	}
	if _, _, err := st.LookupAPIKey(ctx, "hash-b"); err != nil {
		t.Error("the key stopped working after somebody else tried to revoke it")
	}
}

// And a list is the caller's own keys only.
func TestAListHoldsOnlyTheCallersKeys(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	mine := keyUser(t, st, "mine")
	yours := keyUser(t, st, "yours")

	if _, err := st.CreateAPIKey(ctx, "hash-c", mine, "mine"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAPIKey(ctx, "hash-d", yours, "yours"); err != nil {
		t.Fatal(err)
	}
	keys, err := st.ListAPIKeys(ctx, mine)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].Name != "mine" {
		t.Fatalf("list returned %d keys %v; it must hold the caller's own only",
			len(keys), keys)
	}
}

// A deleted account takes its keys with it, immediately, without waiting for a
// cascade to have run — the same join LookupSession makes.
func TestADeletedAccountsKeysStopResolving(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	uid := keyUser(t, st, "leaving")

	if _, err := st.CreateAPIKey(ctx, "hash-e", uid, "script"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteUser(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.LookupAPIKey(ctx, "hash-e"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound — a deleted account's key still works", err)
	}
}

// Last-used starts at zero and moves, which is what makes a list judgeable.
func TestLastUsedStartsAtZeroAndMoves(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	uid := keyUser(t, st, "owner")

	key, err := st.CreateAPIKey(ctx, "hash-f", uid, "script")
	if err != nil {
		t.Fatal(err)
	}
	if key.LastUsed != 0 {
		t.Errorf("last_used = %d on a new key, want 0 — zero is what lets the "+
			"client say 'never used' rather than a date in 1970", key.LastUsed)
	}
	st.TouchAPIKey(ctx, key.ID)

	keys, err := st.ListAPIKeys(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].LastUsed == 0 {
		t.Error("using a key did not record that it was used")
	}
}
