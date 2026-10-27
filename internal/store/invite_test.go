package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"
)

func hashOf(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

// Redeeming makes a member with the invite's limits and spends the invite, in
// one step (ADR 0104).
func TestAnInviteMakesOneMemberWithItsLimits(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	kept, err := st.CreateLibrary(ctx, "Private", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gone, err := st.CreateLibrary(ctx, "Soon gone", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := st.CreateInvite(ctx, Invite{
		Note: "for Georgia", MaxContentRating: "PG-13", HiddenLibraries: []int64{kept.ID, gone.ID},
		CreatedBy: "dana", CreatedAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix(),
	}, hashOf("secret")); err != nil {
		t.Fatal(err)
	}
	// A library removed after the invite was made is simply skipped.
	if err := st.DeleteLibrary(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}

	u, inv, err := st.RedeemInvite(ctx, hashOf("secret"), "georgia", "hash", now)
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != RoleMember || inv.Note != "for Georgia" {
		t.Fatalf("user %+v invite %+v", u, inv)
	}
	if rating, _ := st.MaxContentRating(ctx, u.ID); rating != "PG-13" {
		t.Fatalf("ceiling %q", rating)
	}
	if hidden, _ := st.StoredHiddenLibraries(ctx, u.ID); len(hidden) != 1 || hidden[0] != kept.ID {
		t.Fatalf("hidden %v", hidden)
	}
	// Spent.
	if _, _, err := st.RedeemInvite(ctx, hashOf("secret"), "someone else", "hash", now); !errors.Is(err, ErrInviteUnusable) {
		t.Fatalf("a second redemption: %v", err)
	}
	if list, _ := st.Invites(ctx, now); len(list) != 0 {
		t.Fatalf("a spent invite is still listed: %+v", list)
	}
}

// A taken name leaves the invite as it was, to be tried again.
func TestATakenNameLeavesTheInvite(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if _, err := st.CreateUser(ctx, "", "georgia", "h", RoleMember); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	st.CreateInvite(ctx, Invite{CreatedBy: "dana", CreatedAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix()}, hashOf("s"))
	if _, _, err := st.RedeemInvite(ctx, hashOf("s"), "Georgia", "h", now); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("taken name: %v", err)
	}
	if _, _, err := st.RedeemInvite(ctx, hashOf("s"), "georgia2", "h", now); err != nil {
		t.Fatalf("the invite did not survive a taken name: %v", err)
	}
}

// Expired, withdrawn and unknown all read as the same unusable invite, and
// expired ones leave the list.
func TestAnUnusableInviteIsOneAnswer(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Now()
	st.CreateInvite(ctx, Invite{CreatedBy: "d", CreatedAt: now.Unix(), ExpiresAt: now.Add(-time.Second).Unix()}, hashOf("old"))
	w, _ := st.CreateInvite(ctx, Invite{CreatedBy: "d", CreatedAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix()}, hashOf("withdrawn"))
	if err := st.DeleteInvite(ctx, w.ID); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"old", "withdrawn", "never made"} {
		if _, err := st.InviteByToken(ctx, hashOf(secret), now); !errors.Is(err, ErrInviteUnusable) {
			t.Errorf("%s: %v", secret, err)
		}
		if _, _, err := st.RedeemInvite(ctx, hashOf(secret), "x"+secret, "h", now); !errors.Is(err, ErrInviteUnusable) {
			t.Errorf("redeem %s: %v", secret, err)
		}
	}
	if list, _ := st.Invites(ctx, now); len(list) != 0 {
		t.Fatalf("listed: %+v", list)
	}
	if err := st.DeleteInvite(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("withdrawing nothing: %v", err)
	}
}

// Two people racing one link get one account between them.
func TestARacedInviteMakesOneAccount(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Now()
	st.CreateInvite(ctx, Invite{CreatedBy: "d", CreatedAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix()}, hashOf("race"))
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := st.RedeemInvite(ctx, hashOf("race"), "racer"+string(rune('a'+i)), "h", now)
			if err == nil {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if won != 1 {
		t.Fatalf("%d accounts from one invite", won)
	}
}
