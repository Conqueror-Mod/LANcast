package store

import (
	"context"
	"testing"
	"time"
)

// grantFixture gives a local account, a paired peer, and one person on it —
// the minimum for a grant to be possible at all.
func grantFixture(t *testing.T) (*Store, context.Context, string) {
	t.Helper()
	st := newStore(t)
	ctx := context.Background()

	u, err := st.CreateUser(ctx, "chris", "Chris", "hash", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddPeer(ctx, samplePeer(fpA)); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceRemotePeople(ctx, fpA, []RemotePerson{
		{ID: "g-1", Name: "Georgia"},
		{ID: "g-2", Name: "Alex"},
	}); err != nil {
		t.Fatal(err)
	}
	return st, ctx, u.ID
}

func TestGrantAndRead(t *testing.T) {
	st, ctx, chris := grantFixture(t)

	if err := st.GrantPresence(ctx, chris, fpA, "g-1", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}

	got, err := st.PresenceGrants(ctx, chris)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PersonID != "g-1" {
		t.Fatalf("grants = %+v, want one naming g-1", got)
	}
	if got[0].GrantedAt != 1000 {
		t.Errorf("granted_at = %d, want 1000", got[0].GrantedAt)
	}

	readers, err := st.ReadersOf(ctx, fpA, "g-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(readers) != 1 || readers[0] != chris {
		t.Errorf("ReadersOf = %v, want [%s]", readers, chris)
	}
}

// ADR 0045 §5: off by default. The absence of a row is the default, so this is
// really a test that nothing anywhere creates one on somebody's behalf.
func TestNobodySeesAnybodyByDefault(t *testing.T) {
	st, ctx, chris := grantFixture(t)

	if got, _ := st.PresenceGrants(ctx, chris); len(got) != 0 {
		t.Errorf("a fresh account has grants: %+v", got)
	}
	readers, _ := st.ReadersOf(ctx, fpA, "g-1")
	if len(readers) != 0 {
		t.Errorf("ReadersOf = %v on a server where nobody granted anything", readers)
	}
}

// Clicking a switch that is already on has changed nothing, and rewriting the
// date would misreport when the person actually decided.
func TestRegrantingDoesNotMoveTheDate(t *testing.T) {
	st, ctx, chris := grantFixture(t)

	if err := st.GrantPresence(ctx, chris, fpA, "g-1", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if err := st.GrantPresence(ctx, chris, fpA, "g-1", time.Unix(9999, 0)); err != nil {
		t.Fatalf("re-granting must not be an error: %v", err)
	}

	got, _ := st.PresenceGrants(ctx, chris)
	if len(got) != 1 {
		t.Fatalf("grants = %+v, want exactly one", got)
	}
	if got[0].GrantedAt != 1000 {
		t.Errorf("granted_at = %d, want the original 1000", got[0].GrantedAt)
	}
}

func TestRevokeIsImmediateAndIdempotent(t *testing.T) {
	st, ctx, chris := grantFixture(t)
	if err := st.GrantPresence(ctx, chris, fpA, "g-1", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}

	if err := st.RevokePresence(ctx, chris, fpA, "g-1"); err != nil {
		t.Fatal(err)
	}
	if readers, _ := st.ReadersOf(ctx, fpA, "g-1"); len(readers) != 0 {
		t.Errorf("ReadersOf = %v after revoke; the next request must already see it gone", readers)
	}
	// Revoking what is not granted is success: the caller asked for this person
	// not to see them, and afterwards they cannot.
	if err := st.RevokePresence(ctx, chris, fpA, "g-1"); err != nil {
		t.Errorf("second revoke: %v", err)
	}
}

// ADR 0045 §2: an account that has not opted into the roster cannot be named by
// anybody's grant. The foreign key is what makes that true rather than
// something a handler must remember.
func TestCannotGrantToSomebodyWhoIsNotInTheRoster(t *testing.T) {
	st, ctx, chris := grantFixture(t)

	if err := st.GrantPresence(ctx, chris, fpA, "nobody", time.Unix(1000, 0)); err == nil {
		t.Error("granted presence to a person no peer has vouched for")
	}
	if err := st.GrantPresence(ctx, chris, fpB, "g-1", time.Unix(1000, 0)); err == nil {
		t.Error("granted presence on a peer this server has never been introduced to")
	}
}

/*
 * ADR 0045 §5 and ADR 0046: unpairing revokes everything on that server at
 * once, "immediately, with no per-person cleanup". The cascade is what makes
 * that a property of the schema instead of a loop somebody has to write and
 * keep correct.
 */
func TestUnpairingRevokesEveryGrantToThatServer(t *testing.T) {
	st, ctx, chris := grantFixture(t)
	for _, id := range []string{"g-1", "g-2"} {
		if err := st.GrantPresence(ctx, chris, fpA, id, time.Unix(1000, 0)); err != nil {
			t.Fatal(err)
		}
	}

	if err := st.RemovePeer(ctx, fpA); err != nil {
		t.Fatal(err)
	}

	got, err := st.PresenceGrants(ctx, chris)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("grants = %+v after unpairing; removing a peer must take its grants with it", got)
	}
}

/*
 * A roster refresh replaces the peer's people wholesale. Somebody who turned
 * visible_to_peers off disappears from it — and their grants must go too,
 * because a grant naming somebody who has withdrawn is a permission with
 * nobody's consent behind it.
 */
func TestGrantsFollowThePersonOutOfTheRoster(t *testing.T) {
	st, ctx, chris := grantFixture(t)
	for _, id := range []string{"g-1", "g-2"} {
		if err := st.GrantPresence(ctx, chris, fpA, id, time.Unix(1000, 0)); err != nil {
			t.Fatal(err)
		}
	}

	// Georgia withdrew; Alex is still there.
	if err := st.ReplaceRemotePeople(ctx, fpA, []RemotePerson{{ID: "g-2", Name: "Alex"}}); err != nil {
		t.Fatal(err)
	}

	got, err := st.PresenceGrants(ctx, chris)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PersonID != "g-2" {
		t.Errorf("grants = %+v, want only the person still in the roster", got)
	}
}

// Grants belong to the account that made them. One person's list must never
// include another's, which is also why every method takes the granting id.
func TestGrantsAreNotSharedBetweenAccounts(t *testing.T) {
	st, ctx, chris := grantFixture(t)
	other, err := st.CreateUser(ctx, "sam", "Sam", "hash", "member")
	if err != nil {
		t.Fatal(err)
	}

	if err := st.GrantPresence(ctx, chris, fpA, "g-1", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}

	got, _ := st.PresenceGrants(ctx, other.ID)
	if len(got) != 0 {
		t.Errorf("another account's grants leaked: %+v", got)
	}
	// ReadersOf spans accounts by design — it answers "who agreed to be seen by
	// this person" — so it must name only the one who actually did.
	readers, _ := st.ReadersOf(ctx, fpA, "g-1")
	if len(readers) != 1 || readers[0] != chris {
		t.Errorf("ReadersOf = %v, want only %s", readers, chris)
	}
}

func TestDeletingAnAccountTakesItsGrants(t *testing.T) {
	st, ctx, chris := grantFixture(t)
	if err := st.GrantPresence(ctx, chris, fpA, "g-1", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteUser(ctx, chris); err != nil {
		t.Fatal(err)
	}
	if readers, _ := st.ReadersOf(ctx, fpA, "g-1"); len(readers) != 0 {
		t.Errorf("ReadersOf = %v after the granting account was deleted", readers)
	}
}
